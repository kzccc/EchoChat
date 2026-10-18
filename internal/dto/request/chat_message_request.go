package request

// ChatMessageRequest 描述该接口使用的请求参数结构。
type ChatMessageRequest struct {
	// MessageId 保存服务端生成的消息唯一标识，用于落库、分发和 Kafka 链路追踪。
	MessageId string `json:"message_id,omitempty"`
	// ClientMsgID 保存客户端生成的消息标识，只用于入口幂等。
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// SessionId 保存前端传入的 `session_id` 参数。
	SessionId string `json:"session_id"`
	// Type 保存前端传入的 `type` 参数。
	Type int8 `json:"type"`
	// Content 保存前端传入的 `content` 参数。
	Content string `json:"content"`
	// Url 保存前端传入的 `url` 参数。
	Url string `json:"url"`
	// SendId 保存前端传入的 `send_id` 参数。
	SendId string `json:"send_id"`
	// SendName 保存前端传入的 `send_name` 参数。
	SendName string `json:"send_name"`
	// SendAvatar 保存前端传入的 `send_avatar` 参数。
	SendAvatar string `json:"send_avatar"`
	// ReceiveId 保存前端传入的 `receive_id` 参数。
	ReceiveId string `json:"receive_id"`
	// FileSize 保存前端传入的 `file_size` 参数。
	FileSize string `json:"file_size"`
	// FileType 保存前端传入的 `file_type` 参数。
	FileType string `json:"file_type"`
	// FileName 保存前端传入的 `file_name` 参数。
	FileName string `json:"file_name"`
	// AVdata 保存前端传入的 `av_data` 参数。
	AVdata string `json:"av_data"`
}
