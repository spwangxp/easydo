package models

type Pipeline struct {
	BaseModel
	Name        string `gorm:"size:128;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Config      string `gorm:"type:longtext" json:"config"`
	ProjectID   uint64 `gorm:"index" json:"project_id"`
	OwnerID     uint64 `gorm:"not null;index" json:"owner_id"`
	Environment string `gorm:"size:32;default:'development'" json:"environment"`
	IsPublic    bool   `gorm:"default:false" json:"is_public"`
	IsFavorite  bool   `gorm:"default:false" json:"is_favorite"`
	
	Project     *Project      `gorm:"foreignKey:ProjectID" json:"project"`
	Owner       *User         `gorm:"foreignKey:OwnerID" json:"owner"`
	Runs        []PipelineRun `gorm:"foreignKey:PipelineID" json:"runs"`
}
