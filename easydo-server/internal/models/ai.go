package models

type AIProviderStatus string

const (
	AIProviderStatusActive   AIProviderStatus = "active"
	AIProviderStatusDisabled AIProviderStatus = "disabled"
)

type AIModelBindingStatus string

const (
	AIModelBindingStatusActive   AIModelBindingStatus = "active"
	AIModelBindingStatusDisabled AIModelBindingStatus = "disabled"
)

type AISessionStatus string

const (
	AISessionStatusQueued    AISessionStatus = "queued"
	AISessionStatusRunning   AISessionStatus = "running"
	AISessionStatusCompleted AISessionStatus = "completed"
	AISessionStatusFailed    AISessionStatus = "failed"
	AISessionStatusCancelled AISessionStatus = "cancelled"
)

type AIAgentStatus string

const (
	AIAgentStatusDraft    AIAgentStatus = "draft"
	AIAgentStatusActive   AIAgentStatus = "active"
	AIAgentStatusArchived AIAgentStatus = "archived"
)

type AIRuntimeProfileStatus string

const (
	AIRuntimeProfileStatusActive   AIRuntimeProfileStatus = "active"
	AIRuntimeProfileStatusDisabled AIRuntimeProfileStatus = "disabled"
	AIRuntimeProfileStatusDraft    AIRuntimeProfileStatus = "draft"
)

type AIProvider struct {
	BaseModel
	WorkspaceID  uint64           `gorm:"not null;index" json:"workspace_id"`
	Name         string           `gorm:"size:128;not null;index" json:"name"`
	Description  string           `gorm:"type:text" json:"description"`
	ProviderType string           `gorm:"size:64;not null;index" json:"provider_type"`
	BaseURL      string           `gorm:"size:512" json:"base_url"`
	CredentialID uint64           `gorm:"index" json:"credential_id"`
	HeadersJSON  string           `gorm:"type:longtext" json:"headers_json"`
	SettingsJSON string           `gorm:"type:longtext" json:"settings_json"`
	MetadataJSON string           `gorm:"type:longtext" json:"metadata_json"`
	Status       AIProviderStatus `gorm:"size:32;not null;default:'active';index" json:"status"`
	CreatedBy    uint64           `gorm:"not null;index" json:"created_by"`

	Workspace  *Workspace       `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Credential *Credential      `gorm:"foreignKey:CredentialID" json:"credential,omitempty"`
	Creator    *User            `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Bindings   []AIModelBinding `gorm:"foreignKey:ProviderID" json:"bindings,omitempty"`
}

func (AIProvider) TableName() string {
	return "ai_providers"
}

type AIModelBinding struct {
	BaseModel
	WorkspaceID      uint64               `gorm:"not null;index" json:"workspace_id"`
	ModelID          uint64               `gorm:"not null;index" json:"model_id"`
	ProviderID       uint64               `gorm:"not null;index" json:"provider_id"`
	ProviderModelKey string               `gorm:"size:255" json:"provider_model_key"`
	SettingsJSON     string               `gorm:"type:longtext" json:"settings_json"`
	MetadataJSON     string               `gorm:"type:longtext" json:"metadata_json"`
	Status           AIModelBindingStatus `gorm:"size:32;not null;default:'active';index" json:"status"`
	CreatedBy        uint64               `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace      `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Model     *AIModelCatalog `gorm:"foreignKey:ModelID" json:"model,omitempty"`
	Provider  *AIProvider     `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
	Creator   *User           `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (AIModelBinding) TableName() string {
	return "ai_model_bindings"
}

type AIAgent struct {
	BaseModel
	WorkspaceID        uint64        `gorm:"not null;index" json:"workspace_id"`
	Name               string        `gorm:"size:128;not null;index" json:"name"`
	Description        string        `gorm:"type:text" json:"description"`
	ScopeType          string        `gorm:"size:32;default:'workspace';index" json:"scope_type"`
	RuntimeProfileID   *uint64       `gorm:"index" json:"runtime_profile_id"`
	SystemPrompt       string        `gorm:"type:longtext" json:"system_prompt"`
	UserPromptTemplate string        `gorm:"type:longtext" json:"user_prompt_template"`
	InputSchemaJSON    string        `gorm:"type:longtext" json:"input_schema_json"`
	OutputSchemaJSON   string        `gorm:"type:longtext" json:"output_schema_json"`
	ToolPolicyJSON     string        `gorm:"type:longtext" json:"tool_policy_json"`
	ToolsJSON          string        `gorm:"type:longtext" json:"tools_json"`
	SkillsJSON         string        `gorm:"type:longtext" json:"skills_json"`
	MemoryJSON         string        `gorm:"type:longtext" json:"memory_json"`
	MCPServersJSON     string        `gorm:"type:longtext" json:"mcp_servers_json"`
	SubAgentsJSON      string        `gorm:"type:longtext" json:"sub_agents_json"`
	MetadataJSON       string        `gorm:"type:longtext" json:"metadata_json"`
	Status             AIAgentStatus `gorm:"size:32;not null;default:'draft';index" json:"status"`
	CreatedBy          uint64        `gorm:"not null;index" json:"created_by"`

	Workspace      *Workspace        `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Creator        *User             `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	RuntimeProfile *AIRuntimeProfile `gorm:"foreignKey:RuntimeProfileID" json:"runtime_profile,omitempty"`
}

func (AIAgent) TableName() string {
	return "ai_agents"
}

type AISessionTurn struct {
	BaseModel
	SessionID    uint64 `gorm:"not null;index;uniqueIndex:uk_ai_session_turns_session_seq" json:"session_id"`
	TurnSeq      int    `gorm:"not null;uniqueIndex:uk_ai_session_turns_session_seq" json:"turn_seq"`
	TurnType     string `gorm:"size:64;not null;index" json:"turn_type"`
	Role         string `gorm:"size:32;not null;default:'system';index" json:"role"`
	Status       string `gorm:"size:32;not null;default:'completed';index" json:"status"`
	InputJSON    string `gorm:"type:longtext" json:"input_json"`
	OutputJSON   string `gorm:"type:longtext" json:"output_json"`
	ErrorMsg     string `gorm:"type:text" json:"error_msg"`
	MetadataJSON string `gorm:"type:longtext" json:"metadata_json"`
	StartedAt    *int64 `gorm:"index" json:"started_at"`
	CompletedAt  *int64 `gorm:"index" json:"completed_at"`

	Session *AISession `gorm:"foreignKey:SessionID" json:"session,omitempty"`
}

func (AISessionTurn) TableName() string {
	return "ai_session_turns"
}

type AIRuntimeProfile struct {
	BaseModel
	WorkspaceID         uint64                 `gorm:"not null;index" json:"workspace_id"`
	Name                string                 `gorm:"size:128;not null;index" json:"name"`
	ModelID             uint64                 `gorm:"not null;index" json:"model_id"`
	BindingPriorityJSON string                 `gorm:"type:longtext" json:"binding_priority_json"`
	RuntimeSettingsJSON string                 `gorm:"type:longtext" json:"runtime_settings_json"`
	FallbackEnabled     bool                   `gorm:"default:true" json:"fallback_enabled"`
	Status              AIRuntimeProfileStatus `gorm:"size:32;not null;default:'draft';index" json:"status"`
	CreatedBy           uint64                 `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace      `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Model     *AIModelCatalog `gorm:"foreignKey:ModelID" json:"model,omitempty"`
	Creator   *User           `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (AIRuntimeProfile) TableName() string {
	return "ai_runtime_profiles"
}

type AIScene struct {
	BaseModel
	WorkspaceID          uint64  `gorm:"not null;index;uniqueIndex:uk_ai_scenes_workspace_code" json:"workspace_id"`
	SceneType            string  `gorm:"size:64;not null;index" json:"scene_type"`
	Code                 string  `gorm:"size:128;not null;uniqueIndex:uk_ai_scenes_workspace_code" json:"code"`
	Name                 string  `gorm:"size:128;not null" json:"name"`
	Description          string  `gorm:"type:text" json:"description"`
	MainAgentID          *uint64 `gorm:"index" json:"main_agent_id"`
	ContextProvider      string  `gorm:"size:128" json:"context_provider"`
	Enabled              bool    `gorm:"not null;default:true;index" json:"enabled"`
	DisplayNameOverride  string  `gorm:"size:128" json:"display_name_override"`
	IntroMessageOverride string  `gorm:"type:text" json:"intro_message_override"`
	MetadataJSON         string  `gorm:"type:longtext" json:"metadata_json"`
	CreatedBy            uint64  `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	MainAgent *AIAgent   `gorm:"foreignKey:MainAgentID" json:"main_agent,omitempty"`
	Creator   *User      `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (AIScene) TableName() string {
	return "ai_scenes"
}

type AISession struct {
	BaseModel
	WorkspaceID         uint64          `gorm:"not null;index" json:"workspace_id"`
	SceneID             *uint64         `gorm:"index" json:"scene_id"`
	ParentSessionID     *uint64         `gorm:"index" json:"parent_session_id"`
	RootSessionID       *uint64         `gorm:"index" json:"root_session_id"`
	PipelineRunID       uint64          `gorm:"index" json:"pipeline_run_id"`
	TaskID              uint64          `gorm:"index" json:"task_id"`
	NodeID              string          `gorm:"size:128;index" json:"node_id"`
	TaskType            string          `gorm:"size:64;not null;index" json:"task_type"`
	Status              AISessionStatus `gorm:"size:32;not null;default:'queued';index" json:"status"`
	RuntimeProfileID    uint64          `gorm:"index" json:"runtime_profile_id"`
	ProviderID          uint64          `gorm:"index" json:"provider_id"`
	ModelID             uint64          `gorm:"index" json:"model_id"`
	BindingID           uint64          `gorm:"index" json:"binding_id"`
	AgentID             uint64          `gorm:"index" json:"agent_id"`
	RequestJSON         string          `gorm:"type:longtext" json:"request_json"`
	ContextSnapshotJSON string          `gorm:"type:longtext" json:"context_snapshot_json"`
	MemorySnapshotJSON  string          `gorm:"type:longtext" json:"memory_snapshot_json"`
	MetadataJSON        string          `gorm:"type:longtext" json:"metadata_json"`
	ResponseJSON        string          `gorm:"type:longtext" json:"response_json"`
	ErrorMsg            string          `gorm:"type:text" json:"error_msg"`
	StartedAt           int64           `gorm:"index" json:"started_at"`
	CompletedAt         int64           `gorm:"index" json:"completed_at"`
	CreatedBy           uint64          `gorm:"not null;index" json:"created_by"`

	Scene          *AIScene          `gorm:"foreignKey:SceneID" json:"scene,omitempty"`
	RuntimeProfile *AIRuntimeProfile `gorm:"foreignKey:RuntimeProfileID" json:"runtime_profile,omitempty"`
	Provider       *AIProvider       `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
	Model          *AIModelCatalog   `gorm:"foreignKey:ModelID" json:"model,omitempty"`
	Binding        *AIModelBinding   `gorm:"foreignKey:BindingID" json:"binding,omitempty"`
	Agent          *AIAgent          `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Task           *AgentTask        `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	PipelineRun    *PipelineRun      `gorm:"foreignKey:PipelineRunID" json:"pipeline_run,omitempty"`
	Creator        *User             `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Turns          []AISessionTurn   `gorm:"foreignKey:SessionID" json:"turns,omitempty"`
}
