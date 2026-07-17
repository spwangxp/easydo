package models

import "time"

const (
	MCPTokenStatusActive  = "active"
	MCPTokenStatusRevoked = "revoked"
)

type MCPToken struct {
	BaseModel
	WorkspaceID    uint64     `gorm:"not null;uniqueIndex:uk_mcp_tokens_workspace_user_name;index" json:"workspace_id"`
	UserID         uint64     `gorm:"not null;uniqueIndex:uk_mcp_tokens_workspace_user_name;index" json:"user_id"`
	Name           string     `gorm:"size:96;not null;uniqueIndex:uk_mcp_tokens_workspace_user_name" json:"name"`
	TokenHash      string     `gorm:"size:96;not null;uniqueIndex" json:"-"`
	TokenEncrypted string     `gorm:"column:token_encrypted;type:longtext;not null" json:"-"`
	Status         string     `gorm:"size:32;not null;default:'active';index" json:"status"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

func (MCPToken) TableName() string {
	return "mcp_tokens"
}
