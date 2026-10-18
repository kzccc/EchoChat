package chat

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-redis/redis/v8"

	"echo_chat_server/internal/dto/respond"
	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/constants"
)

const messageListHotCacheMaxItems = 100

func writeConsumedMessageListHotCache(message model.Message, sendAvatar string) error {
	if len(message.ReceiveId) == 0 {
		return nil
	}
	switch message.ReceiveId[0] {
	case 'U':
		cacheKey := userMessageListCacheKey(message.SendId, message.ReceiveId, message.ConversationKey)
		return writeUserMessageListHotCache(cacheKey, respond.GetMessageListRespond{
			MessageId:  message.Uuid,
			SendId:     message.SendId,
			SendName:   message.SendName,
			SendAvatar: sendAvatar,
			ReceiveId:  message.ReceiveId,
			Type:       message.Type,
			Content:    message.Content,
			Url:        message.Url,
			FileSize:   message.FileSize,
			FileName:   message.FileName,
			FileType:   message.FileType,
			SessionSeq: message.SessionSeq,
			CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
		}, "kafka_hot")
	case 'G':
		cacheKey := "group_messagelist_" + message.ReceiveId
		return writeGroupMessageListHotCache(cacheKey, respond.GetGroupMessageListRespond{
			MessageId:  message.Uuid,
			SendId:     message.SendId,
			SendName:   message.SendName,
			SendAvatar: sendAvatar,
			ReceiveId:  message.ReceiveId,
			Type:       message.Type,
			Content:    message.Content,
			Url:        message.Url,
			FileSize:   message.FileSize,
			FileName:   message.FileName,
			FileType:   message.FileType,
			SessionSeq: message.SessionSeq,
			CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
		}, "kafka_hot")
	default:
		return nil
	}
}

func userMessageListCacheKey(sendID, receiveID, conversationKey string) string {
	if conversationKey == "" {
		conversationKey = model.BuildConversationKey(sendID, receiveID)
	}
	return "message_list_" + conversationKey
}

func writeUserMessageListHotCache(cacheKey string, message respond.GetMessageListRespond, mode string) error {
	var list []respond.GetMessageListRespond
	rspString, err := myredis.GetKeyNilIsErr(cacheKey)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(rspString), &list); err != nil {
			return err
		}
	}
	list, _ = appendUniqueUserCacheMessage(cacheKey, list, message, mode)
	return setMessageListHotCache(cacheKey, list)
}

func writeGroupMessageListHotCache(cacheKey string, message respond.GetGroupMessageListRespond, mode string) error {
	var list []respond.GetGroupMessageListRespond
	rspString, err := myredis.GetKeyNilIsErr(cacheKey)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(rspString), &list); err != nil {
			return err
		}
	}
	list, _ = appendUniqueGroupCacheMessage(cacheKey, list, message, mode)
	return setMessageListHotCache(cacheKey, list)
}

func setMessageListHotCache(cacheKey string, list any) error {
	list = trimMessageListHotCache(list)
	payload, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return myredis.SetKeyEx(cacheKey, string(payload), time.Minute*constants.REDIS_TIMEOUT)
}

func trimMessageListHotCache(list any) any {
	switch typed := list.(type) {
	case []respond.GetMessageListRespond:
		slices.SortFunc(typed, func(a, b respond.GetMessageListRespond) int {
			if a.SessionSeq < b.SessionSeq {
				return -1
			}
			if a.SessionSeq > b.SessionSeq {
				return 1
			}
			return 0
		})
		if len(typed) > messageListHotCacheMaxItems {
			return typed[len(typed)-messageListHotCacheMaxItems:]
		}
		return typed
	case []respond.GetGroupMessageListRespond:
		slices.SortFunc(typed, func(a, b respond.GetGroupMessageListRespond) int {
			if a.SessionSeq < b.SessionSeq {
				return -1
			}
			if a.SessionSeq > b.SessionSeq {
				return 1
			}
			return 0
		})
		if len(typed) > messageListHotCacheMaxItems {
			return typed[len(typed)-messageListHotCacheMaxItems:]
		}
		return typed
	default:
		return list
	}
}
