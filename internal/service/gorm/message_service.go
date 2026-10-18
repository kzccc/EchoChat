package gorm

// 本文件实现 message service 相关的业务服务，负责组织数据库、缓存和业务规则。

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"io"

	"echo_chat_server/internal/config"
	"echo_chat_server/internal/dao"
	"echo_chat_server/internal/dto/respond"
	"echo_chat_server/internal/model"
	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/constants"
	"echo_chat_server/pkg/zlog"
	"os"
	"path/filepath"
)

type messageService struct {
}

// MessageService 提供消息列表查询和文件上传能力。
var MessageService = new(messageService)

const (
	defaultMessagePageLimit = 30
	maxMessagePageLimit     = 100
)

// GetMessageList 获取聊天记录
func (m *messageService) GetMessageList(userOneId, userTwoId string, beforeSeq int64, limit int) (string, respond.GetMessageListPageRespond, int) {
	conversationKey := model.BuildConversationKey(userOneId, userTwoId)
	limit = normalizeMessagePageLimit(limit)
	zlog.Info(
		"message.query.start",
		zap.String("event", "message.query.start"),
		zap.String("module", "message.service"),
		zap.String("scope", "user"),
		zap.String("user_one_id", userOneId),
		zap.String("user_two_id", userTwoId),
		zap.Int64("before_seq", beforeSeq),
		zap.Int("limit", limit),
		zap.String("order_by", "session_seq DESC"),
	)

	if beforeSeq <= 0 {
		if page, ok, err := m.getUserMessageListFromCache(userOneId, userTwoId, limit); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, respond.GetMessageListPageRespond{}, -1
		} else if ok {
			m.logUserMessageQueryFinish("cache", userOneId, userTwoId, beforeSeq, limit, page)
			return "获取聊天记录成功", page, 0
		}
	}

	page, err := m.queryUserMessagePage(conversationKey, beforeSeq, limit)
	if err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, respond.GetMessageListPageRespond{}, -1
	}
	m.logUserMessageQueryFinish("db", userOneId, userTwoId, beforeSeq, limit, page)
	return "获取聊天记录成功", page, 0
}

// GetGroupMessageList 获取群聊消息记录
func (m *messageService) GetGroupMessageList(groupId string, beforeSeq int64, limit int) (string, respond.GetGroupMessageListPageRespond, int) {
	conversationKey := model.BuildConversationKey("", groupId)
	limit = normalizeMessagePageLimit(limit)
	zlog.Info(
		"message.query.start",
		zap.String("event", "message.query.start"),
		zap.String("module", "message.service"),
		zap.String("scope", "group"),
		zap.String("group_id", groupId),
		zap.Int64("before_seq", beforeSeq),
		zap.Int("limit", limit),
		zap.String("order_by", "session_seq DESC"),
	)

	if beforeSeq <= 0 {
		if page, ok, err := m.getGroupMessageListFromCache(groupId, limit); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, respond.GetGroupMessageListPageRespond{}, -1
		} else if ok {
			m.logGroupMessageQueryFinish("cache", groupId, beforeSeq, limit, page)
			return "获取聊天记录成功", page, 0
		}
	}

	page, err := m.queryGroupMessagePage(conversationKey, beforeSeq, limit)
	if err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, respond.GetGroupMessageListPageRespond{}, -1
	}
	m.logGroupMessageQueryFinish("db", groupId, beforeSeq, limit, page)
	return "获取聊天记录成功", page, 0
}

func normalizeMessagePageLimit(limit int) int {
	if limit <= 0 {
		return defaultMessagePageLimit
	}
	if limit > maxMessagePageLimit {
		return maxMessagePageLimit
	}
	return limit
}

func (m *messageService) queryUserMessagePage(conversationKey string, beforeSeq int64, limit int) (respond.GetMessageListPageRespond, error) {
	var messageList []model.Message
	query := dao.GormDB.Where("conversation_key = ?", conversationKey)
	if beforeSeq > 0 {
		query = query.Where("session_seq < ?", beforeSeq)
	}
	if res := query.Order("session_seq DESC").Limit(limit + 1).Find(&messageList); res.Error != nil {
		return respond.GetMessageListPageRespond{}, res.Error
	}

	hasMore := len(messageList) > limit
	if hasMore {
		messageList = messageList[:limit]
	}
	slices.Reverse(messageList)

	rspList := make([]respond.GetMessageListRespond, 0, len(messageList))
	for _, message := range messageList {
		rspList = append(rspList, buildUserMessageListRespond(message))
	}
	return respond.GetMessageListPageRespond{
		List:          rspList,
		NextBeforeSeq: oldestUserMessageSeq(rspList),
		HasMore:       hasMore,
		Limit:         limit,
	}, nil
}

func (m *messageService) queryGroupMessagePage(conversationKey string, beforeSeq int64, limit int) (respond.GetGroupMessageListPageRespond, error) {
	var messageList []model.Message
	query := dao.GormDB.Where("conversation_key = ?", conversationKey)
	if beforeSeq > 0 {
		query = query.Where("session_seq < ?", beforeSeq)
	}
	if res := query.Order("session_seq DESC").Limit(limit + 1).Find(&messageList); res.Error != nil {
		return respond.GetGroupMessageListPageRespond{}, res.Error
	}

	hasMore := len(messageList) > limit
	if hasMore {
		messageList = messageList[:limit]
	}
	slices.Reverse(messageList)

	rspList := make([]respond.GetGroupMessageListRespond, 0, len(messageList))
	for _, message := range messageList {
		rspList = append(rspList, buildGroupMessageListRespond(message))
	}
	return respond.GetGroupMessageListPageRespond{
		List:          rspList,
		NextBeforeSeq: oldestGroupMessageSeq(rspList),
		HasMore:       hasMore,
		Limit:         limit,
	}, nil
}

func (m *messageService) getUserMessageListFromCache(userOneId, userTwoId string, limit int) (respond.GetMessageListPageRespond, bool, error) {
	rspString, err := myredis.GetKeyNilIsErr("message_list_" + model.BuildConversationKey(userOneId, userTwoId))
	if errors.Is(err, redis.Nil) {
		return respond.GetMessageListPageRespond{}, false, nil
	}
	if err != nil {
		return respond.GetMessageListPageRespond{}, false, err
	}

	var rsp []respond.GetMessageListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		return respond.GetMessageListPageRespond{}, false, err
	}
	if len(rsp) < limit {
		return respond.GetMessageListPageRespond{}, false, nil
	}
	slices.SortFunc(rsp, func(a, b respond.GetMessageListRespond) int {
		if a.SessionSeq < b.SessionSeq {
			return -1
		}
		if a.SessionSeq > b.SessionSeq {
			return 1
		}
		return 0
	})
	if len(rsp) > limit {
		rsp = rsp[len(rsp)-limit:]
	}
	return respond.GetMessageListPageRespond{
		List:          rsp,
		NextBeforeSeq: oldestUserMessageSeq(rsp),
		HasMore:       true,
		Limit:         limit,
	}, true, nil
}

func (m *messageService) getGroupMessageListFromCache(groupId string, limit int) (respond.GetGroupMessageListPageRespond, bool, error) {
	rspString, err := myredis.GetKeyNilIsErr("group_messagelist_" + groupId)
	if errors.Is(err, redis.Nil) {
		return respond.GetGroupMessageListPageRespond{}, false, nil
	}
	if err != nil {
		return respond.GetGroupMessageListPageRespond{}, false, err
	}

	var rsp []respond.GetGroupMessageListRespond
	if err := json.Unmarshal([]byte(rspString), &rsp); err != nil {
		return respond.GetGroupMessageListPageRespond{}, false, err
	}
	if len(rsp) < limit {
		return respond.GetGroupMessageListPageRespond{}, false, nil
	}
	slices.SortFunc(rsp, func(a, b respond.GetGroupMessageListRespond) int {
		if a.SessionSeq < b.SessionSeq {
			return -1
		}
		if a.SessionSeq > b.SessionSeq {
			return 1
		}
		return 0
	})
	if len(rsp) > limit {
		rsp = rsp[len(rsp)-limit:]
	}
	return respond.GetGroupMessageListPageRespond{
		List:          rsp,
		NextBeforeSeq: oldestGroupMessageSeq(rsp),
		HasMore:       true,
		Limit:         limit,
	}, true, nil
}

func buildUserMessageListRespond(message model.Message) respond.GetMessageListRespond {
	return respond.GetMessageListRespond{
		MessageId:  message.Uuid,
		SendId:     message.SendId,
		SendName:   message.SendName,
		SendAvatar: message.SendAvatar,
		ReceiveId:  message.ReceiveId,
		Content:    message.Content,
		Url:        message.Url,
		Type:       message.Type,
		FileType:   message.FileType,
		FileName:   message.FileName,
		FileSize:   message.FileSize,
		SessionSeq: message.SessionSeq,
		CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
	}
}

func buildGroupMessageListRespond(message model.Message) respond.GetGroupMessageListRespond {
	return respond.GetGroupMessageListRespond{
		MessageId:  message.Uuid,
		SendId:     message.SendId,
		SendName:   message.SendName,
		SendAvatar: message.SendAvatar,
		ReceiveId:  message.ReceiveId,
		Content:    message.Content,
		Url:        message.Url,
		Type:       message.Type,
		FileType:   message.FileType,
		FileName:   message.FileName,
		FileSize:   message.FileSize,
		SessionSeq: message.SessionSeq,
		CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
	}
}

func oldestUserMessageSeq(list []respond.GetMessageListRespond) int64 {
	if len(list) == 0 {
		return 0
	}
	return list[0].SessionSeq
}

func oldestGroupMessageSeq(list []respond.GetGroupMessageListRespond) int64 {
	if len(list) == 0 {
		return 0
	}
	return list[0].SessionSeq
}

func (m *messageService) logUserMessageQueryFinish(source, userOneId, userTwoId string, beforeSeq int64, limit int, page respond.GetMessageListPageRespond) {
	zlog.Info(
		"message.query.finish",
		zap.String("event", "message.query.finish"),
		zap.String("module", "message.service"),
		zap.String("scope", "user"),
		zap.String("source", source),
		zap.String("user_one_id", userOneId),
		zap.String("user_two_id", userTwoId),
		zap.Int64("before_seq", beforeSeq),
		zap.Int64("next_before_seq", page.NextBeforeSeq),
		zap.Bool("has_more", page.HasMore),
		zap.Int("limit", limit),
		zap.Int("result_count", len(page.List)),
		zap.String("order_by", "session_seq ASC"),
	)
}

func (m *messageService) logGroupMessageQueryFinish(source, groupId string, beforeSeq int64, limit int, page respond.GetGroupMessageListPageRespond) {
	zlog.Info(
		"message.query.finish",
		zap.String("event", "message.query.finish"),
		zap.String("module", "message.service"),
		zap.String("scope", "group"),
		zap.String("source", source),
		zap.String("group_id", groupId),
		zap.Int64("before_seq", beforeSeq),
		zap.Int64("next_before_seq", page.NextBeforeSeq),
		zap.Bool("has_more", page.HasMore),
		zap.Int("limit", limit),
		zap.Int("result_count", len(page.List)),
		zap.String("order_by", "session_seq ASC"),
	)
}

// UploadAvatar 上传头像
func (m *messageService) UploadAvatar(c *gin.Context) (string, int) {
	if err := c.Request.ParseMultipartForm(constants.FILE_MAX_SIZE); err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, -1
	}
	mForm := c.Request.MultipartForm
	for key, _ := range mForm.File {
		// 当前接口允许一次上传多个字段，但实际前端通常只传一个头像文件。
		file, fileHeader, err := c.Request.FormFile(key)
		if err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		defer file.Close()
		zlog.Info(fmt.Sprintf("文件名：%s，文件大小：%d", fileHeader.Filename, fileHeader.Size))
		// 原来Filename应该是213451545.xxx，将Filename修改为avatar_ownerId.xxx
		ext := filepath.Ext(fileHeader.Filename)
		zlog.Info(ext)
		// 落盘前确保头像目录已存在，兼容首次启动或空目录场景。
		if err := os.MkdirAll(config.GetConfig().StaticAvatarPath, 0755); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		localFileName := config.GetConfig().StaticAvatarPath + "/" + fileHeader.Filename
		out, err := os.Create(localFileName)
		if err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		// 上传接口只负责保存文件，不在这里直接改用户资料表。
		zlog.Info("完成文件上传")
	}
	return "上传成功", 0
}

// UploadFile 上传文件
func (m *messageService) UploadFile(c *gin.Context) (string, int) {
	if err := c.Request.ParseMultipartForm(constants.FILE_MAX_SIZE); err != nil {
		zlog.Error(err.Error())
		return constants.SYSTEM_ERROR, -1
	}
	mForm := c.Request.MultipartForm
	for key, _ := range mForm.File {
		// 文件消息先通过 HTTP 接口落盘，再由聊天消息体携带 URL 通知接收方。
		file, fileHeader, err := c.Request.FormFile(key)
		if err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		defer file.Close()
		zlog.Info(fmt.Sprintf("文件名：%s，文件大小：%d", fileHeader.Filename, fileHeader.Size))
		// 原来Filename应该是213451545.xxx，将Filename修改为avatar_ownerId.xxx
		ext := filepath.Ext(fileHeader.Filename)
		zlog.Info(ext)
		// 落盘前同样保证目标目录存在，防止部署时目录缺失导致上传失败。
		if err := os.MkdirAll(config.GetConfig().StaticFilePath, 0755); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		localFileName := config.GetConfig().StaticFilePath + "/" + fileHeader.Filename
		out, err := os.Create(localFileName)
		if err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
		zlog.Info("完成文件上传")
	}
	return "上传成功", 0
}
