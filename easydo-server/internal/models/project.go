package models

type Project struct {
	BaseModel
	Name        string `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Color       string `gorm:"size:20" json:"color"`
	OwnerID     uint64 `gorm:"not null;index" json:"owner_id"`
	IsFavorited bool   `gorm:"default:false" json:"is_favorited"`
	
	Owner       *User       `gorm:"foreignKey:OwnerID" json:"owner"`
	Pipelines   []Pipeline  `gorm:"foreignKey:ProjectID" json:"pipelines"`
	DeployRecords []DeployRecord `gorm:"foreignKey:ProjectID" json:"deploy_records"`
}
