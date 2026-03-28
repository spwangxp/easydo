package models

type ResourceType string

const (
	ResourceTypeVM         ResourceType = "vm"
	ResourceTypeK8sCluster ResourceType = "k8s"
)

type ResourceStatus string

const (
	ResourceStatusOnline   ResourceStatus = "online"
	ResourceStatusOffline  ResourceStatus = "offline"
	ResourceStatusError    ResourceStatus = "error"
	ResourceStatusArchived ResourceStatus = "archived"
)

type Resource struct {
	BaseModel
	WorkspaceID         uint64         `gorm:"not null;index" json:"workspace_id"`
	ProjectID           *uint64        `gorm:"index" json:"project_id"`
	Name                string         `gorm:"size:128;not null;index" json:"name"`
	Description         string         `gorm:"type:text" json:"description"`
	Type                ResourceType   `gorm:"size:32;not null;index" json:"type"`
	Environment         string         `gorm:"size:32;default:'development';index" json:"environment"`
	Status              ResourceStatus `gorm:"size:32;default:'offline';index" json:"status"`
	Endpoint            string         `gorm:"size:512" json:"endpoint"`
	Labels              string         `gorm:"type:longtext" json:"labels"`
	Metadata            string         `gorm:"type:longtext" json:"metadata"`
	BaseInfo            string         `gorm:"type:longtext" json:"base_info"`
	BaseInfoStatus      string         `gorm:"size:32;default:'';index" json:"base_info_status"`
	BaseInfoSource      string         `gorm:"size:64" json:"base_info_source"`
	BaseInfoLastError   string         `gorm:"type:text" json:"base_info_last_error"`
	BaseInfoCollectedAt int64          `json:"base_info_collected_at"`
	LastCheckAt         int64          `json:"last_check_at"`
	LastCheckResult     string         `gorm:"type:text" json:"last_check_result"`
	CreatedBy           uint64         `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace                  `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Project   *Project                    `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Creator   *User                       `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Bindings  []ResourceCredentialBinding `gorm:"foreignKey:ResourceID" json:"bindings,omitempty"`
	Health    []ResourceHealthSnapshot    `gorm:"foreignKey:ResourceID" json:"health,omitempty"`
}

type ResourceCredentialBinding struct {
	BaseModel
	WorkspaceID  uint64 `gorm:"not null;index" json:"workspace_id"`
	ResourceID   uint64 `gorm:"not null;index" json:"resource_id"`
	CredentialID uint64 `gorm:"not null;index" json:"credential_id"`
	Purpose      string `gorm:"size:64;default:'primary'" json:"purpose"`
	BoundBy      uint64 `gorm:"not null;index" json:"bound_by"`

	Resource   *Resource   `gorm:"foreignKey:ResourceID" json:"resource,omitempty"`
	Credential *Credential `gorm:"foreignKey:CredentialID" json:"credential,omitempty"`
	Binder     *User       `gorm:"foreignKey:BoundBy" json:"binder,omitempty"`
}

type ResourceHealthSnapshot struct {
	BaseModel
	WorkspaceID uint64 `gorm:"not null;index" json:"workspace_id"`
	ResourceID  uint64 `gorm:"not null;index" json:"resource_id"`
	Status      string `gorm:"size:32;not null;index" json:"status"`
	Summary     string `gorm:"type:text" json:"summary"`
	Metrics     string `gorm:"type:longtext" json:"metrics"`
	ObservedAt  int64  `gorm:"index" json:"observed_at"`

	Resource *Resource `gorm:"foreignKey:ResourceID" json:"resource,omitempty"`
}

type ResourceOperationStatus string

const (
	ResourceOperationStatusQueued    ResourceOperationStatus = "queued"
	ResourceOperationStatusRunning   ResourceOperationStatus = "running"
	ResourceOperationStatusSuccess   ResourceOperationStatus = "success"
	ResourceOperationStatusFailed    ResourceOperationStatus = "failed"
	ResourceOperationStatusCancelled ResourceOperationStatus = "cancelled"
)

type ResourceOperationAudit struct {
	BaseModel
	WorkspaceID     uint64                  `gorm:"not null;index" json:"workspace_id"`
	ResourceID      uint64                  `gorm:"not null;index" json:"resource_id"`
	ResourceType    string                  `gorm:"size:32;not null;index" json:"resource_type"`
	Domain          string                  `gorm:"size:32;not null;index" json:"domain"`
	Namespace       string                  `gorm:"size:128;index" json:"namespace"`
	TargetKind      string                  `gorm:"size:64;index" json:"target_kind"`
	TargetName      string                  `gorm:"size:255;index" json:"target_name"`
	Action          string                  `gorm:"size:64;not null;index" json:"action"`
	Reason          string                  `gorm:"type:text" json:"reason"`
	RequestSnapshot string                  `gorm:"type:longtext" json:"request_snapshot"`
	ResultSummary   string                  `gorm:"type:text" json:"result_summary"`
	ErrorMessage    string                  `gorm:"type:text" json:"error_message"`
	TaskID          *uint64                 `gorm:"index" json:"task_id"`
	Status          ResourceOperationStatus `gorm:"size:32;not null;index" json:"status"`
	CreatedBy       uint64                  `gorm:"not null;index" json:"created_by"`
	CompletedAt     int64                   `gorm:"index" json:"completed_at"`

	Resource *Resource  `gorm:"foreignKey:ResourceID" json:"resource,omitempty"`
	Creator  *User      `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Task     *AgentTask `gorm:"foreignKey:TaskID" json:"task,omitempty"`
}

type StoreTemplateType string

const (
	StoreTemplateTypeApp StoreTemplateType = "app"
	StoreTemplateTypeLLM StoreTemplateType = "llm"
)

type StoreTemplateSource string

const (
	StoreTemplateSourcePlatform  StoreTemplateSource = "platform"
	StoreTemplateSourceWorkspace StoreTemplateSource = "workspace"
)

type StoreTemplateStatus string

const (
	StoreTemplateStatusDraft       StoreTemplateStatus = "draft"
	StoreTemplateStatusPublished   StoreTemplateStatus = "published"
	StoreTemplateStatusUnpublished StoreTemplateStatus = "unpublished"
)

type StoreTemplate struct {
	BaseModel
	WorkspaceID        uint64              `gorm:"index" json:"workspace_id"`
	Name               string              `gorm:"size:128;not null;index" json:"name"`
	Description        string              `gorm:"type:text" json:"description"`
	Category           string              `gorm:"size:64;default:'';index" json:"category"`
	TemplateType       StoreTemplateType   `gorm:"size:32;not null;index" json:"template_type"`
	TargetResourceType ResourceType        `gorm:"size:32;not null;index" json:"target_resource_type"`
	Source             StoreTemplateSource `gorm:"size:32;not null;index" json:"source"`
	Status             StoreTemplateStatus `gorm:"size:32;not null;index" json:"status"`
	Summary            string              `gorm:"size:512" json:"summary"`
	Icon               string              `gorm:"size:255" json:"icon"`
	CreatedBy          uint64              `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace             `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Creator   *User                  `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Versions  []StoreTemplateVersion `gorm:"foreignKey:TemplateID" json:"versions,omitempty"`
}

type StoreTemplateVersion struct {
	BaseModel
	WorkspaceID       uint64              `gorm:"index" json:"workspace_id"`
	TemplateID        uint64              `gorm:"not null;index" json:"template_id"`
	PipelineID        uint64              `gorm:"index" json:"pipeline_id"`
	Version           string              `gorm:"size:64;not null;index" json:"version"`
	DeploymentMode    string              `gorm:"size:64;not null" json:"deployment_mode"`
	DefaultConfig     string              `gorm:"type:longtext" json:"default_config"`
	DependencyConfig  string              `gorm:"type:longtext" json:"dependency_config"`
	TargetConstraints string              `gorm:"type:longtext" json:"target_constraints"`
	Status            StoreTemplateStatus `gorm:"size:32;not null;index" json:"status"`
	CreatedBy         uint64              `gorm:"not null;index" json:"created_by"`

	Template   *StoreTemplate      `gorm:"foreignKey:TemplateID" json:"template,omitempty"`
	Pipeline   *Pipeline           `gorm:"foreignKey:PipelineID" json:"pipeline,omitempty"`
	Creator    *User               `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
	Parameters []TemplateParameter `gorm:"foreignKey:TemplateVersionID" json:"parameters,omitempty"`
}

type TemplateParameter struct {
	BaseModel
	TemplateVersionID uint64 `gorm:"not null;index" json:"template_version_id"`
	Name              string `gorm:"size:128;not null" json:"name"`
	Label             string `gorm:"size:128;not null" json:"label"`
	Description       string `gorm:"type:text" json:"description"`
	Type              string `gorm:"size:32;not null" json:"type"`
	DefaultValue      string `gorm:"type:text" json:"default_value"`
	OptionValues      string `gorm:"type:longtext" json:"option_values"`
	Required          bool   `gorm:"default:false" json:"required"`
	Mutable           bool   `gorm:"default:true" json:"mutable"`
	Advanced          bool   `gorm:"default:false" json:"advanced"`
	SortOrder         int    `gorm:"default:0" json:"sort_order"`

	TemplateVersion *StoreTemplateVersion `gorm:"foreignKey:TemplateVersionID" json:"template_version,omitempty"`
}

type LLMModelCatalog struct {
	BaseModel
	Name          string `gorm:"size:128;not null;index" json:"name"`
	DisplayName   string `gorm:"size:255" json:"display_name"`
	Source        string `gorm:"size:32;not null;index:idx_llm_model_catalog_source_ref,priority:1" json:"source"`
	SourceModelID string `gorm:"size:255;not null;index:idx_llm_model_catalog_source_ref,priority:2" json:"source_model_id"`
	ParameterSize string `gorm:"size:64" json:"parameter_size"`
	Summary       string `gorm:"type:text" json:"summary"`
	License       string `gorm:"size:128" json:"license"`
	Tags          string `gorm:"type:longtext" json:"tags"`
	Metadata      string `gorm:"type:longtext" json:"metadata"`
	ImportedBy    uint64 `gorm:"not null;index" json:"imported_by"`

	Importer *User `gorm:"foreignKey:ImportedBy" json:"importer,omitempty"`
}

type DeploymentRequestStatus string

const (
	DeploymentRequestStatusPending    DeploymentRequestStatus = "pending"
	DeploymentRequestStatusValidating DeploymentRequestStatus = "validating"
	DeploymentRequestStatusQueued     DeploymentRequestStatus = "queued"
	DeploymentRequestStatusRunning    DeploymentRequestStatus = "running"
	DeploymentRequestStatusSuccess    DeploymentRequestStatus = "success"
	DeploymentRequestStatusFailed     DeploymentRequestStatus = "failed"
	DeploymentRequestStatusCancelled  DeploymentRequestStatus = "cancelled"
)

type DeploymentRequest struct {
	BaseModel
	WorkspaceID             uint64                  `gorm:"not null;index" json:"workspace_id"`
	ProjectID               *uint64                 `gorm:"index" json:"project_id"`
	TemplateID              uint64                  `gorm:"not null;index" json:"template_id"`
	TemplateVersionID       uint64                  `gorm:"not null;index" json:"template_version_id"`
	TemplateType            StoreTemplateType       `gorm:"size:32;not null;index" json:"template_type"`
	TargetResourceID        uint64                  `gorm:"not null;index" json:"target_resource_id"`
	TargetResourceType      ResourceType            `gorm:"size:32;not null;index" json:"target_resource_type"`
	ParameterSnapshot       string                  `gorm:"type:longtext" json:"parameter_snapshot"`
	ResourceSnapshot        string                  `gorm:"type:longtext" json:"resource_snapshot"`
	TemplateVersionSnapshot string                  `gorm:"type:longtext" json:"template_version_snapshot"`
	LLMModelID              uint64                  `gorm:"index" json:"llm_model_id"`
	LLMModelSnapshot        string                  `gorm:"type:longtext" json:"llm_model_snapshot"`
	ValidationError         string                  `gorm:"type:text" json:"validation_error"`
	Status                  DeploymentRequestStatus `gorm:"size:32;not null;index" json:"status"`
	PipelineID              uint64                  `gorm:"index" json:"pipeline_id"`
	PipelineRunID           uint64                  `gorm:"index" json:"pipeline_run_id"`
	RequestedBy             uint64                  `gorm:"not null;index" json:"requested_by"`

	Workspace       *Workspace            `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Project         *Project              `gorm:"foreignKey:ProjectID" json:"project,omitempty"`
	Template        *StoreTemplate        `gorm:"foreignKey:TemplateID" json:"template,omitempty"`
	TemplateVersion *StoreTemplateVersion `gorm:"foreignKey:TemplateVersionID" json:"template_version,omitempty"`
	LLMModel        *LLMModelCatalog      `gorm:"foreignKey:LLMModelID" json:"llm_model,omitempty"`
	TargetResource  *Resource             `gorm:"foreignKey:TargetResourceID" json:"target_resource,omitempty"`
	Requester       *User                 `gorm:"foreignKey:RequestedBy" json:"requester,omitempty"`
	Records         []DeploymentRecord    `gorm:"foreignKey:RequestID" json:"records,omitempty"`
}

type DeploymentRecord struct {
	BaseModel
	WorkspaceID      uint64                  `gorm:"not null;index" json:"workspace_id"`
	RequestID        uint64                  `gorm:"not null;index" json:"request_id"`
	PipelineRunID    uint64                  `gorm:"index" json:"pipeline_run_id"`
	Status           DeploymentRequestStatus `gorm:"size:32;not null;index" json:"status"`
	LogPath          string                  `gorm:"size:512" json:"log_path"`
	AuditSummary     string                  `gorm:"type:text" json:"audit_summary"`
	ResultSummary    string                  `gorm:"type:text" json:"result_summary"`
	EffectSnapshot   string                  `gorm:"type:longtext" json:"effect_snapshot"`
	RetryCount       int                     `gorm:"default:0" json:"retry_count"`
	FailureReason    string                  `gorm:"type:text" json:"failure_reason"`
	ExecutionTaskRef string                  `gorm:"size:128" json:"execution_task_ref"`

	Request *DeploymentRequest `gorm:"foreignKey:RequestID" json:"request,omitempty"`
}
