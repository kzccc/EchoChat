package model

import "time"

// UserReadState 保存用户在某个会话内已经读到的最大 session_seq。
type UserReadState struct {
	Id              int64     `gorm:"column:id;primaryKey;comment:自增id"`
	UserId          string    `gorm:"column:user_id;type:char(20);not null;uniqueIndex:uniq_user_read_state,priority:1;index:idx_user_read_state_user;comment:用户uuid"`
	ConversationKey string    `gorm:"column:conversation_key;type:varchar(64);not null;uniqueIndex:uniq_user_read_state,priority:2;index:idx_user_read_state_conversation;comment:会话顺序作用域"`
	LastReadSeq     int64     `gorm:"column:last_read_seq;not null;comment:已读到的最大会话序号"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;comment:创建时间"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null;comment:更新时间"`
}

// TableName 返回 UserReadState 对应的表名。
func (UserReadState) TableName() string {
	return "user_read_state"
}
