package models

type Project struct {
	BaseModel
	Name        string `gorm:"size:64;not null;index:idx_workspace_project_name,unique" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Color       string `gorm:"size:20" json:"color"`
	WorkspaceID uint64 `gorm:"not null;index;index:idx_workspace_project_name,unique" json:"workspace_id"`
	OwnerID     uint64 `gorm:"not null;index" json:"owner_id"`
	IsFavorited bool   `gorm:"default:false" json:"is_favorited"`

	Workspace     *Workspace     `gorm:"foreignKey:WorkspaceID" json:"workspace,omitempty"`
	Owner         *User          `gorm:"foreignKey:OwnerID" json:"owner"`
	Pipelines     []Pipeline     `gorm:"foreignKey:ProjectID" json:"pipelines"`
	DeployRecords []DeployRecord `gorm:"foreignKey:ProjectID" json:"deploy_records"`
}
