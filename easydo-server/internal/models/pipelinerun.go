package models

type PipelineRun struct {
	BaseModel
	PipelineID   uint64 `gorm:"index;not null" json:"pipeline_id"`
	BuildNumber  int    `gorm:"not null" json:"build_number"`
	Status       string `gorm:"size:32;not null" json:"status"` // pending/running/success/failed/cancelled
	TriggerType  string `gorm:"size:32" json:"trigger_type"` // manual/webhook/schedule
	TriggerUser  string `gorm:"size:64" json:"trigger_user"`
	TriggerSource string `gorm:"size:256" json:"trigger_source"` // webhook来源URL
	StartTime    int64  `json:"start_time"`
	EndTime      int64  `json:"end_time"`
	Duration     int    `json:"duration"`
	ErrorMsg     string `gorm:"type:text" json:"error_msg"` // 失败时的错误信息

	// 关键设计：保存执行时的配置快照（因为流水线可能被编辑）
	// 每次执行使用独立的配置副本，互不影响
	Config       string `gorm:"type:longtext" json:"config"` // 执行时的 PipelineConfig JSON 快照

	// 执行时绑定的 Agent（一次 PipelineRun 只使用一个 Agent，0 表示未分配）
	AgentID uint64 `gorm:"index" json:"agent_id"`

	// 关联
	Pipeline *Pipeline   `gorm:"foreignKey:PipelineID" json:"pipeline"`
	Agent    *Agent      `gorm:"-" json:"-"` // 不创建外键约束
	Tasks    []AgentTask `gorm:"foreignKey:PipelineRunID" json:"tasks"` // 所有子任务
}
