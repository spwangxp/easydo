package models

const (
	NotificationSenderScopePlatform  = "platform"
	NotificationSenderScopeWorkspace = "workspace"
)

// NotificationSenderConfig stores platform and workspace SMTP sender settings.
// WorkspaceID uses 0 for platform scope so the unique index works consistently in MySQL.
type NotificationSenderConfig struct {
	BaseModel
	Scope                 string `gorm:"size:32;not null;uniqueIndex:idx_notification_sender_scope_workspace" json:"scope"`
	WorkspaceID           uint64 `gorm:"not null;default:0;uniqueIndex:idx_notification_sender_scope_workspace;index" json:"workspace_id"`
	Enabled               bool   `gorm:"default:false" json:"enabled"`
	FromName              string `gorm:"size:128" json:"from_name"`
	FromAddress           string `gorm:"size:255" json:"from_address"`
	SMTPHost              string `gorm:"size:255" json:"smtp_host"`
	SMTPPort              int    `json:"smtp_port"`
	SMTPUsername          string `gorm:"size:255" json:"smtp_username"`
	SMTPPasswordEncrypted string `gorm:"type:longtext" json:"-"`
	SMTPTLSMode           string `gorm:"size:32;default:'plain'" json:"smtp_tls_mode"`
	UpdatedBy             uint64 `gorm:"index" json:"updated_by"`
	LastTestedAt          int64  `json:"last_tested_at"`
	LastTestStatus        string `gorm:"size:32" json:"last_test_status"`
	LastTestError         string `gorm:"type:text" json:"last_test_error"`
}
