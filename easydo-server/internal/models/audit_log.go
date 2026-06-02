package models

// AuditLog records high-risk personnel and governance changes for later review.
type AuditLog struct {
	BaseModel
	WorkspaceID      *uint64 `gorm:"index" json:"workspace_id"`
	ActorUserID      uint64  `gorm:"not null;index" json:"actor_user_id"`
	ActorRole        string  `gorm:"size:32" json:"actor_role"`
	ActorWorkspaceID *uint64 `gorm:"index" json:"actor_workspace_id"`
	Action           string  `gorm:"size:96;not null;index" json:"action"`
	TargetType       string  `gorm:"size:64;not null;index" json:"target_type"`
	TargetID         uint64  `gorm:"not null;index" json:"target_id"`
	BeforeJSON       string  `gorm:"type:longtext" json:"before_json"`
	AfterJSON        string  `gorm:"type:longtext" json:"after_json"`
	MetadataJSON     string  `gorm:"type:longtext" json:"metadata_json"`
	IP               string  `gorm:"size:64" json:"ip"`
	UserAgent        string  `gorm:"size:512" json:"user_agent"`
}
