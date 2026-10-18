package gorm

// 本文件实现 session service 相关的业务服务，负责组织数据库、缓存和业务规则。

import (
	"echo_chat_server/internal/dao"
	"echo_chat_server/internal/dto/request"
	"echo_chat_server/internal/dto/respond"
	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/constants"
	"echo_chat_server/pkg/enum/contact/contact_status_enum"
	"echo_chat_server/pkg/enum/group_info/group_status_enum"
	"echo_chat_server/pkg/enum/user_info/user_status_enum"
	"echo_chat_server/pkg/util/random"
	"echo_chat_server/pkg/zlog"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type sessionService struct {
}

// SessionService 是会话相关业务的统一入口。
var SessionService = new(sessionService)

// CreateSession 创建会话
func (s *sessionService) CreateSession(req request.CreateSessionRequest) (string, string, int) {
	var user model.UserInfo
	if res := dao.GormDB.Where("uuid = ?", req.SendId).First(&user); res.Error != nil {
		zlog.Error(res.Error.Error())
		return constants.SYSTEM_ERROR, "", -1
	}
	var session model.Session
	session.Uuid = fmt.Sprintf("S%s", random.GetNowAndLenRandomString(11))
	session.SendId = req.SendId
	session.ReceiveId = req.ReceiveId
	session.CreatedAt = time.Now()
	if req.ReceiveId[0] == 'U' {
		// 单聊场景下需要检查目标用户状态并回填昵称、头像。
		var receiveUser model.UserInfo
		if res := dao.GormDB.Where("uuid = ?", req.ReceiveId).First(&receiveUser); res.Error != nil {
			zlog.Error(res.Error.Error())
			return constants.SYSTEM_ERROR, "", -1
		}
		if receiveUser.Status == user_status_enum.DISABLE {
			zlog.Error("该用户被禁用了")
			return "该用户被禁用了", "", -2
		} else {
			session.ReceiveName = receiveUser.Nickname
			session.Avatar = receiveUser.Avatar
		}
	} else {
		// 群聊场景下需要检查群状态并把群资料拷贝进会话快照。
		var receiveGroup model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", req.ReceiveId).First(&receiveGroup); res.Error != nil {
			zlog.Error(res.Error.Error())
			return constants.SYSTEM_ERROR, "", -1
		}
		if receiveGroup.Status == group_status_enum.DISABLE {
			zlog.Error("该群聊被禁用了")
			return "该群聊被禁用了", "", -2
		} else {
			session.ReceiveName = receiveGroup.Name
			session.Avatar = receiveGroup.Avatar
		}
	}

	if res := dao.GormDB.Create(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return constants.SYSTEM_ERROR, "", -1
	}
	// 新会话创建后，主动清理会话列表缓存，保证前端刷新能看到最新会话。
	if err := myredis.DelKeysWithPattern("group_session_list_" + req.SendId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("session_list_" + req.SendId); err != nil {
		zlog.Error(err.Error())
	}
	return "会话创建成功", session.Uuid, 0
}

// CheckOpenSessionAllowed 检查是否允许发起会话
func (s *sessionService) CheckOpenSessionAllowed(sendId, receiveId string) (string, bool, int) {
	var contact model.UserContact
	if res := dao.GormDB.Where("user_id = ? and contact_id = ?", sendId, receiveId).First(&contact); res.Error != nil {
		zlog.Error(res.Error.Error())
		return constants.SYSTEM_ERROR, false, -1
	}
	if contact.Status == contact_status_enum.BE_BLACK {
		return "已被对方拉黑，无法发起会话", false, -2
	} else if contact.Status == contact_status_enum.BLACK {
		return "已拉黑对方，先解除拉黑状态才能发起会话", false, -2
	}
	if receiveId[0] == 'U' {
		// 单聊时检查目标用户是否仍然可用。
		var user model.UserInfo
		if res := dao.GormDB.Where("uuid = ?", receiveId).First(&user); res.Error != nil {
			zlog.Error(res.Error.Error())
			return constants.SYSTEM_ERROR, false, -1
		}
		if user.Status == user_status_enum.DISABLE {
			zlog.Info("对方已被禁用，无法发起会话")
			return "对方已被禁用，无法发起会话", false, -2
		}
	} else {
		// 群聊时检查目标群是否处于可进入状态。
		var group model.GroupInfo
		if res := dao.GormDB.Where("uuid = ?", receiveId).First(&group); res.Error != nil {
			zlog.Error(res.Error.Error())
			return constants.SYSTEM_ERROR, false, -1
		}
		if group.Status == group_status_enum.DISABLE {
			zlog.Info("对方已被禁用，无法发起会话")
			return "对方已被禁用，无法发起会话", false, -2
		}
	}
	return "可以发起会话", true, 0
}

// unreadCountCap 是未读数的封顶值。
// 产品上未读数只需要精确到 99+，所以最多数这么多条就停，
// 让统计成本与会话积压深度无关。
const unreadCountCap = 100

// unreadCount 返回 ownerID 在某个会话下的未读消息数。
//
// 统计方式是「数消息行」，而不是「最新序号减已读序号」：
// session_seq 会因为重试、重放、进死信而出现空洞，用减法会把空洞算成未读，
// 会话越活跃虚高越明显。数行数则与序号连不连续无关，结果始终准确。
//
// 统计口径：会话内、序号大于该用户已读位置、且不是该用户自己发送的消息。
// 返回值被 unreadCountCap 封顶，达到上限时前端按 99+ 展示。
func (s *sessionService) unreadCount(ownerID, conversationKey string) int64 {
	if ownerID == "" || conversationKey == "" {
		return 0
	}

	// 查不到已读记录表示该用户从未读过这个会话，按 0 处理（全部未读）。
	lastReadSeq := int64(0)
	var readState model.UserReadState
	if err := dao.GormDB.
		Where("user_id = ? AND conversation_key = ?", ownerID, conversationKey).
		Take(&readState).Error; err == nil {
		lastReadSeq = readState.LastReadSeq
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		// 查询出错时保守返回 0，不要把错误当成"全部未读"抛给前端。
		zlog.Error(err.Error())
		return 0
	}

	var count int64
	// 用子查询 + LIMIT 封顶：外层 COUNT 只数内层最多 cap 行。
	countErr := dao.GormDB.Raw(
		"SELECT COUNT(*) FROM ("+
			"SELECT 1 FROM message "+
			"WHERE conversation_key = ? AND session_seq > ? AND send_id <> ? "+
			"LIMIT ?) AS t",
		conversationKey, lastReadSeq, ownerID, unreadCountCap,
	).Row().Scan(&count)
	if countErr != nil {
		zlog.Error(countErr.Error())
		return 0
	}
	return count
}

// MarkConversationRead 上报当前用户在某个会话内已经读到的最大 session_seq。
func (s *sessionService) MarkConversationRead(userID, conversationKey string, lastReadSeq int64) (string, int) {
	if userID == "" || conversationKey == "" || lastReadSeq <= 0 {
		return "请求参数错误", -2
	}
	// 校验上报的已读位置确实存在：客户端可能上报一个远大于当前最新序号的值，
	// 不做收敛的话这个会话的未读会被永久清零，而且之后所有新消息都追不上这个位置。
	var maxSeq int64
	if err := dao.GormDB.Model(&model.Message{}).
		Where("conversation_key = ?", conversationKey).
		Select("COALESCE(MAX(session_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, -1
	}
	if maxSeq > 0 && lastReadSeq > maxSeq {
		lastReadSeq = maxSeq
	}
	now := time.Now()
	readState := model.UserReadState{
		UserId:          userID,
		ConversationKey: conversationKey,
		LastReadSeq:     lastReadSeq,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := dao.GormDB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_read_seq": gorm.Expr("GREATEST(last_read_seq, VALUES(last_read_seq))"),
			"updated_at":    now,
		}),
	}).Create(&readState).Error; err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, -1
	}
	if err := myredis.DelKeysWithPattern("session_list_" + userID); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("group_session_list_" + userID); err != nil {
		zlog.Error(err.Error())
	}
	return "已读进度更新成功", 0
}

// DeleteSession 删除会话

// OpenSession 打开会话
func (s *sessionService) OpenSession(req request.OpenSessionRequest) (string, string, int) {
	rspString, err := myredis.GetKeyWithPrefixNilIsErr("session_" + req.SendId + "_" + req.ReceiveId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// 缓存未命中时优先查库；如果连数据库都没有，则现场创建一个新会话。
			var session model.Session
			if res := dao.GormDB.Where("send_id = ? and receive_id = ?", req.SendId, req.ReceiveId).First(&session); res.Error != nil {
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("会话没有找到，将新建会话")
					createReq := request.CreateSessionRequest{
						SendId:    req.SendId,
						ReceiveId: req.ReceiveId,
					}
					return s.CreateSession(createReq)
				}
			}
			//rspString, err := json.Marshal(session)
			//if err != nil {
			//	zlog.Error(err.Error())
			//}
			//if err := myredis.SetKeyEx("session_"+req.SendId+"_"+req.ReceiveId+"_"+session.Uuid, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
			//	zlog.Error(err.Error())
			//}
			return "会话创建成功", session.Uuid, 0
		} else {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, "", -1
		}
	}
	var session model.Session
	// 这里读取到的实际上是命中的 key 名，历史代码中对应 value 写回被注释了，因此只保留兼容处理。
	if err := json.Unmarshal([]byte(rspString), &session); err != nil {
		zlog.Error(err.Error())
	}
	return "会话创建成功", session.Uuid, 0
}

// GetUserSessionList 获取用户会话列表
func (s *sessionService) GetUserSessionList(ownerId string) (string, []respond.UserSessionListRespond, int) {
	rspString, err := myredis.GetKeyNilIsErr("session_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// 缓存未命中时读取该用户全部会话，再过滤出单聊项。
			var sessionList []model.Session
			if res := dao.GormDB.Order("created_at DESC").Where("send_id = ?", ownerId).Find(&sessionList); res.Error != nil {
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("未创建用户会话")
					return "未创建用户会话", nil, 0
				} else {
					zlog.Error(res.Error.Error())
					return constants.SYSTEM_ERROR, nil, -1
				}
			}
			var sessionListRsp []respond.UserSessionListRespond
			for i := 0; i < len(sessionList); i++ {
				if sessionList[i].ReceiveId[0] == 'U' {
					sessionListRsp = append(sessionListRsp, respond.UserSessionListRespond{
						SessionId:   sessionList[i].Uuid,
						Avatar:      sessionList[i].Avatar,
						UserId:      sessionList[i].ReceiveId,
						Username:    sessionList[i].ReceiveName,
						UnreadCount: s.unreadCount(ownerId, model.BuildConversationKey(sessionList[i].SendId, sessionList[i].ReceiveId)),
					})
				}
			}
			// 会话列表适合短时间缓存，避免每次切页都回查数据库。
			rspString, err := json.Marshal(sessionListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyEx("session_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			return "获取成功", sessionListRsp, 0
		} else {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, nil, -1
		}
	}
	var rsp []respond.UserSessionListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return "获取成功", rsp, 0
}

// GetGroupSessionList 获取群聊会话列表
func (s *sessionService) GetGroupSessionList(ownerId string) (string, []respond.GroupSessionListRespond, int) {
	rspString, err := myredis.GetKeyNilIsErr("group_session_list_" + ownerId)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// 群聊会话列表和单聊列表走同一张会话表，只在返回阶段做类型过滤。
			var sessionList []model.Session
			if res := dao.GormDB.Order("created_at DESC").Where("send_id = ?", ownerId).Find(&sessionList); res.Error != nil {
				if errors.Is(res.Error, gorm.ErrRecordNotFound) {
					zlog.Info("未创建群聊会话")
					return "未创建群聊会话", nil, 0
				} else {
					zlog.Error(res.Error.Error())
					return constants.SYSTEM_ERROR, nil, -1
				}
			}
			var sessionListRsp []respond.GroupSessionListRespond
			for i := 0; i < len(sessionList); i++ {
				if sessionList[i].ReceiveId[0] == 'G' {
					sessionListRsp = append(sessionListRsp, respond.GroupSessionListRespond{
						SessionId:   sessionList[i].Uuid,
						Avatar:      sessionList[i].Avatar,
						GroupId:     sessionList[i].ReceiveId,
						GroupName:   sessionList[i].ReceiveName,
						UnreadCount: s.unreadCount(ownerId, model.BuildConversationKey(sessionList[i].SendId, sessionList[i].ReceiveId)),
					})
				}
			}
			rspString, err := json.Marshal(sessionListRsp)
			if err != nil {
				zlog.Error(err.Error())
			}
			if err := myredis.SetKeyEx("group_session_list_"+ownerId, string(rspString), time.Minute*constants.REDIS_TIMEOUT); err != nil {
				zlog.Error(err.Error())
			}
			return "获取成功", sessionListRsp, 0
		} else {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, nil, -1
		}
	}
	var rsp []respond.GroupSessionListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		zlog.Error(err.Error())
	}
	return "获取成功", rsp, 0
}

// DeleteSession 删除会话
func (s *sessionService) DeleteSession(ownerId, sessionId string) (string, int) {
	var session model.Session
	if res := dao.GormDB.Where("uuid = ?", sessionId).Find(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return constants.SYSTEM_ERROR, -1
	}
	// 当前删除是软删除，保留历史记录，前端界面上不再展示即可。
	session.DeletedAt.Valid = true
	session.DeletedAt.Time = time.Now()
	if res := dao.GormDB.Save(&session); res.Error != nil {
		zlog.Error(res.Error.Error())
		return constants.SYSTEM_ERROR, -1
	}
	//if err := myredis.DelKeysWithSuffix(sessionId); err != nil {
	//	zlog.Error(err.Error())
	//}
	if err := myredis.DelKeysWithPattern("group_session_list_" + ownerId); err != nil {
		zlog.Error(err.Error())
	}
	if err := myredis.DelKeysWithPattern("session_list_" + ownerId); err != nil {
		zlog.Error(err.Error())
	}
	return "删除成功", 0
}
