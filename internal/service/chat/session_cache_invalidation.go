package chat

import (
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/zlog"
)

func invalidateSessionListCaches(userIDs ...string) {
	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		if err := myredis.DelKeysWithPattern("session_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("group_session_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
	}
}
