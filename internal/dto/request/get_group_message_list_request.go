package request

// GetGroupMessageListRequest 描述该接口使用的请求参数结构。
type GetGroupMessageListRequest struct {
	// GroupId 保存前端传入的 `group_id` 参数。
	GroupId string `json:"group_id"`
	// BeforeSeq 表示查询该 session_seq 之前的更早消息；为空或 0 表示查询最新一页。
	BeforeSeq int64 `json:"before_seq,omitempty"`
	// Limit 表示本次最多返回多少条消息；为空时使用服务端默认值。
	Limit int `json:"limit,omitempty"`
}
