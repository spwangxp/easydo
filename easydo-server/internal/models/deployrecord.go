package models

type DeployRecord struct {
	BaseModel
	ProjectID   uint64 `gorm:"index;not null" json:"project_id"`
	PipelineID  uint64 `gorm:"index" json:"pipeline_id"`
	DeployerID  uint64 `gorm:"index" json:"deployer_id"`
	AppName     string `gorm:"size:64;not null" json:"app_name"`
	Version     string `gorm:"size:64;not null" json:"version"`
	Environment string `gorm:"size:32;not null" json:"environment"`
	Status      string `gorm:"size:32;not null" json:"status"`
	DeployType  string `gorm:"size:32" json:"deploy_type"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	
	Project      *Project  `gorm:"foreignKey:ProjectID" json:"project"`
	Pipeline     *Pipeline `gorm:"foreignKey:PipelineID" json:"pipeline"`
	Deployer     *User     `gorm:"foreignKey:DeployerID" json:"deployer"`
}
