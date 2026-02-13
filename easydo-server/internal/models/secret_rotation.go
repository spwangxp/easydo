package models

import "time"

// SecretRotation 密钥轮换历史
type SecretRotation struct {
	BaseModel
	SecretID   uint64    `gorm:"index;not null" json:"secret_id"`
	OldVersion int       `json:"old_version"`
	NewVersion int       `json:"new_version"`
	OldValue   string    `gorm:"type:text" json:"-"` // 加密存储，不返回给前端
	NewValue   string    `gorm:"type:text" json:"-"`
	RotatedBy  uint64    `gorm:"index;not null" json:"rotated_by"`
	Reason     string    `gorm:"type:varchar(255)" json:"reason"` // manual_rotation, auto_rotation, security_incident
	CreatedAt  time.Time `json:"created_at"`
}

// TableName 指定表名
func (SecretRotation) TableName() string {
	return "secret_rotations"
}
