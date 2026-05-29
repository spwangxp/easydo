package models

type MCPCallAudit struct {
	BaseModel
	RequestID     string `gorm:"size:191;not null;index" json:"request_id"`
	UserID        uint64 `gorm:"not null;index" json:"user_id"`
	WorkspaceID   uint64 `gorm:"not null;index" json:"workspace_id"`
	ToolName      string `gorm:"size:128;not null;index" json:"tool_name"`
	OperationType string `gorm:"size:32;not null;index" json:"operation_type"`
	TargetType    string `gorm:"size:64;index" json:"target_type"`
	TargetID      string `gorm:"size:191;index" json:"target_id"`
	ClientName    string `gorm:"size:128" json:"client_name"`
	Protocol      string `gorm:"size:32;not null;index" json:"protocol"`
	Status        string `gorm:"size:32;not null;index" json:"status"`
	DurationMS    int64  `json:"duration_ms"`
	InputSummary  string `gorm:"type:text" json:"input_summary"`
	OutputSummary string `gorm:"type:text" json:"output_summary"`
	ErrorCode     string `gorm:"size:64;index" json:"error_code"`
	ErrorMessage  string `gorm:"type:text" json:"error_message"`
}

func (MCPCallAudit) TableName() string {
	return "mcp_call_audits"
}
