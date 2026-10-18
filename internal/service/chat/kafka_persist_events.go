package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"echo_chat_server/internal/config"
	"echo_chat_server/internal/dao"
	"echo_chat_server/internal/model"
	"echo_chat_server/internal/observability"
	"echo_chat_server/internal/service/kafka"
	"echo_chat_server/pkg/enum/message/message_status_enum"
	"echo_chat_server/pkg/zlog"
)

const (
	persistEventInsert       = "message_insert"
	persistEventStatusUpdate = "message_status_update"
)

type kafkaPersistEvent struct {
	EventType       string        `json:"event_type"`
	MessageID       string        `json:"message_id"`
	ConversationKey string        `json:"conversation_key"`
	Message         model.Message `json:"message,omitempty"`
	Status          int8          `json:"status,omitempty"`
	Route           string        `json:"route,omitempty"`
	OccurredAt      time.Time     `json:"occurred_at"`
}

func publishPersistInsert(message *model.Message) error {
	if message == nil {
		return errors.New("persist insert message is nil")
	}
	if message.Uuid == "" {
		return errors.New("persist insert message uuid is empty")
	}
	if message.ConversationKey == "" {
		return fmt.Errorf("persist insert conversation_key is empty: message_id=%s", message.Uuid)
	}
	event := kafkaPersistEvent{
		EventType:       persistEventInsert,
		MessageID:       message.Uuid,
		ConversationKey: message.ConversationKey,
		Message:         *message,
		OccurredAt:      time.Now(),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return kafka.KafkaService.PublishPersistEvent(ctx, message.ConversationKey, payload, message.Uuid, persistEventInsert)
}

func publishPersistStatusUpdate(messageID, conversationKey, route string) error {
	if messageID == "" {
		return errors.New("persist status message_id is empty")
	}
	if conversationKey == "" {
		return fmt.Errorf("persist status conversation_key is empty: message_id=%s", messageID)
	}
	event := kafkaPersistEvent{
		EventType:       persistEventStatusUpdate,
		MessageID:       messageID,
		ConversationKey: conversationKey,
		Status:          message_status_enum.Sent,
		Route:           route,
		OccurredAt:      time.Now(),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return kafka.KafkaService.PublishPersistEvent(ctx, conversationKey, payload, messageID, persistEventStatusUpdate)
}

type kafkaPersistConsumerHandler struct{}

func (h *kafkaPersistConsumerHandler) Setup(sarama.ConsumerGroupSession) error {
	observability.ObserveKafkaConsumerSessionReady(config.GetConfig().KafkaConfig.ResolvePersistConsumerGroup(), true)
	return nil
}

func (h *kafkaPersistConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	observability.ObserveKafkaConsumerSessionReady(config.GetConfig().KafkaConfig.ResolvePersistConsumerGroup(), false)
	return nil
}

func (h *kafkaPersistConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	consumerGroup := config.GetConfig().KafkaConfig.ResolvePersistConsumerGroup()
	commitBatchSize := config.GetConfig().KafkaConfig.ResolveConsumerCommitBatchSize()
	commitInterval := config.GetConfig().KafkaConfig.ResolveConsumerCommitInterval()
	commitTimer := time.NewTimer(commitInterval)
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
		commitTimer.Reset(commitInterval)
		commitTimerC = commitTimer.C
	}
	flushPending := func() {
		if pendingCommitCount == 0 {
			return
		}
		stopCommitTimer()
		start := time.Now()
		session.Commit()
		observability.ObserveKafkaOffsetCommit(consumerGroup, claim.Topic(), claim.Partition(), time.Since(start), pendingCommitCount)
		pendingCommitCount = 0
	}
	defer flushPending()

	for {
		select {
		case <-KafkaChatServer.done:
			flushPending()
			return nil
		case <-commitTimerC:
			flushPending()
		case message, ok := <-claim.Messages():
			if !ok {
				flushPending()
				return nil
			}
			if message == nil {
				continue
			}
			lag := claim.HighWaterMarkOffset() - message.Offset - 1
			if lag < 0 {
				lag = 0
			}
			observability.ObserveKafkaConsumePull(consumerGroup, message.Topic, message.Partition, lag)
			if err := handlePersistConsumerMessage(message); err != nil {
				observability.ObserveKafkaConsumeFailure(consumerGroup, message.Topic, message.Partition, "persist_event")
				observability.ObserveKafkaConsumeHandled(consumerGroup, message.Topic, message.Partition, "retryable_failure")
				zlog.Error(
					"kafka.persist.consume.failed",
					zap.String("event", "kafka.persist.consume.failed"),
					zap.String("module", "chat.kafka.persist"),
					zap.String("topic", message.Topic),
					zap.Int32("partition", message.Partition),
					zap.Int64("offset", message.Offset),
					zap.String("error", err.Error()),
				)
				return err
			}
			observability.ObserveKafkaConsumeHandled(consumerGroup, message.Topic, message.Partition, "success")
			session.MarkMessage(message, "")
			observability.ObserveKafkaConsumeMarked(consumerGroup, message.Topic, message.Partition)
			pendingCommitCount++
			if pendingCommitCount >= commitBatchSize {
				flushPending()
				continue
			}
			armCommitTimer()
		}
	}
}

func handlePersistConsumerMessage(message *sarama.ConsumerMessage) error {
	var event kafkaPersistEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return err
	}
	switch event.EventType {
	case persistEventInsert:
		persistMessage := event.Message
		if persistMessage.Uuid == "" {
			persistMessage.Uuid = event.MessageID
		}
		if persistMessage.ConversationKey == "" {
			persistMessage.ConversationKey = event.ConversationKey
		}
		_, _, err := saveKafkaMessage(&persistMessage)
		return err
	case persistEventStatusUpdate:
		return persistMessageStatus(event)
	default:
		return fmt.Errorf("unsupported persist event type: %s", event.EventType)
	}
}

func persistMessageStatus(event kafkaPersistEvent) error {
	if event.MessageID == "" {
		return errors.New("persist status event message_id is empty")
	}
	status := event.Status
	if status == 0 {
		status = message_status_enum.Sent
	}
	start := time.Now()
	result := dao.GormDB.Model(&model.Message{}).
		Where("uuid = ?", event.MessageID).
		Update("status", status)
	duration := time.Since(start)
	if result.Error != nil {
		observability.ObserveWSStatusUpdate(event.Route, duration, "failure")
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := dao.GormDB.Model(&model.Message{}).Where("uuid = ?", event.MessageID).Count(&count).Error; err != nil {
			observability.ObserveWSStatusUpdate(event.Route, duration, "failure")
			return err
		}
		if count == 0 {
			observability.ObserveWSStatusUpdate(event.Route, duration, "failure")
			return gorm.ErrRecordNotFound
		}
	}
	observability.ObserveWSStatusUpdate(event.Route, duration, "success")
	return nil
}
