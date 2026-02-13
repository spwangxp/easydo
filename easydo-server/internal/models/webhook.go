package models

import "time"

// WebhookConfig Webhook配置
type WebhookConfig struct {
	BaseModel
	Name      string    `gorm:"type:varchar(100)" json:"name"`
	URL       string    `gorm:"type:varchar(500)" json:"url"`
	Secret    string    `gorm:"type:varchar(255)" json:"-"`
	Events    string    `gorm:"type:text" json:"events"`
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	CreatedBy uint64    `json:"created_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WebhookEvent Webhook发送记录
type WebhookEvent struct {
	BaseModel
	ConfigID   uint64    `gorm:"index" json:"config_id"`
	EventType  string    `gorm:"type:varchar(50)" json:"event_type"`
	Payload    string    `gorm:"type:text" json:"payload"`
	Status     string    `gorm:"type:varchar(20)" json:"status"`
	Response   string    `gorm:"type:text" json:"response"`
	RetryCount int       `gorm:"default:0" json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func (WebhookConfig) TableName() string {
	return "webhook_configs"
}

func (WebhookEvent) TableName() string {
	return "webhook_events"
}
