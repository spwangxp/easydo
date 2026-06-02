package models

import (
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	BaseModel
	Username    string `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Password    string `gorm:"size:128;not null" json:"-"`
	Email       string `gorm:"size:128" json:"email"`
	Phone       string `gorm:"size:20" json:"phone"`
	Nickname    string `gorm:"size:64" json:"nickname"`
	Avatar      string `gorm:"size:256" json:"avatar"`
	Bio         string `gorm:"type:text" json:"bio"`
	Role        string `gorm:"size:32;default:'user'" json:"role"`
	Status      string `gorm:"size:32;default:'active'" json:"status"`
	LastLoginAt int64  `json:"last_login_at"`

	MustChangePassword bool    `gorm:"column:must_change_password;default:false" json:"must_change_password"`
	PasswordChangedAt  int64   `gorm:"column:password_changed_at" json:"password_changed_at"`
	DisabledAt         int64   `gorm:"column:disabled_at" json:"disabled_at"`
	DisabledBy         *uint64 `gorm:"column:disabled_by;index" json:"disabled_by"`

	Projects         []Project         `gorm:"foreignKey:OwnerID" json:"projects"`
	Pipelines        []Pipeline        `gorm:"foreignKey:OwnerID" json:"pipelines"`
	DeployRecords    []DeployRecord    `gorm:"foreignKey:DeployerID" json:"deploy_records"`
	WorkspaceMembers []WorkspaceMember `gorm:"foreignKey:UserID" json:"workspace_members"`
}

// SetPassword 加密密码
func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hash)
	return nil
}

// CheckPassword 验证密码
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
	return err == nil
}
