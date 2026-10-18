package chat

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"runtime/debug"
	"sync"
	"time"

	"github.com/IBM/sarama"

	"echo_chat_server/internal/config"
	"echo_chat_server/internal/observability"
	"echo_chat_server/internal/pressure"
	"echo_chat_server/pkg/zlog"

	"go.uber.org/zap"
)

const (
	conversationBucketMaxBuckets         = 100000
	conversationBucketIdleTTL            = 5 * time.Minute
	conversationBucketGCInterval         = 1 * time.Minute
	conversationBucketMaxGCPerRun        = 1024
	conversationBucketPauseThreshold     = 20000
	conversationBucketResumeThreshold    = 12000
	conversationBucketMaxQueueDepth      = 2048
	conversationBucketCompleteDrainBatch = 64
	conversationBucketIngressQueueSize   = 8192
	conversationBucketKafkaReaderCount   = 20
)

type conversationBucketJob struct {
	kafkaMessage  *sarama.ConsumerMessage
	decoded       decodedConsumedMessage
	readyEnqueued bool
	readyDequeued bool
}

type conversationBucketState struct {
	key          string
	mu           sync.Mutex
	queue        []*conversationBucketJob
	running      bool
	queued       bool
	lastActiveAt int64
	lruElem      *list.Element
}

type conversationBucketProcessResult struct {
	job       *conversationBucketJob
	err       error
	markable  bool
	retryable bool
}

type conversationBucketSessionLaneItem struct {
	kafkaMessage *sarama.ConsumerMessage
	readerID     int
	lane         int
}

type conversationBucketClaimRunner struct {
	server     *KafkaServer
	session    sarama.ConsumerGroupSession
	claim      sarama.ConsumerGroupClaim
	readyCh    chan *conversationBucketState
	completeCh chan *conversationBucketProcessResult
	stopCh     chan struct{}

	workerCount        int
	maxMessagesPerTurn int
	maxRunDuration     time.Duration
	drainTimeout       time.Duration

	commitBatchSize int
	commitInterval  time.Duration
	asyncCommit     bool

	wg                sync.WaitGroup
	buckets           map[string]*conversationBucketState
	bucketsMu         sync.Mutex
	lruMu             sync.Mutex
	stateMu           sync.Mutex
	lruList           *list.List
	maxBuckets        int
	bucketIdleTTL     time.Duration
	gcInterval        time.Duration
	maxGCPerRun       int
	totalBucketQueued int
	pauseThreshold    int
	resumeThreshold   int
	maxBucketDepth    int
	paused            bool
	pendingMessage    *decodedConsumedMessage

	markMu             sync.Mutex
	nextMarkOffset     int64
	inflight           int
	completed          map[int64]*conversationBucketProcessResult
	completeDrainBatch int
}

func (k *KafkaServer) consumeClaimConversationBucket(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	return newConversationBucketClaimRunner(k, session, claim).run()
}

func newConversationBucketClaimRunner(server *KafkaServer, session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) *conversationBucketClaimRunner {
	kafkaCfg := config.GetConfig().KafkaConfig
	completeChSize := kafkaCfg.ResolveConversationBucketReadyQueueSize() * 8
	workerSizedCompleteCh := kafkaCfg.ResolveConversationBucketWorkerCount() * 16
	if completeChSize < workerSizedCompleteCh {
		completeChSize = workerSizedCompleteCh
	}
	return &conversationBucketClaimRunner{
		server:             server,
		session:            session,
		claim:              claim,
		readyCh:            make(chan *conversationBucketState, kafkaCfg.ResolveConversationBucketReadyQueueSize()),
		completeCh:         make(chan *conversationBucketProcessResult, completeChSize),
		stopCh:             make(chan struct{}),
		workerCount:        kafkaCfg.ResolveConversationBucketWorkerCount(),
		maxMessagesPerTurn: kafkaCfg.ResolveConversationBucketMaxMessagesPerTurn(),
		maxRunDuration:     kafkaCfg.ResolveConversationBucketMaxRunDuration(),
		drainTimeout:       kafkaCfg.ResolveConversationBucketDrainTimeout(),
		commitBatchSize:    kafkaCfg.ResolveConsumerCommitBatchSize(),
		commitInterval:     kafkaCfg.ResolveConsumerCommitInterval(),
		asyncCommit:        kafkaCfg.ResolveConsumerAsyncCommitEnabled(),
		buckets:            make(map[string]*conversationBucketState),
		lruList:            list.New(),
		maxBuckets:         conversationBucketMaxBuckets,
		bucketIdleTTL:      conversationBucketIdleTTL,
		gcInterval:         conversationBucketGCInterval,
		maxGCPerRun:        conversationBucketMaxGCPerRun,
		pauseThreshold:     conversationBucketPauseThreshold,
		resumeThreshold:    conversationBucketResumeThreshold,
		maxBucketDepth:     conversationBucketMaxQueueDepth,
		completed:          make(map[int64]*conversationBucketProcessResult),
		nextMarkOffset:     -1,
		completeDrainBatch: conversationBucketCompleteDrainBatch,
	}
}

func (r *conversationBucketClaimRunner) run() error {
	if r.workerCount < 1 {
		r.workerCount = 1
	}
	if r.maxMessagesPerTurn < 1 {
		r.maxMessagesPerTurn = 1
	}
	if r.maxRunDuration <= 0 {
		r.maxRunDuration = 5 * time.Millisecond
	}
	if r.drainTimeout <= 0 {
		r.drainTimeout = 3 * time.Second
	}

	for i := 0; i < r.workerCount; i++ {
		r.wg.Add(1)
		go r.runWorker(i)
	}
	r.wg.Add(1)
	go r.runBucketGC()
	defer func() {
		close(r.stopCh)
		r.wg.Wait()
	}()

	consumerGroup := config.GetConfig().KafkaConfig.ResolveConsumerGroup()
	controlStopCh := make(chan struct{})
	controlDoneCh := make(chan struct{})
	go r.runControlPlane(consumerGroup, controlStopCh, controlDoneCh)
	err := r.runDataPlane(consumerGroup)
	close(controlStopCh)
	<-controlDoneCh
	return err
}

func (r *conversationBucketClaimRunner) runControlPlane(consumerGroup string, stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)
	var asyncCommitCh chan int
	var asyncCommitDone chan struct{}
	if r.asyncCommit {
		asyncCommitCh = make(chan int, 1024)
		asyncCommitDone = make(chan struct{})
		go func() {
			defer close(asyncCommitDone)
			for commitCount := range asyncCommitCh {
				commitStart := time.Now()
				r.session.Commit()
				duration := time.Since(commitStart)
				pressure.ObserveBenchmarkSystemEventAt("consumer_loop_commit_done", commitStart, map[string]interface{}{
					"topic":       r.claim.Topic(),
					"partition":   r.claim.Partition(),
					"count":       commitCount,
					"duration_ms": float64(duration.Microseconds()) / 1000.0,
					"async":       true,
				})
				observability.ObserveKafkaOffsetCommit(
					consumerGroup,
					r.claim.Topic(),
					r.claim.Partition(),
					duration,
					commitCount,
				)
			}
		}()
		defer func() {
			close(asyncCommitCh)
			<-asyncCommitDone
		}()
	}
	commitTimer := time.NewTimer(r.commitInterval)
	if !commitTimer.Stop() {
		select {
		case <-commitTimer.C:
		default:
		}
	}
	defer commitTimer.Stop()

	pendingCommitCount := 0
	var commitTimerC <-chan time.Time
	stopCommitTimer := func() {
		if commitTimerC == nil {
			return
		}
		if !commitTimer.Stop() {
			select {
			case <-commitTimer.C:
			default:
			}
		}
		commitTimerC = nil
	}
	armCommitTimer := func() {
		if pendingCommitCount == 0 {
			stopCommitTimer()
			return
		}
		stopCommitTimer()
		commitTimer.Reset(r.commitInterval)
		commitTimerC = commitTimer.C
	}
	flushPending := func() {
		if pendingCommitCount == 0 {
			return
		}
		stopCommitTimer()
		commitStart := time.Now()
		commitCount := pendingCommitCount
		pendingCommitCount = 0
		if r.asyncCommit {
			pressure.ObserveBenchmarkSystemEventAt("consumer_loop_async_commit_enqueue", commitStart, map[string]interface{}{
				"topic":     r.claim.Topic(),
				"partition": r.claim.Partition(),
				"count":     commitCount,
			})
			asyncCommitCh <- commitCount
			return
		}
		r.session.Commit()
		pressure.ObserveBenchmarkSystemEventAt("consumer_loop_commit_done", commitStart, map[string]interface{}{
			"topic":       r.claim.Topic(),
			"partition":   r.claim.Partition(),
			"count":       commitCount,
			"duration_ms": float64(time.Since(commitStart).Microseconds()) / 1000.0,
			"async":       false,
		})
		observability.ObserveKafkaOffsetCommit(
			consumerGroup,
			r.claim.Topic(),
			r.claim.Partition(),
			time.Since(commitStart),
			commitCount,
		)
	}
	defer flushPending()

	for {
		select {
		case <-stopCh:
			r.drainInflight(&pendingCommitCount, flushPending, armCommitTimer)
			return
		case <-commitTimerC:
			pressure.ObserveBenchmarkSystemEvent("consumer_loop_select_commit_timer", map[string]interface{}{
				"topic":                r.claim.Topic(),
				"partition":            r.claim.Partition(),
				"pending_commit_count": pendingCommitCount,
				"complete_ch_len":      len(r.completeCh),
				"backlog":              r.currentStateBacklog(),
				"split_control_plane":  true,
			})
			flushPending()
		case result := <-r.completeCh:
			r.handleComplete(result, &pendingCommitCount, flushPending, armCommitTimer)
		}
	}
}

func (r *conversationBucketClaimRunner) runDataPlane(consumerGroup string) error {
	laneCount := r.workerCount
	if laneCount < 1 {
		laneCount = 1
	}
	laneChs := make([]chan conversationBucketSessionLaneItem, laneCount)
	for i := range laneChs {
		laneChs[i] = make(chan conversationBucketSessionLaneItem, conversationBucketIngressQueueSize)
	}

	readerCount := conversationBucketKafkaReaderCount
	if readerCount < 1 {
		readerCount = 1
	}
	readerDoneCh := make(chan error, readerCount)
	laneDoneChs := make([]chan struct{}, laneCount)

	r.ensureNextMarkOffset(r.claim.InitialOffset())
	for i := range laneChs {
		laneDoneChs[i] = make(chan struct{})
		go r.runSessionLane(consumerGroup, i, laneChs[i], laneDoneChs[i])
	}
	for i := 0; i < readerCount; i++ {
		go r.runKafkaReader(consumerGroup, i, laneChs, readerDoneCh)
	}

	var err error
	for i := 0; i < readerCount; i++ {
		if readerErr := <-readerDoneCh; readerErr != nil && err == nil {
			err = readerErr
		}
	}
	for _, laneCh := range laneChs {
		close(laneCh)
	}
	for _, laneDoneCh := range laneDoneChs {
		<-laneDoneCh
	}
	return err
}

func (r *conversationBucketClaimRunner) runKafkaReader(consumerGroup string, readerID int, laneChs []chan conversationBucketSessionLaneItem, doneCh chan<- error) {
	messagesCh := r.claim.Messages()
	for {
		selectStart := time.Now()
		messagesLenBeforeSelect := len(messagesCh)
		completeLenBeforeSelect := len(r.completeCh)
		backlogBeforeSelect := r.currentStateBacklog()
		select {
		case <-r.server.done:
			doneCh <- nil
			return
		case kafkaMessage, ok := <-messagesCh:
			if !ok {
				doneCh <- nil
				return
			}
			if kafkaMessage == nil {
				continue
			}
			fetchAt := time.Now()
			lane := sessionLaneForKafkaMessage(kafkaMessage, len(laneChs))
			laneCh := laneChs[lane]
			pressure.ObserveBenchmarkEventAt(messageIDFromKafkaHeaders(kafkaMessage), "consumer_decoder_shard_selected", fetchAt, map[string]interface{}{
				"topic":         kafkaMessage.Topic,
				"partition":     kafkaMessage.Partition,
				"offset":        kafkaMessage.Offset,
				"kafka_reader":  readerID,
				"decoder_shard": lane,
				"session_lane":  lane,
			})
			pressure.ObserveBenchmarkEventAt(messageIDFromKafkaHeaders(kafkaMessage), "consumer_session_lane_selected", fetchAt, map[string]interface{}{
				"topic":        kafkaMessage.Topic,
				"partition":    kafkaMessage.Partition,
				"offset":       kafkaMessage.Offset,
				"kafka_reader": readerID,
				"session_lane": lane,
			})
			pressure.ObserveBenchmarkEventAt(messageIDFromKafkaHeaders(kafkaMessage), "consumer_fetch_done", fetchAt, map[string]interface{}{
				"topic":                         kafkaMessage.Topic,
				"partition":                     kafkaMessage.Partition,
				"offset":                        kafkaMessage.Offset,
				"kafka_reader":                  readerID,
				"decoder_shard":                 lane,
				"session_lane":                  lane,
				"select_wait_ms":                float64(fetchAt.Sub(selectStart).Microseconds()) / 1000.0,
				"messages_ch_len_before_select": messagesLenBeforeSelect,
				"messages_ch_len_after_receive": len(messagesCh),
				"complete_ch_len_before_select": completeLenBeforeSelect,
				"backlog_before_select":         backlogBeforeSelect,
			})
			pressure.ObserveBenchmarkEventAt(messageIDFromKafkaHeaders(kafkaMessage), "consumer_ingress_enqueue_start", time.Now(), map[string]interface{}{
				"topic":          kafkaMessage.Topic,
				"partition":      kafkaMessage.Partition,
				"offset":         kafkaMessage.Offset,
				"kafka_reader":   readerID,
				"decoder_shard":  lane,
				"session_lane":   lane,
				"ingress_ch_len": len(laneCh),
			})
			select {
			case <-r.server.done:
				doneCh <- nil
				return
			case laneCh <- conversationBucketSessionLaneItem{
				kafkaMessage: kafkaMessage,
				readerID:     readerID,
				lane:         lane,
			}:
				pressure.ObserveBenchmarkEvent(messageIDFromKafkaHeaders(kafkaMessage), "consumer_ingress_enqueue_done", map[string]interface{}{
					"topic":          kafkaMessage.Topic,
					"partition":      kafkaMessage.Partition,
					"offset":         kafkaMessage.Offset,
					"kafka_reader":   readerID,
					"decoder_shard":  lane,
					"session_lane":   lane,
					"ingress_ch_len": len(laneCh),
				})
			}
			observability.ObserveKafkaConsumePull(consumerGroup, kafkaMessage.Topic, kafkaMessage.Partition, kafkaConsumerLag(r.claim, kafkaMessage))
		}
	}
}

func (r *conversationBucketClaimRunner) runSessionLane(consumerGroup string, lane int, laneCh <-chan conversationBucketSessionLaneItem, doneCh chan<- struct{}) {
	defer close(doneCh)
	for {
		select {
		case <-r.server.done:
			return
		case item, ok := <-laneCh:
			if !ok {
				return
			}
			kafkaMessage := item.kafkaMessage
			if kafkaMessage == nil {
				continue
			}
			r.handleSessionLaneMessage(consumerGroup, lane, laneCh, kafkaMessage)
		}
	}
}

func (r *conversationBucketClaimRunner) handleSessionLaneMessage(consumerGroup string, lane int, laneCh <-chan conversationBucketSessionLaneItem, kafkaMessage *sarama.ConsumerMessage) {
	r.ensureNextMarkOffset(kafkaMessage.Offset)

	for r.shouldPauseConsume() {
		select {
		case <-r.server.done:
			return
		case <-time.After(2 * time.Millisecond):
		}
	}

	messageID := messageIDFromKafkaHeaders(kafkaMessage)
	pressure.ObserveBenchmarkEvent(messageID, "consumer_ingress_dequeue", map[string]interface{}{
		"topic":          kafkaMessage.Topic,
		"partition":      kafkaMessage.Partition,
		"offset":         kafkaMessage.Offset,
		"decoder_shard":  lane,
		"session_lane":   lane,
		"ingress_ch_len": len(laneCh),
	})

	decoded, err := decodeConsumedMessage(kafkaMessage)
	pressure.ObserveBenchmarkEvent(messageIDFromDecodedMessage(messageID, decoded), "consumer_decoded_enqueue_start", map[string]interface{}{
		"topic":          kafkaMessage.Topic,
		"partition":      kafkaMessage.Partition,
		"offset":         kafkaMessage.Offset,
		"decoder_shard":  lane,
		"session_lane":   lane,
		"decoded_ch_len": 0,
	})
	pressure.ObserveBenchmarkEvent(messageIDFromDecodedMessage(messageID, decoded), "consumer_decoded_enqueue_done", map[string]interface{}{
		"topic":          kafkaMessage.Topic,
		"partition":      kafkaMessage.Partition,
		"offset":         kafkaMessage.Offset,
		"decoder_shard":  lane,
		"session_lane":   lane,
		"decoded_ch_len": 0,
	})

	r.routeDecodedFromSessionLane(consumerGroup, lane, kafkaMessage, decoded, err)
}

func (r *conversationBucketClaimRunner) routeDecodedFromSessionLane(consumerGroup string, lane int, kafkaMessage *sarama.ConsumerMessage, decoded decodedConsumedMessage, decodeErr error) {
	messageID := messageIDFromDecodedMessage(messageIDFromKafkaHeaders(kafkaMessage), decoded)
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decoded_dequeue", map[string]interface{}{
		"topic":          kafkaMessage.Topic,
		"partition":      kafkaMessage.Partition,
		"offset":         kafkaMessage.Offset,
		"decoder_shard":  lane,
		"session_lane":   lane,
		"decoded_ch_len": 0,
		"backlog":        r.currentStateBacklog(),
	})
	pressure.ObserveBenchmarkEvent(messageID, "consumer_dispatcher_dequeue", map[string]interface{}{
		"topic":          kafkaMessage.Topic,
		"partition":      kafkaMessage.Partition,
		"offset":         kafkaMessage.Offset,
		"decoder_shard":  lane,
		"session_lane":   lane,
		"decoded_ch_len": 0,
		"backlog":        r.currentStateBacklog(),
	})

	if decodeErr != nil {
		stage, retryable := consumerErrorMeta(decodeErr)
		if !retryable {
			if dlqErr := writeKafkaMessageToDLQ(kafkaMessage, nil, stage, false, decodeErr); dlqErr != nil {
				zlog.Error(dlqErr.Error())
			}
		}
		observability.ObserveKafkaConsumeFailure(consumerGroup, kafkaMessage.Topic, kafkaMessage.Partition, stage)
		if retryable {
			observability.ObserveKafkaConsumeHandled(consumerGroup, kafkaMessage.Topic, kafkaMessage.Partition, "retryable_failure")
			return
		}
		observability.ObserveKafkaConsumeHandled(consumerGroup, kafkaMessage.Topic, kafkaMessage.Partition, "non_retryable_failure")
		r.incrementInflight()
		r.completeCh <- &conversationBucketProcessResult{
			job: &conversationBucketJob{
				kafkaMessage: kafkaMessage,
			},
			err:       decodeErr,
			markable:  true,
			retryable: false,
		}
		return
	}

	pressure.ObserveBenchmarkEvent(decoded.messageID, "consumer_route_enqueue_start", map[string]interface{}{
		"topic":            kafkaMessage.Topic,
		"partition":        kafkaMessage.Partition,
		"offset":           kafkaMessage.Offset,
		"decoder_shard":    lane,
		"session_lane":     lane,
		"conversation_key": decoded.conversationKey,
		"route_ch_len":     0,
		"backlog":          r.currentStateBacklog(),
	})
	pressure.ObserveBenchmarkEvent(decoded.messageID, "consumer_route_enqueue_done", map[string]interface{}{
		"topic":            kafkaMessage.Topic,
		"partition":        kafkaMessage.Partition,
		"offset":           kafkaMessage.Offset,
		"decoder_shard":    lane,
		"session_lane":     lane,
		"conversation_key": decoded.conversationKey,
		"route_ch_len":     0,
		"backlog":          r.currentStateBacklog(),
	})

	for {
		for r.shouldPauseConsume() {
			select {
			case <-r.server.done:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}

		routeAt := time.Now()
		pressure.ObserveBenchmarkEventAt(decoded.messageID, "consumer_route_dequeue", routeAt, map[string]interface{}{
			"topic":            kafkaMessage.Topic,
			"partition":        kafkaMessage.Partition,
			"offset":           kafkaMessage.Offset,
			"decoder_shard":    lane,
			"session_lane":     lane,
			"conversation_key": decoded.conversationKey,
			"route_ch_len":     0,
			"backlog":          r.currentStateBacklog(),
		})

		if r.enqueue(decoded) {
			r.incrementInflight()
			return
		}

		select {
		case <-r.server.done:
			return
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func messageIDFromDecodedMessage(fallback string, decoded decodedConsumedMessage) string {
	if decoded.messageID != "" {
		return decoded.messageID
	}
	return fallback
}

func sessionLaneForKafkaMessage(message *sarama.ConsumerMessage, laneCount int) int {
	if laneCount <= 1 || message == nil {
		return 0
	}
	key := message.Key
	if len(key) == 0 {
		key = []byte(messageIDFromKafkaHeaders(message))
	}
	if len(key) == 0 {
		return int(message.Offset % int64(laneCount))
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write(key)
	return int(hasher.Sum32() % uint32(laneCount))
}

func kafkaConsumerLag(claim sarama.ConsumerGroupClaim, message *sarama.ConsumerMessage) int64 {
	if claim == nil || message == nil {
		return 0
	}
	lag := claim.HighWaterMarkOffset() - message.Offset - 1
	if lag < 0 {
		return 0
	}
	return lag
}

func (r *conversationBucketClaimRunner) drainCompleteBatch(pendingCommitCount *int, flushPending func(), armCommitTimer func()) int {
	if r.completeDrainBatch <= 0 {
		r.completeDrainBatch = conversationBucketCompleteDrainBatch
	}
	drained := 0
	for drained < r.completeDrainBatch {
		select {
		case result := <-r.completeCh:
			r.handleComplete(result, pendingCommitCount, flushPending, armCommitTimer)
			drained++
		default:
			return drained
		}
	}
	return drained
}

func (r *conversationBucketClaimRunner) drainInflight(pendingCommitCount *int, flushPending func(), armCommitTimer func()) {
	deadline := time.NewTimer(r.drainTimeout)
	defer deadline.Stop()
	for r.currentInflight() > 0 {
		select {
		case result := <-r.completeCh:
			r.handleComplete(result, pendingCommitCount, flushPending, armCommitTimer)
		case <-deadline.C:
			zlog.Error(
				"kafka.consume.conversation_bucket_drain_timeout",
				zap.String("event", "kafka.consume.conversation_bucket_drain_timeout"),
				zap.String("module", "chat.kafka"),
				zap.String("topic", r.claim.Topic()),
				zap.Int32("partition", r.claim.Partition()),
				zap.Int("inflight", r.currentInflight()),
			)
			return
		}
	}
}

func (r *conversationBucketClaimRunner) handleComplete(result *conversationBucketProcessResult, pendingCommitCount *int, flushPending func(), armCommitTimer func()) {
	if result == nil || result.job == nil || result.job.kafkaMessage == nil {
		return
	}
	r.decrementInflight()
	r.markMu.Lock()
	defer r.markMu.Unlock()
	r.completed[result.job.kafkaMessage.Offset] = result
	for {
		next, ok := r.completed[r.nextMarkOffset]
		if !ok {
			break
		}
		delete(r.completed, r.nextMarkOffset)
		if next.markable {
			r.session.MarkMessage(next.job.kafkaMessage, "")
			observability.ObserveKafkaConsumeMarked(
				config.GetConfig().KafkaConfig.ResolveConsumerGroup(),
				next.job.kafkaMessage.Topic,
				next.job.kafkaMessage.Partition,
			)
			*pendingCommitCount = *pendingCommitCount + 1
			if *pendingCommitCount >= r.commitBatchSize {
				flushPending()
			} else {
				armCommitTimer()
			}
		}
		r.nextMarkOffset++
	}
}

func (r *conversationBucketClaimRunner) ensureNextMarkOffset(offset int64) {
	r.markMu.Lock()
	defer r.markMu.Unlock()
	if r.nextMarkOffset < 0 {
		r.nextMarkOffset = offset
	}
}

func (r *conversationBucketClaimRunner) enqueue(decoded decodedConsumedMessage) bool {
	job := &conversationBucketJob{
		kafkaMessage: decoded.kafkaMessage,
		decoded:      decoded,
	}
	pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_bucket_enqueue", map[string]interface{}{
		"partition":        decoded.kafkaMessage.Partition,
		"offset":           decoded.kafkaMessage.Offset,
		"conversation_key": decoded.conversationKey,
	})

	bucket := r.getOrCreateBucket(decoded.conversationKey)
	bucket.mu.Lock()
	bucketQueueLenBefore := len(bucket.queue)
	if len(bucket.queue) >= r.maxBucketDepth {
		bucket.mu.Unlock()
		r.observeBucketGCSkipped("bucket_depth_limit")
		return false
	}
	bucket.queue = append(bucket.queue, job)
	bucketQueueLenAfter := len(bucket.queue)
	bucket.mu.Unlock()
	r.incrementQueued()
	pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_bucket_enqueue_done", map[string]interface{}{
		"partition":               decoded.kafkaMessage.Partition,
		"offset":                  decoded.kafkaMessage.Offset,
		"conversation_key":        decoded.conversationKey,
		"bucket_queue_len_before": bucketQueueLenBefore,
		"bucket_queue_len_after":  bucketQueueLenAfter,
		"total_backlog":           r.currentStateBacklog(),
	})
	r.enqueueBucketReady(bucket)
	return true
}

func (r *conversationBucketClaimRunner) getOrCreateBucket(conversationKey string) *conversationBucketState {
	now := time.Now().UnixNano()
	r.bucketsMu.Lock()
	if bucket, ok := r.buckets[conversationKey]; ok {
		r.lruMu.Lock()
		bucket.mu.Lock()
		r.touchBucketLocked(bucket, now)
		bucket.mu.Unlock()
		r.lruMu.Unlock()
		r.bucketsMu.Unlock()
		return bucket
	}
	bucket := &conversationBucketState{
		key:          conversationKey,
		queue:        make([]*conversationBucketJob, 0, config.GetConfig().KafkaConfig.ResolveConversationBucketQueueSize()),
		lastActiveAt: now,
	}
	r.lruMu.Lock()
	bucket.mu.Lock()
	r.attachNewBucketLocked(bucket, now)
	bucket.mu.Unlock()
	total := len(r.buckets)
	lruTotal := r.lruList.Len()
	r.lruMu.Unlock()
	r.bucketsMu.Unlock()
	r.observeBucketTotalConsistency("create", total, lruTotal)
	r.evictBucketsUntilWithinLimit()
	return bucket
}

func (r *conversationBucketClaimRunner) enqueueBucketReady(bucket *conversationBucketState) {
	var jobs []*conversationBucketJob
	bucket.mu.Lock()
	if bucket.running || bucket.queued || len(bucket.queue) == 0 {
		bucket.mu.Unlock()
		return
	}
	bucket.queued = true
	for _, job := range bucket.queue {
		if job.readyEnqueued {
			continue
		}
		job.readyEnqueued = true
		jobs = append(jobs, job)
	}
	bucket.mu.Unlock()

	for _, job := range jobs {
		pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_ready_enqueue", map[string]interface{}{
			"partition":        job.kafkaMessage.Partition,
			"offset":           job.kafkaMessage.Offset,
			"conversation_key": job.decoded.conversationKey,
			"ready_ch_len":     len(r.readyCh),
		})
	}

	select {
	case <-r.stopCh:
		return
	case r.readyCh <- bucket:
		for _, job := range jobs {
			pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_ready_enqueue_done", map[string]interface{}{
				"partition":        job.kafkaMessage.Partition,
				"offset":           job.kafkaMessage.Offset,
				"conversation_key": job.decoded.conversationKey,
				"ready_ch_len":     len(r.readyCh),
			})
		}
	}
}

func (r *conversationBucketClaimRunner) runWorker(workerIndex int) {
	defer r.wg.Done()
	for {
		select {
		case <-r.stopCh:
			return
		case bucket := <-r.readyCh:
			if bucket == nil {
				continue
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						r.logWorkerPanic(workerIndex, nil, recovered)
					}
				}()
				r.runBucketTurn(workerIndex, bucket)
			}()
		}
	}
}

func (r *conversationBucketClaimRunner) runBucketTurn(workerIndex int, bucket *conversationBucketState) {
	start := time.Now()
	processed := 0
	var currentJob *conversationBucketJob

	defer func() {
		if recovered := recover(); recovered != nil {
			r.logWorkerPanic(workerIndex, currentJob, recovered)
			if currentJob != nil {
				if currentJob.kafkaMessage != nil {
					panicErr := fmt.Errorf("conversation bucket panic: %v", recovered)
					if dlqErr := writeKafkaMessageToDLQ(currentJob.kafkaMessage, &currentJob.decoded, "conversation_bucket_worker_panic", false, panicErr); dlqErr != nil {
						zlog.Error(dlqErr.Error())
					}
				}
				select {
				case <-r.stopCh:
				case r.completeCh <- &conversationBucketProcessResult{
					job:       currentJob,
					err:       fmt.Errorf("conversation bucket panic: %v", recovered),
					markable:  true,
					retryable: false,
				}:
				}
			}

			bucket.mu.Lock()
			hasMore := len(bucket.queue) > 0
			bucket.running = false
			bucket.mu.Unlock()
			if hasMore {
				r.enqueueBucketReady(bucket)
			}
		}
	}()

	bucket.mu.Lock()
	bucket.queued = false
	bucket.running = true
	bucket.mu.Unlock()

	for processed < r.maxMessagesPerTurn && time.Since(start) < r.maxRunDuration {
		bucket.mu.Lock()
		if len(bucket.queue) == 0 {
			bucket.running = false
			bucket.mu.Unlock()
			return
		}
		job := bucket.queue[0]
		bucket.queue = bucket.queue[1:]
		currentJob = job
		bucket.mu.Unlock()
		r.decrementQueued()

		if !job.readyDequeued {
			job.readyDequeued = true
			pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_ready_dequeue", map[string]interface{}{
				"partition":        job.kafkaMessage.Partition,
				"offset":           job.kafkaMessage.Offset,
				"worker_index":     workerIndex,
				"conversation_key": job.decoded.conversationKey,
			})
		}
		pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_worker_start", map[string]interface{}{
			"partition":        job.kafkaMessage.Partition,
			"offset":           job.kafkaMessage.Offset,
			"worker_index":     workerIndex,
			"conversation_key": job.decoded.conversationKey,
		})
		result := r.safeProcessJob(workerIndex, job)
		select {
		case <-r.stopCh:
			return
		case r.completeCh <- result:
		}
		currentJob = nil
		processed++
	}

	bucket.mu.Lock()
	hasMore := len(bucket.queue) > 0
	bucket.running = false
	bucket.mu.Unlock()
	if hasMore {
		r.enqueueBucketReady(bucket)
	}
}

func (r *conversationBucketClaimRunner) safeProcessJob(workerIndex int, job *conversationBucketJob) (result *conversationBucketProcessResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logWorkerPanic(workerIndex, job, recovered)
			if job != nil && job.kafkaMessage != nil {
				panicErr := fmt.Errorf("conversation bucket panic: %v", recovered)
				if dlqErr := writeKafkaMessageToDLQ(job.kafkaMessage, &job.decoded, "conversation_bucket_worker_panic", false, panicErr); dlqErr != nil {
					zlog.Error(dlqErr.Error())
				}
			}
			result = &conversationBucketProcessResult{
				job:       job,
				err:       fmt.Errorf("conversation bucket panic: %v", recovered),
				markable:  true,
				retryable: false,
			}
		}
	}()
	return r.processJob(workerIndex, job)
}

func (r *conversationBucketClaimRunner) logWorkerPanic(workerIndex int, job *conversationBucketJob, recovered interface{}) {
	fields := []zap.Field{
		zap.String("event", "kafka.consume.conversation_bucket_worker_panic"),
		zap.String("module", "chat.kafka"),
		zap.String("topic", r.claim.Topic()),
		zap.Int32("partition", r.claim.Partition()),
		zap.Int("worker_index", workerIndex),
		zap.Any("panic", recovered),
		zap.ByteString("stack", debug.Stack()),
	}
	if job != nil {
		fields = append(fields,
			zap.String("conversation_key", job.decoded.conversationKey),
			zap.Int64("offset", job.kafkaMessage.Offset),
			zap.String("message_id", job.decoded.messageID),
		)
	}
	zlog.Error("kafka.consume.conversation_bucket_worker_panic", fields...)
}

func (r *conversationBucketClaimRunner) totalBacklog() int {
	return r.currentStateBacklog()
}

func (r *conversationBucketClaimRunner) updatePauseStateLocked(backlog int) {
	wasPaused := r.paused
	if !r.paused && backlog >= r.pauseThreshold {
		r.paused = true
	}
	if r.paused && backlog <= r.resumeThreshold {
		r.paused = false
	}
	if wasPaused == r.paused {
		return
	}
	zlog.Info(
		"kafka.consume.conversation_bucket_backpressure_state",
		zap.String("event", "kafka.consume.conversation_bucket_backpressure_state"),
		zap.String("module", "chat.kafka"),
		zap.String("topic", r.claim.Topic()),
		zap.Int32("partition", r.claim.Partition()),
		zap.Bool("paused", r.paused),
		zap.Int("backlog", backlog),
		zap.Int("pause_threshold", r.pauseThreshold),
		zap.Int("resume_threshold", r.resumeThreshold),
	)
}

func (r *conversationBucketClaimRunner) shouldPauseConsume() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.paused
}

func (r *conversationBucketClaimRunner) tryEnqueuePending() bool {
	decoded, ok := r.getPendingMessage()
	if !ok {
		return true
	}
	if !r.enqueue(decoded) {
		return false
	}
	r.clearPendingMessage()
	return true
}

func (r *conversationBucketClaimRunner) incrementInflight() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.inflight++
	r.updatePauseStateLocked(r.inflight + r.totalBucketQueued)
}

func (r *conversationBucketClaimRunner) decrementInflight() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.inflight > 0 {
		r.inflight--
	}
	r.updatePauseStateLocked(r.inflight + r.totalBucketQueued)
}

func (r *conversationBucketClaimRunner) incrementQueued() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.totalBucketQueued++
	r.updatePauseStateLocked(r.inflight + r.totalBucketQueued)
}

func (r *conversationBucketClaimRunner) decrementQueued() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.totalBucketQueued > 0 {
		r.totalBucketQueued--
	}
	r.updatePauseStateLocked(r.inflight + r.totalBucketQueued)
}

func (r *conversationBucketClaimRunner) currentInflight() int {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.inflight
}

func (r *conversationBucketClaimRunner) currentStateBacklog() int {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.inflight + r.totalBucketQueued
}

func (r *conversationBucketClaimRunner) hasPendingMessage() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.pendingMessage != nil
}

func (r *conversationBucketClaimRunner) getPendingMessage() (decodedConsumedMessage, bool) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.pendingMessage == nil {
		return decodedConsumedMessage{}, false
	}
	return *r.pendingMessage, true
}

func (r *conversationBucketClaimRunner) setPendingMessage(decoded *decodedConsumedMessage) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.pendingMessage = decoded
}

func (r *conversationBucketClaimRunner) clearPendingMessage() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.pendingMessage = nil
}

func (r *conversationBucketClaimRunner) runBucketGC() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.observeBucketGCScan()
			r.evictBucketsUntilWithinLimit()
			r.gcEvictIdleBuckets()
		}
	}
}

func (r *conversationBucketClaimRunner) touchBucketLocked(bucket *conversationBucketState, now int64) {
	bucket.lastActiveAt = now
	if bucket.lruElem != nil {
		r.lruList.MoveToFront(bucket.lruElem)
		return
	}
	bucket.lruElem = r.lruList.PushFront(bucket)
}

func (r *conversationBucketClaimRunner) attachNewBucketLocked(bucket *conversationBucketState, now int64) {
	bucket.lastActiveAt = now
	bucket.lruElem = r.lruList.PushFront(bucket)
	r.buckets[bucket.key] = bucket
}

func (r *conversationBucketClaimRunner) removeBucketLocked(bucket *conversationBucketState) {
	delete(r.buckets, bucket.key)
	if bucket.lruElem != nil {
		r.lruList.Remove(bucket.lruElem)
		bucket.lruElem = nil
	}
}

func (r *conversationBucketClaimRunner) isBucketEvictableLocked(bucket *conversationBucketState, now int64) bool {
	if bucket == nil {
		return false
	}
	if bucket.running || bucket.queued || len(bucket.queue) > 0 {
		return false
	}
	if now-bucket.lastActiveAt <= r.bucketIdleTTL.Nanoseconds() {
		return false
	}
	return true
}

func (r *conversationBucketClaimRunner) tryEvictOneFromTail(now int64) bool {
	r.bucketsMu.Lock()
	defer r.bucketsMu.Unlock()
	r.lruMu.Lock()
	defer r.lruMu.Unlock()
	for e := r.lruList.Back(); e != nil; e = e.Prev() {
		bucket, ok := e.Value.(*conversationBucketState)
		if !ok || bucket == nil {
			continue
		}
		bucket.mu.Lock()
		evictable := r.isBucketEvictableLocked(bucket, now)
		bucket.mu.Unlock()
		if !evictable {
			r.observeBucketGCSkipped("not_evictable")
			continue
		}
		if current, ok := r.buckets[bucket.key]; !ok || current != bucket {
			continue
		}
		r.removeBucketLocked(bucket)
		total := len(r.buckets)
		lruTotal := r.lruList.Len()
		r.observeBucketLRUEvict(total, lruTotal)
		return true
	}
	return false
}

func (r *conversationBucketClaimRunner) evictBucketsUntilWithinLimit() {
	for {
		total, _ := r.bucketCountsSnapshot()
		if total <= r.maxBuckets {
			r.observeBucketTotalConsistency("limit", total, -1)
			return
		}
		if !r.tryEvictOneFromTail(time.Now().UnixNano()) {
			r.observeBucketTotalConsistency("limit", total, -1)
			return
		}
	}
}

func (r *conversationBucketClaimRunner) gcEvictIdleBuckets() {
	now := time.Now().UnixNano()
	evicted := 0
	for evicted < r.maxGCPerRun {
		if !r.tryEvictOneFromTail(now) {
			r.observeBucketTotalConsistency("gc", -1, -1)
			return
		}
		evicted++
	}
	r.observeBucketTotalConsistency("gc", -1, -1)
}

func (r *conversationBucketClaimRunner) bucketCountsSnapshot() (int, int) {
	r.bucketsMu.Lock()
	defer r.bucketsMu.Unlock()
	r.lruMu.Lock()
	defer r.lruMu.Unlock()
	return len(r.buckets), r.lruList.Len()
}

func (r *conversationBucketClaimRunner) observeBucketTotalConsistency(stage string, total int, lruTotal int) {
	if total < 0 || lruTotal < 0 {
		total, lruTotal = r.bucketCountsSnapshot()
	}
	if total != lruTotal {
		zlog.Error(
			"kafka.consume.conversation_bucket_total_inconsistent",
			zap.String("event", "kafka.consume.conversation_bucket_total_inconsistent"),
			zap.String("module", "chat.kafka"),
			zap.String("topic", r.claim.Topic()),
			zap.Int32("partition", r.claim.Partition()),
			zap.String("stage", stage),
			zap.Int("bucket_total", total),
			zap.Int("lru_total", lruTotal),
		)
		r.observeBucketGCSkipped("inconsistent_total")
		return
	}
	observability.ObserveConversationBucketTotal(
		config.GetConfig().KafkaConfig.ResolveConsumerGroup(),
		r.claim.Topic(),
		r.claim.Partition(),
		total,
	)
}

func (r *conversationBucketClaimRunner) observeBucketLRUEvict(total int, lruTotal int) {
	observability.ObserveConversationBucketLRUEvict(
		config.GetConfig().KafkaConfig.ResolveConsumerGroup(),
		r.claim.Topic(),
		r.claim.Partition(),
	)
	r.observeBucketTotalConsistency("lru_evict", total, lruTotal)
}

func (r *conversationBucketClaimRunner) observeBucketGCScan() {
	observability.ObserveConversationBucketGCScan(
		config.GetConfig().KafkaConfig.ResolveConsumerGroup(),
		r.claim.Topic(),
		r.claim.Partition(),
	)
}

func (r *conversationBucketClaimRunner) observeBucketGCSkipped(reason string) {
	observability.ObserveConversationBucketGCSkipped(
		config.GetConfig().KafkaConfig.ResolveConsumerGroup(),
		r.claim.Topic(),
		r.claim.Partition(),
		reason,
	)
}

func (r *conversationBucketClaimRunner) processJob(workerIndex int, job *conversationBucketJob) *conversationBucketProcessResult {
	consumerGroup := config.GetConfig().KafkaConfig.ResolveConsumerGroup()
	topic := job.kafkaMessage.Topic
	partition := job.kafkaMessage.Partition

	// 可重试失败不再无限原地重试：受次数上限、指数退避和总耗时上限共同约束，
	// 预算耗尽后按不可重试处置（写死信 + 放行位点），避免一条毒丸消息永久钉住
	// 一个 worker 和一个会话桶，进而让整个分区停摆。
	outcome, stage, err, attempts := consumeWithRetryBudget(
		resolveConsumeRetryPolicy(),
		func() error {
			totalStart := time.Now()
			pressure.ObserveBenchmarkEvent(job.decoded.messageID, "conversation_dispatch_start", map[string]interface{}{
				"partition":        job.kafkaMessage.Partition,
				"offset":           job.kafkaMessage.Offset,
				"worker_index":     workerIndex,
				"conversation_key": job.decoded.conversationKey,
			})
			attemptErr := handleDecodedConsumedMessage(r.server, job.decoded)
			pressure.ObserveBenchmarkEvent(job.decoded.messageID, "consumer_total_done", map[string]interface{}{
				"partition":        job.kafkaMessage.Partition,
				"offset":           job.kafkaMessage.Offset,
				"worker_index":     workerIndex,
				"conversation_key": job.decoded.conversationKey,
			})
			observability.ObserveKafkaConsumerStageDuration(consumerGroup, topic, partition, "total", time.Since(totalStart))
			if attemptErr != nil {
				attemptStage, _ := consumerErrorMeta(attemptErr)
				observability.ObserveKafkaConsumeFailure(consumerGroup, topic, partition, attemptStage)
			}
			return attemptErr
		},
		func() bool {
			select {
			case <-r.stopCh:
				return true
			default:
				return false
			}
		},
	)

	switch outcome {
	case consumeRetrySucceeded:
		observability.ObserveKafkaConsumeHandled(consumerGroup, topic, partition, "success")
		return &conversationBucketProcessResult{job: job, markable: true}

	case consumeRetryStopped:
		// 停机：这条消息还没有了结，位点必须保持不动，留给下次重放。
		observability.ObserveKafkaConsumeHandled(consumerGroup, topic, partition, "retryable_failure")
		return &conversationBucketProcessResult{job: job, err: err, markable: false, retryable: true}

	case consumeRetryNonRetryable:
		if dlqErr := writeKafkaMessageToDLQ(job.kafkaMessage, &job.decoded, stage, false, err); dlqErr != nil {
			zlog.Error(dlqErr.Error())
		}
		observability.ObserveKafkaConsumeHandled(consumerGroup, topic, partition, "non_retryable_failure")
		return &conversationBucketProcessResult{job: job, err: err, markable: true, retryable: false}

	default: // consumeRetryExhausted
		logConsumeRetryBudgetExhausted("chat.kafka", job.kafkaMessage, stage, attempts, err)
		if dlqErr := writeKafkaMessageToDLQ(job.kafkaMessage, &job.decoded, stage, false, err); dlqErr != nil {
			zlog.Error(dlqErr.Error())
		}
		observability.ObserveKafkaConsumeHandled(consumerGroup, topic, partition, "retry_budget_exhausted")
		return &conversationBucketProcessResult{job: job, err: err, markable: true, retryable: false}
	}
}
