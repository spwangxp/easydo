package models

type ResourceTerminalSessionStatus string

const (
	ResourceTerminalSessionStatusActive ResourceTerminalSessionStatus = "active"
	ResourceTerminalSessionStatusClosed ResourceTerminalSessionStatus = "closed"
)

type ResourceTerminalSession struct {
	BaseModel
	SessionID         string                        `gorm:"size:64;not null;uniqueIndex" json:"session_id"`
	WorkspaceID       uint64                        `gorm:"not null;index" json:"workspace_id"`
	ResourceID        uint64                        `gorm:"not null;index" json:"resource_id"`
	CredentialID      uint64                        `gorm:"not null;index" json:"credential_id"`
	AgentID           uint64                        `gorm:"not null;index" json:"agent_id"`
	ResourceType      ResourceType                  `gorm:"size:32;not null;index" json:"resource_type"`
	Endpoint          string                        `gorm:"size:512" json:"endpoint"`
	Status            ResourceTerminalSessionStatus `gorm:"size:32;not null;default:'active';index" json:"status"`
	OwnerServerID     string                        `gorm:"size:128;index" json:"owner_server_id"`
	OwnerConnectionID string                        `gorm:"size:64;index" json:"owner_connection_id"`
	AttachedBy        *uint64                       `gorm:"index" json:"attached_by"`
	AttachedAt        int64                         `json:"attached_at"`
	CloseReason       string                        `gorm:"size:128" json:"close_reason"`
	ClosedAt          int64                         `json:"closed_at"`
	ClosedBy          *uint64                       `gorm:"index" json:"closed_by"`
	CreatedBy         uint64                        `gorm:"not null;index" json:"created_by"`

	Workspace  *Workspace  `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Resource   *Resource   `gorm:"foreignKey:ResourceID" json:"resource,omitempty"`
	Credential *Credential `gorm:"foreignKey:CredentialID" json:"credential,omitempty"`
	Agent      *Agent      `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Creator    *User       `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Closer     *User       `gorm:"foreignKey:ClosedBy" json:"closer,omitempty"`
	Attacher   *User       `gorm:"foreignKey:AttachedBy" json:"attacher,omitempty"`
}
