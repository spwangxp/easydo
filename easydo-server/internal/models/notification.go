package models

type NotificationEvent struct {
	BaseModel
	WorkspaceID    uint64  `gorm:"index" json:"workspace_id"`
	Family         string  `gorm:"size:64;not null;index" json:"family"`
	EventType      string  `gorm:"size:64;not null" json:"event_type"`
	ResourceType   string  `gorm:"size:64;index" json:"resource_type"`
	ResourceID     uint64  `gorm:"index" json:"resource_id"`
	ActorUserID    *uint64 `gorm:"index" json:"actor_user_id"`
	ActorType      string  `gorm:"size:32;default:'system'" json:"actor_type"`
	Title          string  `gorm:"size:256;not null" json:"title"`
	Content        string  `gorm:"type:longtext" json:"content"`
	Priority       int     `gorm:"default:0" json:"priority"`
	Metadata       string  `gorm:"type:longtext" json:"metadata"`
	IdempotencyKey string  `gorm:"size:191;not null;uniqueIndex" json:"idempotency_key"`
}

type NotificationAudience struct {
	BaseModel
	EventID          uint64  `gorm:"not null;index" json:"event_id"`
	WorkspaceID      uint64  `gorm:"index" json:"workspace_id"`
	RecipientUserID  *uint64 `gorm:"index" json:"recipient_user_id"`
	RecipientEmail   string  `gorm:"size:255" json:"recipient_email"`
	ResourceType     string  `gorm:"size:64;index" json:"resource_type"`
	ResourceID       uint64  `gorm:"index" json:"resource_id"`
	PermissionPolicy string  `gorm:"size:64;default:'workspace_member'" json:"permission_policy"`
	AudienceKey      string  `gorm:"size:191;not null;uniqueIndex" json:"audience_key"`
}

type Notification struct {
	BaseModel
	EventID         uint64  `gorm:"not null;index" json:"event_id"`
	AudienceID      uint64  `gorm:"not null;index" json:"audience_id"`
	WorkspaceID     uint64  `gorm:"index" json:"workspace_id"`
	RecipientUserID *uint64 `gorm:"index" json:"recipient_user_id"`
	RecipientEmail  string  `gorm:"size:255" json:"recipient_email"`
	Family          string  `gorm:"size:64;not null;index" json:"family"`
	EventType       string  `gorm:"size:64;not null" json:"event_type"`
	ResourceType    string  `gorm:"size:64;index" json:"resource_type"`
	ResourceID      uint64  `gorm:"index" json:"resource_id"`
	ActorUserID     *uint64 `gorm:"index" json:"actor_user_id"`
	ActorType       string  `gorm:"size:32;default:'system'" json:"actor_type"`
	Title           string  `gorm:"size:256;not null" json:"title"`
	Content         string  `gorm:"type:longtext" json:"content"`
	Priority        int     `gorm:"default:0" json:"priority"`
	Metadata        string  `gorm:"type:longtext" json:"metadata"`
	CanonicalKey    string  `gorm:"size:191;not null;uniqueIndex" json:"canonical_key"`
}

type NotificationDelivery struct {
	BaseModel
	NotificationID uint64 `gorm:"not null;index;uniqueIndex:idx_notification_channel" json:"notification_id"`
	Channel        string `gorm:"size:32;not null;uniqueIndex:idx_notification_channel" json:"channel"`
	Destination    string `gorm:"size:255" json:"destination"`
	Status         string `gorm:"size:32;not null;index" json:"status"`
	Provider       string `gorm:"size:64" json:"provider"`
	ExternalRef    string `gorm:"size:191" json:"external_ref"`
	ErrorMessage   string `gorm:"type:text" json:"error_message"`
	AttemptCount   int    `gorm:"default:0" json:"attempt_count"`
	LastAttemptAt  int64  `json:"last_attempt_at"`
	NextRetryAt    int64  `gorm:"index" json:"next_retry_at"`
	SentAt         int64  `json:"sent_at"`
	ReadAt         int64  `json:"read_at"`
}

type NotificationPreference struct {
	BaseModel
	UserID       uint64  `gorm:"not null;index" json:"user_id"`
	WorkspaceID  *uint64 `gorm:"index" json:"workspace_id"`
	ResourceType string  `gorm:"size:64;index" json:"resource_type"`
	ResourceID   *uint64 `gorm:"index" json:"resource_id"`
	Family       string  `gorm:"size:64;not null;index" json:"family"`
	EventType    string  `gorm:"size:64;not null;index" json:"event_type"`
	Channel      string  `gorm:"size:32;not null;index" json:"channel"`
	Enabled      bool    `gorm:"not null" json:"enabled"`
	RuleKey      string  `gorm:"size:191;not null;uniqueIndex" json:"rule_key"`
}

type InboxMessage struct {
	BaseModel
	NotificationID  uint64  `gorm:"column:notification_id;index;uniqueIndex" json:"notification_id"`
	EventID         uint64  `gorm:"column:event_id;index" json:"event_id"`
	AudienceID      uint64  `gorm:"column:audience_id;index" json:"audience_id"`
	WorkspaceID     uint64  `gorm:"column:workspace_id;index" json:"workspace_id"`
	RecipientUserID *uint64 `gorm:"column:recipient_id;index" json:"recipient_user_id"`
	Family          string  `gorm:"column:type;size:64;not null" json:"family"`
	EventType       string  `gorm:"column:event_type;size:64" json:"event_type"`
	Title           string  `gorm:"column:title;size:256;not null" json:"title"`
	Content         string  `gorm:"column:content;type:longtext" json:"content"`
	SenderID        *uint64 `gorm:"column:sender_id;index" json:"sender_id"`
	SenderType      string  `gorm:"column:sender_type;size:32;default:'system'" json:"sender_type"`
	Priority        int     `gorm:"column:priority;default:0" json:"priority"`
	IsRead          bool    `gorm:"column:is_read;default:false" json:"is_read"`
	ReadAt          int64   `gorm:"column:read_at" json:"read_at"`
	Metadata        string  `gorm:"column:metadata;type:text" json:"metadata"`
	Channel         string  `gorm:"column:channel;size:32;default:'in_app'" json:"channel"`
	ResourceType    string  `gorm:"column:resource_type;size:64;index" json:"resource_type"`
	ResourceID      uint64  `gorm:"column:resource_id;index" json:"resource_id"`
}

func (InboxMessage) TableName() string {
	return "messages"
}

const (
	NotificationChannelInApp = "in_app"
	NotificationChannelEmail = "email"

	NotificationDeliveryStatusPending       = "pending"
	NotificationDeliveryStatusDelivered     = "delivered"
	NotificationDeliveryStatusFailed        = "failed"
	NotificationDeliveryStatusSuppressed    = "suppressed"
	NotificationDeliveryStatusSkipped       = "skipped"
	NotificationDeliveryStatusNotConfigured = "not_configured"

	NotificationResourceTypeAgent             = "agent"
	NotificationResourceTypePipelineRun       = "pipeline_run"
	NotificationResourceTypeDeploymentRequest = "deployment_request"
	NotificationResourceTypeWorkspace         = "workspace"
	NotificationResourceTypeWorkspaceInvite   = "workspace_invitation"
	NotificationResourceTypeWorkspaceMember   = "workspace_member"
	NotificationResourceTypeUser              = "user"
	NotificationResourceTypePlatform          = "platform"
)
