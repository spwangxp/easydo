package models

type TaskDefinition struct {
	TaskKey         string                         `json:"task_key"`
	Name            string                         `json:"name"`
	Description     string                         `json:"description,omitempty"`
	Category        string                         `json:"category"`
	Status          string                         `json:"status,omitempty"`
	Version         int                            `json:"version"`
	ExecutorType    string                         `json:"executor_type"`
	FieldsSchema    []TaskDefinitionField          `json:"fields_schema,omitempty"`
	OutputsSchema   []TaskDefinitionOutput         `json:"outputs_schema,omitempty"`
	CredentialSlots []TaskCredentialSlotDefinition `json:"credential_slots,omitempty"`
	ExecutionSpec   TaskExecutionSpec              `json:"execution_spec"`
}

type TaskDefinitionField struct {
	Key           string        `json:"key"`
	Label         string        `json:"label"`
	Type          string        `json:"type"`
	Required      bool          `json:"required"`
	Default       interface{}   `json:"default,omitempty"`
	Description   string        `json:"description,omitempty"`
	UIComponent   string        `json:"ui_component,omitempty"`
	UIPlaceholder string        `json:"ui_placeholder,omitempty"`
	Options       []FieldOption `json:"options,omitempty"`
	Secret        bool          `json:"secret"`
	Readonly      bool          `json:"readonly"`
}

type FieldOption struct {
	Label string      `json:"label"`
	Value interface{} `json:"value"`
}

type TaskDefinitionOutput struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type TaskCredentialSlotDefinition struct {
	SlotKey           string               `json:"slot_key"`
	Label             string               `json:"label"`
	Required          bool                 `json:"required"`
	AllowedTypes      []CredentialType     `json:"allowed_types,omitempty"`
	AllowedCategories []CredentialCategory `json:"allowed_categories,omitempty"`
}

type TaskExecutionSpec struct {
	Mode           string            `json:"mode"`
	Entry          string            `json:"entry,omitempty"`
	ScriptTemplate string            `json:"script_template,omitempty"`
	EnvMapping     map[string]string `json:"env_mapping,omitempty"`
	TimeoutDefault int               `json:"timeout_default,omitempty"`
	RetryDefault   int               `json:"retry_default,omitempty"`
}

type PipelineDefinitionParam struct {
	Key        string      `json:"key"`
	Label      string      `json:"label,omitempty"`
	Value      interface{} `json:"value"`
	IsFlexible bool        `json:"is_flexible"`
}

type PipelineRunConfigSnapshot struct {
	Trigger PipelineRunTriggerSnapshot        `json:"trigger,omitempty"`
	Inputs  map[string]map[string]interface{} `json:"inputs,omitempty"`
	Options map[string]interface{}            `json:"options,omitempty"`
}

type PipelineRunTriggerSnapshot struct {
	Type     string `json:"type,omitempty"`
	Source   string `json:"source,omitempty"`
	Operator string `json:"operator,omitempty"`
}
