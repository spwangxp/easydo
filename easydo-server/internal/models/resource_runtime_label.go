package models

type ResourceRuntimeLabel struct {
	BaseModel
	WorkspaceID uint64 `gorm:"not null;uniqueIndex:idx_resource_runtime_labels_target,priority:1;index:idx_resource_runtime_labels_workspace_target,priority:1" json:"workspace_id"`
	ResourceID  uint64 `gorm:"not null;uniqueIndex:idx_resource_runtime_labels_target,priority:2;index:idx_resource_runtime_labels_resource_target,priority:1" json:"resource_id"`
	TargetType  string `gorm:"size:64;not null;uniqueIndex:idx_resource_runtime_labels_target,priority:3;index:idx_resource_runtime_labels_workspace_target,priority:2;index:idx_resource_runtime_labels_resource_target,priority:2" json:"target_type"`
	TargetKey   string `gorm:"size:255;not null;uniqueIndex:idx_resource_runtime_labels_target,priority:4;index:idx_resource_runtime_labels_workspace_target,priority:3;index:idx_resource_runtime_labels_resource_target,priority:3" json:"target_key"`
	LabelsJSON  string `gorm:"type:longtext;not null" json:"labels_json"`
	CreatedBy   uint64 `gorm:"not null;index" json:"created_by"`

	Workspace *Workspace `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Resource  *Resource  `gorm:"foreignKey:ResourceID" json:"resource,omitempty"`
	Creator   *User      `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (ResourceRuntimeLabel) TableName() string {
	return "resource_runtime_labels"
}
