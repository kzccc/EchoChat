package request

// GetMessageListRequest 描述该接口使用的请求参数结构。
type GetMessageListRequest struct {
	// UserOneId 保存前端传入的 `user_one_id` 参数。
	UserOneId string `json:"user_one_id"`
	// UserTwoId 保存前端传入的 `user_two_id` 参数。
	UserTwoId string `json:"user_two_id"`
	// BeforeSeq 表示查询该 session_seq 之前的更早消息；为空或 0 表示查询最新一页。
	BeforeSeq int64 `json:"before_seq,omitempty"`
	// Limit 表示本次最多返回多少条消息；为空时使用服务端默认值。
	Limit int `json:"limit,omitempty"`
}
