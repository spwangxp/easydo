package models

import "time"

type PipelineTrigger struct {
	BaseModel
	WorkspaceID                     uint64     `gorm:"not null;index;uniqueIndex:idx_pipeline_trigger_pipeline" json:"workspace_id"`
	PipelineID                      uint64     `gorm:"not null;index;uniqueIndex:idx_pipeline_trigger_pipeline" json:"pipeline_id"`
	Provider                        string     `gorm:"size:32;not null;default:'gitlab'" json:"provider"`
	WebhookEnabled                  bool       `gorm:"default:false" json:"webhook_enabled"`
	PushEnabled                     bool       `gorm:"default:false" json:"push_enabled"`
	TagEnabled                      bool       `gorm:"default:false" json:"tag_enabled"`
	MergeRequestEnabled             bool       `gorm:"default:false" json:"merge_request_enabled"`
	ScheduleEnabled                 bool       `gorm:"default:false" json:"schedule_enabled"`
	CronExpression                  string     `gorm:"size:128" json:"cron_expression"`
	Timezone                        string     `gorm:"size:64;default:'UTC'" json:"timezone"`
	PushBranchFilters               string     `gorm:"type:text" json:"push_branch_filters"`
	TagFilters                      string     `gorm:"type:text" json:"tag_filters"`
	MergeRequestSourceBranchFilters string     `gorm:"type:text" json:"merge_request_source_branch_filters"`
	MergeRequestTargetBranchFilters string     `gorm:"type:text" json:"merge_request_target_branch_filters"`
	SecretToken                     string     `gorm:"size:255" json:"secret_token"`
	WebhookToken                    string     `gorm:"size:255;index" json:"webhook_token"`
	LastEventTypes                  string     `gorm:"type:text" json:"last_event_types"`
	NextRunAt                       *time.Time `json:"next_run_at,omitempty"`
	LastRunAt                       *time.Time `json:"last_run_at,omitempty"`
	LastTriggeredAt                 *time.Time `json:"last_triggered_at,omitempty"`
	CreatedBy                       uint64     `gorm:"index" json:"created_by"`
	UpdatedBy                       uint64     `gorm:"index" json:"updated_by"`
	Pipeline                        *Pipeline  `gorm:"foreignKey:PipelineID" json:"pipeline,omitempty"`
}
