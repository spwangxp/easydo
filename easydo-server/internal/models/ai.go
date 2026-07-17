package models

import "time"

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

type AIProvider struct {
	BaseModel
	WorkspaceID  uint64           `gorm:"not null;index" json:"workspace_id"`
	Name         string           `gorm:"size:128;not null;index" json:"name"`
	Description  string           `gorm:"type:text" json:"description"`
	ProviderType string           `gorm:"size:64;not null;index" json:"provider_type"`
	BaseURL      string           `gorm:"size:512" json:"base_url"`
	CredentialID *uint64          `gorm:"index" json:"credential_id"`
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
	WorkspaceID             uint64               `gorm:"not null;index" json:"workspace_id"`
	ModelID                 uint64               `gorm:"not null;index" json:"model_id"`
	ProviderID              uint64               `gorm:"not null;index" json:"provider_id"`
	ProviderModelKey        string               `gorm:"size:255" json:"provider_model_key"`
	SettingsJSON            string               `gorm:"type:longtext" json:"settings_json"`
	MetadataJSON            string               `gorm:"type:longtext" json:"metadata_json"`
	ContextWindowTokens     *uint64              `gorm:"column:context_window_tokens" json:"context_window_tokens,omitempty"`
	MaxOutputTokens         *uint64              `gorm:"column:max_output_tokens" json:"max_output_tokens,omitempty"`
	SupportsToolUse         bool                 `gorm:"column:supports_tool_use;not null;default:false" json:"supports_tool_use"`
	SupportsStreaming       bool                 `gorm:"column:supports_streaming;not null;default:true" json:"supports_streaming"`
	CapabilitySource        string               `gorm:"size:64" json:"capability_source,omitempty"`
	CapabilityCheckedAt     *time.Time           `gorm:"column:capability_checked_at" json:"capability_checked_at,omitempty"`
	CapabilitySnapshotHash  string               `gorm:"size:71" json:"capability_snapshot_hash,omitempty"`
	Status                  AIModelBindingStatus `gorm:"size:32;not null;default:'active';index" json:"status"`
	CreatedBy               uint64               `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace      `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Model     *AIModelCatalog `gorm:"foreignKey:ModelID" json:"model,omitempty"`
	Provider  *AIProvider     `gorm:"foreignKey:ProviderID" json:"provider,omitempty"`
	Creator   *User           `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (AIModelBinding) TableName() string {
	return "ai_model_bindings"
}
