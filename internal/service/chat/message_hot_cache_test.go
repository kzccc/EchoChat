package chat

import (
	"fmt"
	"os"
	"testing"
	"time"

	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/enum/message/message_status_enum"
	"echo_chat_server/pkg/enum/message/message_type_enum"
)

func TestMessageHotCacheWrite100(t *testing.T) {
	if os.Getenv("ECHOCHAT_HOT_CACHE_WRITE_BENCH") != "1" {
		t.Skip("set ECHOCHAT_HOT_CACHE_WRITE_BENCH=1 to run the Redis hot-cache write bench")
	}

	const total = 100
	conversationKey := fmt.Sprintf("bench:message_hot:%d", time.Now().UnixNano())
	defer func() {
		if err := myredis.DelKeysWithPattern("message_hot:" + conversationKey + ":*"); err != nil {
			t.Logf("cleanup hot cache keys failed: %v", err)
		}
	}()

	var sum time.Duration
	var min time.Duration
	var max time.Duration
	for i := 1; i <= total; i++ {
		message := &model.Message{
			Uuid:            fmt.Sprintf("bench_hot_%06d", i),
			SessionId:       "bench_session",
			Type:            message_type_enum.Text,
			Content:         "hot cache bench message",
			SendId:          "U_bench_sender",
			SendName:        "bench",
			SendAvatar:      "",
			ReceiveId:       "U_bench_receiver",
			ConversationKey: conversationKey,
			Status:          message_status_enum.Unsent,
			SessionSeq:      int64(i),
			CreatedAt:       time.Now(),
		}
		elapsed, err := writeMessageHotCache(message)
		if err != nil {
			t.Fatalf("write hot cache failed at message %d: %v", i, err)
		}
		if i == 1 || elapsed < min {
			min = elapsed
		}
		if elapsed > max {
			max = elapsed
		}
		sum += elapsed
	}

	t.Logf("message hot cache Redis SET bench: count=%d avg=%s min=%s max=%s", total, sum/total, min, max)
}
