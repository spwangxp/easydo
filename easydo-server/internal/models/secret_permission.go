package models

// SecretPermission 密钥权限
type SecretPermission struct {
	BaseModel
	RoleID   uint64 `gorm:"index;not null" json:"role_id"`
	SecretID uint64 `gorm:"index;not null" json:"secret_id"`
	CanRead   bool `gorm:"default:false" json:"can_read"`
	CanUpdate bool `gorm:"default:false" json:"can_update"`
	CanDelete bool `gorm:"default:false" json:"can_delete"`
	CanUse    bool `gorm:"default:false" json:"can_use"`
	CanRotate bool `gorm:"default:false" json:"can_rotate"`
}

func (SecretPermission) TableName() string {
	return "secret_permissions"
}
