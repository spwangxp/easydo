package models

type PipelineRun struct {
	BaseModel
	WorkspaceID     uint64  `gorm:"not null;index" json:"workspace_id"`
	PipelineID      uint64  `gorm:"index;not null;uniqueIndex:idx_pipeline_build_number" json:"pipeline_id"`
	BuildNumber     int     `gorm:"not null;uniqueIndex:idx_pipeline_build_number" json:"build_number"`
	Status          string  `gorm:"size:32;not null" json:"status"` // queued/pending/running/success/failed/cancelled
	TriggerType     string  `gorm:"size:32" json:"trigger_type"`    // manual/webhook/schedule
	TriggerUser     string  `gorm:"size:64" json:"trigger_user"`
	TriggerUserID   uint64  `gorm:"index;default:0" json:"trigger_user_id"`
	TriggerUserRole string  `gorm:"size:32" json:"trigger_user_role"`
	TriggerSource   string  `gorm:"size:256" json:"trigger_source"` // webhook来源URL
	IdempotencyKey  *string `gorm:"size:191;uniqueIndex" json:"idempotency_key,omitempty"`
	StartTime       int64   `json:"start_time"`
	EndTime         int64   `json:"end_time"`
	Duration        int     `json:"duration"`
	ErrorMsg        string  `gorm:"type:text" json:"error_msg"` // 失败时的错误信息

	// 关键设计：保存执行时的配置快照（因为流水线可能被编辑）
	// 每次执行使用独立的配置副本，互不影响
	Config string `gorm:"type:longtext" json:"config"` // 执行时的 PipelineConfig JSON 快照

	// 执行时绑定的 Agent（一次 PipelineRun 只使用一个 Agent，0 表示未分配）
	AgentID uint64 `gorm:"index" json:"agent_id"`

	// 关联
	Workspace *Workspace  `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Pipeline  *Pipeline   `gorm:"foreignKey:PipelineID" json:"pipeline"`
	Agent     *Agent      `gorm:"-" json:"-"`                            // 不创建外键约束
	Tasks     []AgentTask `gorm:"foreignKey:PipelineRunID" json:"tasks"` // 所有子任务
}

const (
	PipelineRunStatusQueued    = "queued"
	PipelineRunStatusPending   = "pending"
	PipelineRunStatusRunning   = "running"
	PipelineRunStatusSuccess   = "success"
	PipelineRunStatusFailed    = "failed"
	PipelineRunStatusCancelled = "cancelled"
)
