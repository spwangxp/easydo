package models

import "strings"

const (
	WorkspaceStatusActive   = "active"
	WorkspaceStatusArchived = "archived"

	WorkspaceVisibilityPrivate = "private"

	WorkspaceMemberStatusActive   = "active"
	WorkspaceMemberStatusDisabled = "disabled"

	WorkspaceRoleViewer     = "viewer"
	WorkspaceRoleDeveloper  = "developer"
	WorkspaceRoleMaintainer = "maintainer"
	WorkspaceRoleOwner      = "owner"

	WorkspaceInvitationStatusPending  = "pending"
	WorkspaceInvitationStatusAccepted = "accepted"
	WorkspaceInvitationStatusRevoked  = "revoked"
	WorkspaceInvitationStatusExpired  = "expired"

	AgentScopePlatform  = "platform"
	AgentScopeWorkspace = "workspace"
)

type Workspace struct {
	BaseModel
	Name        string `gorm:"size:128;not null" json:"name"`
	Slug        string `gorm:"size:128;not null;uniqueIndex" json:"slug"`
	Description string `gorm:"type:text" json:"description"`
	Status      string `gorm:"size:32;default:'active';index" json:"status"`
	Visibility  string `gorm:"size:32;default:'private'" json:"visibility"`
	CreatedBy   uint64 `gorm:"not null;index" json:"created_by"`

	Creator *User             `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Members []WorkspaceMember `gorm:"foreignKey:WorkspaceID" json:"members,omitempty"`
}

type WorkspaceMember struct {
	BaseModel
	WorkspaceID uint64 `gorm:"not null;index:idx_workspace_user,unique" json:"workspace_id"`
	UserID      uint64 `gorm:"not null;index:idx_workspace_user,unique" json:"user_id"`
	Role        string `gorm:"size:32;default:'viewer';index" json:"role"`
	Status      string `gorm:"size:32;default:'active';index" json:"status"`
	InvitedBy   uint64 `gorm:"index" json:"invited_by"`
	JoinedAt    int64  `json:"joined_at"`

	Workspace *Workspace `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	User      *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Inviter   *User      `gorm:"foreignKey:InvitedBy" json:"inviter,omitempty"`
}

type WorkspaceInvitation struct {
	BaseModel
	WorkspaceID    uint64  `gorm:"not null;index" json:"workspace_id"`
	Email          string  `gorm:"size:128;not null;index" json:"email"`
	InvitedUserID  *uint64 `gorm:"index" json:"invited_user_id"`
	Role           string  `gorm:"size:32;default:'viewer'" json:"role"`
	TokenHash      string  `gorm:"size:128;not null;uniqueIndex" json:"-"`
	Status         string  `gorm:"size:32;default:'pending';index" json:"status"`
	InvitedBy      uint64  `gorm:"not null;index" json:"invited_by"`
	ExpiresAt      int64   `gorm:"index" json:"expires_at"`
	AcceptedAt     int64   `json:"accepted_at"`
	AcceptedByUser *uint64 `gorm:"index" json:"accepted_by_user_id"`

	Workspace         *Workspace `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Inviter           *User      `gorm:"foreignKey:InvitedBy" json:"inviter,omitempty"`
	InvitedUser       *User      `gorm:"foreignKey:InvitedUserID" json:"invited_user,omitempty"`
	AcceptedByUserRef *User      `gorm:"foreignKey:AcceptedByUser" json:"accepted_by_user,omitempty"`
}

func NormalizeWorkspaceRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case WorkspaceRoleOwner:
		return WorkspaceRoleOwner
	case WorkspaceRoleMaintainer:
		return WorkspaceRoleMaintainer
	case WorkspaceRoleDeveloper:
		return WorkspaceRoleDeveloper
	default:
		return WorkspaceRoleViewer
	}
}
