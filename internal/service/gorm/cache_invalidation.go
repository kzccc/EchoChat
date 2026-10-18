package gorm

import (
	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/zlog"
)

func invalidateUserContactCaches(userIDs ...string) {
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if err := myredis.DelKeysWithPattern("contact_user_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("session_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
	}
}

func invalidateGroupMembershipCaches(groupID string, userIDs ...string) {
	if groupID != "" {
		if err := myredis.DelKeysWithPattern("group_info_" + groupID); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("group_memberlist_" + groupID); err != nil {
			zlog.Error(err.Error())
		}
	}
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if err := myredis.DelKeysWithPattern("my_joined_group_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("group_session_list_" + userID); err != nil {
			zlog.Error(err.Error())
		}
	}
}

func invalidateGroupForAllMembers(groupID string, members []string) {
	if groupID != "" {
		if err := myredis.DelKeysWithPattern("group_info_" + groupID); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("group_memberlist_" + groupID); err != nil {
			zlog.Error(err.Error())
		}
	}
	for _, member := range members {
		invalidateGroupMembershipCaches("", member)
	}
}

func invalidateUserProfileCaches(userIDs ...string) {
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if err := myredis.DelKeysWithPattern("user_info_" + userID); err != nil {
			zlog.Error(err.Error())
		}
	}
}

func invalidateSessionOwnerCachesFromSessions(sessions []model.Session) {
	seen := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		if session.SendId == "" {
			continue
		}
		if _, ok := seen[session.SendId]; ok {
			continue
		}
		seen[session.SendId] = struct{}{}
		if err := myredis.DelKeysWithPattern("session_list_" + session.SendId); err != nil {
			zlog.Error(err.Error())
		}
		if err := myredis.DelKeysWithPattern("group_session_list_" + session.SendId); err != nil {
			zlog.Error(err.Error())
		}
	}
}

func membersFromSessions(sessions []model.Session) []string {
	members := make([]string, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		if session.SendId == "" {
			continue
		}
		if _, ok := seen[session.SendId]; ok {
			continue
		}
		seen[session.SendId] = struct{}{}
		members = append(members, session.SendId)
	}
	return members
}
