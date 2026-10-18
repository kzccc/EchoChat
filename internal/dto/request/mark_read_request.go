package request

// MarkReadRequest 描述上报会话已读进度的请求参数结构。
type MarkReadRequest struct {
	ConversationKey string `json:"conversation_key"`
	LastReadSeq     int64  `json:"last_read_seq"`
}
