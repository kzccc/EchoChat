package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-redis/redis/v8"

	"echo_chat_server/internal/config"
	"echo_chat_server/internal/dto/request"
	"echo_chat_server/internal/dto/respond"
	"echo_chat_server/internal/model"
	"echo_chat_server/internal/observability"
	"echo_chat_server/internal/pressure"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/enum/message/message_status_enum"
	"echo_chat_server/pkg/enum/message/message_type_enum"
	"echo_chat_server/pkg/zlog"
)

type decodedConsumedMessage struct {
	kafkaMessage     *sarama.ConsumerMessage
	request          request.ChatMessageRequest
	messageID        string
	conversationKey  string
	normalizedAvatar string
}

func messageIDFromKafkaHeaders(kafkaMessage *sarama.ConsumerMessage) string {
	if kafkaMessage == nil {
		return ""
	}
	for _, header := range kafkaMessage.Headers {
		if string(header.Key) == "message_id" {
			return string(header.Value)
		}
	}
	return ""
}

func decodeConsumedMessage(kafkaMessage *sarama.ConsumerMessage) (decodedConsumedMessage, error) {
	data := kafkaMessage.Value
	var chatMessageReq request.ChatMessageRequest
	decodeStart := time.Now()
	messageID := messageIDFromKafkaHeaders(kafkaMessage)
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_start", decodeStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	jsonStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_json_start", jsonStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
		"bytes":     len(data),
	})
	if err := json.Unmarshal(data, &chatMessageReq); err != nil {
		observeKafkaConsumerStage(kafkaMessage, "deserialize", decodeStart)
		return decodedConsumedMessage{}, nonRetryableConsumerError("deserialize", err)
	}
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decode_json_done", map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	observeKafkaConsumerStage(kafkaMessage, "deserialize", decodeStart)

	ensureIDStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_ensure_id_start", ensureIDStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	ensureServerMessageID(&chatMessageReq)
	messageID = chatMessageReq.MessageId
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decode_ensure_id_done", map[string]interface{}{
		"topic":         kafkaMessage.Topic,
		"partition":     kafkaMessage.Partition,
		"offset":        kafkaMessage.Offset,
		"client_msg_id": chatMessageReq.ClientMsgID,
	})

	validateStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_validate_start", validateStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	if chatMessageReq.SendId == "" || chatMessageReq.ReceiveId == "" {
		return decodedConsumedMessage{}, nonRetryableConsumerError("route", errors.New("send_id or receive_id is empty"))
	}
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decode_validate_done", map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})

	registerStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_register_benchmark_start", registerStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	pressure.RegisterBenchmarkMessage(chatMessageReq.MessageId, chatMessageReq.Content)
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decode_register_benchmark_done", map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})

	conversationKeyStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_conversation_key_start", conversationKeyStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	conversationKey := model.BuildConversationKey(chatMessageReq.SendId, chatMessageReq.ReceiveId)
	pressure.ObserveBenchmarkEvent(messageID, "consumer_decode_conversation_key_done", map[string]interface{}{
		"topic":            kafkaMessage.Topic,
		"partition":        kafkaMessage.Partition,
		"offset":           kafkaMessage.Offset,
		"conversation_key": conversationKey,
	})

	buildStart := time.Now()
	pressure.ObserveBenchmarkEventAt(messageID, "consumer_decode_build_result_start", buildStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	normalizedAvatar := normalizePath(chatMessageReq.SendAvatar)
	pressure.ObserveBenchmarkEvent(chatMessageReq.MessageId, "consumer_decode_done", map[string]interface{}{
		"topic":            kafkaMessage.Topic,
		"partition":        kafkaMessage.Partition,
		"offset":           kafkaMessage.Offset,
		"conversation_key": conversationKey,
	})
	return decodedConsumedMessage{
		kafkaMessage:     kafkaMessage,
		request:          chatMessageReq,
		messageID:        chatMessageReq.MessageId,
		conversationKey:  conversationKey,
		normalizedAvatar: normalizedAvatar,
	}, nil
}

func handleDecodedConsumedMessage(k *KafkaServer, decoded decodedConsumedMessage) error {
	kafkaMessage := decoded.kafkaMessage
	chatMessageReq := decoded.request

	seqStart := time.Now()
	pressure.ObserveBenchmarkEventAt(chatMessageReq.MessageId, "session_seq_start", seqStart, map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
	})
	sessionSeq, seqMode, err := nextMessageSessionSeqDetail(chatMessageReq.SendId, chatMessageReq.ReceiveId)
	observeKafkaConsumerStage(kafkaMessage, "session_seq", seqStart)
	if err != nil {
		if dlqErr := writeKafkaMessageToDLQ(kafkaMessage, &decoded, "session_seq", true, err); dlqErr != nil {
			zlog.Error(dlqErr.Error())
		}
		return retryableConsumerError("session_seq", err)
	}
	pressure.ObserveBenchmarkEvent(chatMessageReq.MessageId, "session_seq_done", map[string]interface{}{
		"topic":     kafkaMessage.Topic,
		"partition": kafkaMessage.Partition,
		"offset":    kafkaMessage.Offset,
		"seq_mode":  seqMode,
	})

	message := model.Message{
		Uuid:            decoded.messageID,
		SessionId:       chatMessageReq.SessionId,
		Type:            chatMessageReq.Type,
		Content:         chatMessageReq.Content,
		Url:             chatMessageReq.Url,
		SendId:          chatMessageReq.SendId,
		SendName:        chatMessageReq.SendName,
		SendAvatar:      decoded.normalizedAvatar,
		ReceiveId:       chatMessageReq.ReceiveId,
		ConversationKey: decoded.conversationKey,
		FileSize:        chatMessageReq.FileSize,
		FileType:        chatMessageReq.FileType,
		FileName:        chatMessageReq.FileName,
		Status:          message_status_enum.Unsent,
		SessionSeq:      sessionSeq,
		CreatedAt:       time.Now(),
		AVdata:          chatMessageReq.AVdata,
	}

	switch chatMessageReq.Type {
	case message_type_enum.Text:
		message.Url = ""
		message.FileSize = "0B"
		message.FileType = ""
		message.FileName = ""
	case message_type_enum.File:
		message.Content = ""
	case message_type_enum.AudioOrVideo:
		message.Content = ""
		message.Url = ""
		message.FileSize = ""
		message.FileType = ""
		message.FileName = ""
	default:
		return nonRetryableConsumerError("route", fmt.Errorf("unsupported message type: %d", chatMessageReq.Type))
	}

	shouldPersist := chatMessageReq.Type != message_type_enum.AudioOrVideo
	if chatMessageReq.Type == message_type_enum.AudioOrVideo {
		var avData request.AVData
		if err := json.Unmarshal([]byte(chatMessageReq.AVdata), &avData); err != nil {
			return nonRetryableConsumerError("deserialize", err)
		}
		shouldPersist = avData.MessageId == "PROXY" && (avData.Type == "start_call" || avData.Type == "receive_call" || avData.Type == "reject_call")
	}

	messageBackUUID := ""
	useAsyncPersist := config.GetConfig().KafkaConfig.UseAsyncPersist()
	useGroupAsyncPipeline := len(message.ReceiveId) > 0 &&
		message.ReceiveId[0] == 'G' &&
		config.GetConfig().KafkaConfig.UseGroupAsyncPipelineExperimental() &&
		!useAsyncPersist
	if shouldPersist && useAsyncPersist && !useGroupAsyncPipeline {
		messageListCacheStart := time.Now()
		if err := writeConsumedMessageListHotCache(message, chatMessageReq.SendAvatar); err != nil {
			observeKafkaConsumerStage(kafkaMessage, "message_list_hot_cache", messageListCacheStart)
			return retryableConsumerError("message_list_hot_cache", err)
		}
		observeKafkaConsumerStage(kafkaMessage, "message_list_hot_cache", messageListCacheStart)
	}
	if shouldPersist && !useGroupAsyncPipeline {
		if useAsyncPersist {
			hotCacheStart := time.Now()
			if _, err := writeMessageHotCache(&message); err != nil {
				observeKafkaConsumerStage(kafkaMessage, "message_hot_cache", hotCacheStart)
				return retryableConsumerError("message_hot_cache", err)
			}
			observeKafkaConsumerStage(kafkaMessage, "message_hot_cache", hotCacheStart)

			persistProduceStart := time.Now()
			if err := publishPersistInsert(&message); err != nil {
				observeKafkaConsumerStage(kafkaMessage, "persist_topic_produce", persistProduceStart)
				return retryableConsumerError("persist_topic_produce", err)
			}
			observeKafkaConsumerStage(kafkaMessage, "persist_topic_produce", persistProduceStart)
			messageBackUUID = message.Uuid
		} else {
			mysqlStart := time.Now()
			alreadyProcessed, persisted, err := saveKafkaMessage(&message)
			observeKafkaConsumerStage(kafkaMessage, "mysql_persist", mysqlStart)
			if err != nil {
				return retryableConsumerError("mysql_persist", err)
			}
			if alreadyProcessed {
				return nil
			}
			if persisted {
				messageBackUUID = message.Uuid
			}
		}
	}

	switch {
	case len(message.ReceiveId) == 0:
		if dlqErr := writeKafkaMessageToDLQ(kafkaMessage, &decoded, "route", false, errors.New("receive_id is empty")); dlqErr != nil {
			zlog.Error(dlqErr.Error())
		}
		return nonRetryableConsumerError("route", errors.New("receive_id is empty"))
	case message.ReceiveId[0] == 'U':
		switch chatMessageReq.Type {
		case message_type_enum.Text, message_type_enum.File:
			messageRsp := respond.GetMessageListRespond{
				MessageId:  message.Uuid,
				SendId:     message.SendId,
				SendName:   message.SendName,
				SendAvatar: chatMessageReq.SendAvatar,
				ReceiveId:  message.ReceiveId,
				Type:       message.Type,
				Content:    message.Content,
				Url:        message.Url,
				FileSize:   message.FileSize,
				FileName:   message.FileName,
				FileType:   message.FileType,
				SessionSeq: message.SessionSeq,
				CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
			}
			jsonMessage, err := json.Marshal(messageRsp)
			if err != nil {
				if dlqErr := writeDispatchFailureToDLQ("websocket_dispatch", message, chatMessageReq.SendAvatar, err); dlqErr != nil {
					zlog.Error(dlqErr.Error())
				}
				return retryableConsumerError("websocket_dispatch", err)
			}
			logMessageDispatch("kafka", "user", &message, len(jsonMessage))
			dispatchStart := time.Now()
			k.dispatchToKafkaRecipients([]string{message.ReceiveId, message.SendId}, jsonMessage, messageBackUUID, message.ConversationKey)
			observeKafkaConsumerStage(kafkaMessage, "websocket_dispatch", dispatchStart)
			completeIngressIdempotencyResult(message.SendId, chatMessageReq.ClientMsgID, mustMarshalIngressReplayPayload(messageRsp))
			invalidateSessionListCaches(message.SendId, message.ReceiveId)

			cacheKey := userMessageListCacheKey(message.SendId, message.ReceiveId, message.ConversationKey)
			redisReadStart := time.Now()
			rspString, redisErr := myredis.GetKeyNilIsErr(cacheKey)
			observeKafkaConsumerStage(kafkaMessage, "redis_read", redisReadStart)
			if redisErr == nil {
				var rsp []respond.GetMessageListRespond
				if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
					zlog.Error(err.Error())
				} else {
					rsp, _ = appendUniqueUserCacheMessage(cacheKey, rsp, messageRsp, "kafka")
					redisWriteStart := time.Now()
					err = setMessageListHotCache(cacheKey, rsp)
					observeKafkaConsumerStage(kafkaMessage, "redis_write", redisWriteStart)
					if err != nil {
						observability.ObserveKafkaConsumeFailure(config.GetConfig().KafkaConfig.ResolveConsumerGroup(), kafkaMessage.Topic, kafkaMessage.Partition, "redis_write")
						zlog.Error(err.Error())
					} else {
						logMessageCacheUpdate("kafka", cacheKey, len(rsp))
					}
				}
			} else if !errors.Is(redisErr, redis.Nil) {
				observability.ObserveKafkaConsumeFailure(config.GetConfig().KafkaConfig.ResolveConsumerGroup(), kafkaMessage.Topic, kafkaMessage.Partition, "redis_read")
				zlog.Error(redisErr.Error())
			}
		case message_type_enum.AudioOrVideo:
			messageRsp := respond.AVMessageRespond{
				MessageId:  message.Uuid,
				SendId:     message.SendId,
				SendName:   message.SendName,
				SendAvatar: message.SendAvatar,
				ReceiveId:  message.ReceiveId,
				Type:       message.Type,
				Content:    message.Content,
				Url:        message.Url,
				FileSize:   message.FileSize,
				FileName:   message.FileName,
				FileType:   message.FileType,
				SessionSeq: message.SessionSeq,
				CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
				AVdata:     message.AVdata,
			}
			jsonMessage, err := json.Marshal(messageRsp)
			if err != nil {
				if dlqErr := writeDispatchFailureToDLQ("websocket_dispatch", message, message.SendAvatar, err); dlqErr != nil {
					zlog.Error(dlqErr.Error())
				}
				return retryableConsumerError("websocket_dispatch", err)
			}
			logMessageDispatch("kafka", "user", &message, len(jsonMessage))
			dispatchStart := time.Now()
			k.dispatchToKafkaRecipients([]string{message.ReceiveId}, jsonMessage, messageBackUUID, message.ConversationKey)
			observeKafkaConsumerStage(kafkaMessage, "websocket_dispatch", dispatchStart)
			completeIngressIdempotencyResult(message.SendId, chatMessageReq.ClientMsgID, mustMarshalIngressReplayPayload(messageRsp))
			invalidateSessionListCaches(message.SendId, message.ReceiveId)
		}
	case message.ReceiveId[0] == 'G':
		if chatMessageReq.Type == message_type_enum.AudioOrVideo {
			if dlqErr := writeKafkaMessageToDLQ(kafkaMessage, &decoded, "route", false, errors.New("group audio/video is not supported")); dlqErr != nil {
				zlog.Error(dlqErr.Error())
			}
			return nonRetryableConsumerError("route", errors.New("group audio/video is not supported"))
		}
		if useGroupAsyncPipeline {
			enqueueStart := time.Now()
			if err := k.enqueueConsumedGroupMessage(message, chatMessageReq.SendAvatar, chatMessageReq.ClientMsgID); err != nil {
				observeKafkaConsumerStage(kafkaMessage, "group_async_enqueue", enqueueStart)
				return retryableConsumerError("group_async_enqueue", err)
			}
			observeKafkaConsumerStage(kafkaMessage, "group_async_enqueue", enqueueStart)
			return nil
		}
		if err := k.handleConsumedGroupMessage(kafkaMessage, &message, chatMessageReq.SendAvatar, messageBackUUID, chatMessageReq.ClientMsgID); err != nil {
			stage, retryable := consumerErrorMeta(err)
			if retryable && (stage == "group_member_query" || stage == "websocket_dispatch") {
				if dlqErr := writeDispatchFailureToDLQ(stage, message, chatMessageReq.SendAvatar, err); dlqErr != nil {
					zlog.Error(dlqErr.Error())
				}
			}
			return err
		}
		return nil
	default:
		if dlqErr := writeKafkaMessageToDLQ(kafkaMessage, &decoded, "route", false, fmt.Errorf("unsupported receive_id: %s", message.ReceiveId)); dlqErr != nil {
			zlog.Error(dlqErr.Error())
		}
		return nonRetryableConsumerError("route", fmt.Errorf("unsupported receive_id: %s", message.ReceiveId))
	}
	return nil
}
