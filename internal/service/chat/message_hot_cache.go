package chat

import (
	"encoding/json"
	"fmt"
	"time"

	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
)

const messageHotCacheTTL = 24 * time.Hour

func messageHotCacheKey(conversationKey string, sessionSeq int64) string {
	return fmt.Sprintf("message_hot:%s:%d", conversationKey, sessionSeq)
}

func writeMessageHotCache(message *model.Message) (time.Duration, error) {
	if message == nil || message.ConversationKey == "" || message.SessionSeq <= 0 {
		return 0, nil
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	err = myredis.SetKeyEx(messageHotCacheKey(message.ConversationKey, message.SessionSeq), string(payload), messageHotCacheTTL)
	return time.Since(start), err
}
