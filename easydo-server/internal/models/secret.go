package models

// SecretType 密钥类型
type SecretType string

const (
	SecretTypeSSH         SecretType = "ssh"
	SecretTypeToken       SecretType = "token"
	SecretTypeRegistry    SecretType = "registry"
	SecretTypeAPIKey      SecretType = "api_key"
	SecretTypeKubernetes  SecretType = "kubernetes"
	SecretTypeCertificate SecretType = "certificate"
)

// SecretCategory 密钥分类
type SecretCategory string

const (
	SecretCategoryGitHub     SecretCategory = "github"
	SecretCategoryGitLab     SecretCategory = "gitlab"
	SecretCategoryGitee      SecretCategory = "gitee"
	SecretCategoryDocker     SecretCategory = "docker"
	SecretCategoryDingTalk   SecretCategory = "dingtalk"
	SecretCategoryWeChat     SecretCategory = "wechat"
	SecretCategoryKubernetes SecretCategory = "kubernetes"
	SecretCategoryCustom     SecretCategory = "custom"
)

// SecretScope 密钥使用范围
type SecretScope string

const (
	SecretScopeAll     SecretScope = "all"
	SecretScopeProject SecretScope = "project"
)

// SecretStatus 密钥状态
type SecretStatus string

const (
	SecretStatusActive   SecretStatus = "active"
	SecretStatusInactive SecretStatus = "inactive"
	SecretStatusExpired  SecretStatus = "expired"
	SecretStatusRevoked  SecretStatus = "revoked"
)

// Secret 密钥表
type Secret struct {
	BaseModel

	Name        string         `gorm:"size:128;not null;index" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	Type        SecretType     `gorm:"size:32;not null;index" json:"type"`
	Category    SecretCategory `gorm:"size:32;index" json:"category"`

	EncryptedValue string `gorm:"type:longtext;not null" json:"-"`
	EncryptionIV   string `gorm:"size:32" json:"-"`
	EncryptionAlgo string `gorm:"size:16;default:'aes-256-gcm'" json:"-"`

	Metadata string `gorm:"type:text" json:"metadata"`

	Scope       SecretScope `gorm:"size:32;default:'all'" json:"scope"`
	WorkspaceID uint64      `gorm:"not null;index" json:"workspace_id"`
	ProjectID   uint64      `gorm:"index" json:"project_id"`
	IsShared    bool        `gorm:"default:false" json:"is_shared"`
	SharedWith  string      `gorm:"type:text" json:"shared_with"`

	CreatedBy  uint64       `gorm:"not null;index" json:"created_by"`
	LastUsedAt int64        `json:"last_used_at"`
	UsedCount  int64        `gorm:"default:0" json:"used_count"`
	Version    int          `gorm:"default:1" json:"version"`
	Status     SecretStatus `gorm:"size:32;default:'active';index" json:"status"`

	ExpiresAt     *int64 `json:"expires_at"`
	ExpiryWarning int    `gorm:"default:7" json:"expiry_warning"`
	AutoRotate    bool   `gorm:"default:false" json:"auto_rotate"`
	RotatePeriod  int    `gorm:"default:0" json:"rotate_period"`
}

func (Secret) TableName() string {
	return "secrets"
}

// SecretUsage 密钥使用记录表
type SecretUsage struct {
	BaseModel

	SecretID   uint64 `gorm:"index;not null" json:"secret_id"`
	UsedByType string `gorm:"size:32;index" json:"used_by_type"`
	UsedByID   uint64 `gorm:"index" json:"used_by_id"`
	UsedByName string `gorm:"size:256" json:"used_by_name"`
	UsedAt     int64  `gorm:"not null;index" json:"used_at"`
	Result     string `gorm:"size:32" json:"result"`
	ErrorMsg   string `gorm:"type:text" json:"error_msg"`
}

func (SecretUsage) TableName() string {
	return "secret_usages"
}

// SecretAuditLog 密钥审计日志表
type SecretAuditLog struct {
	BaseModel

	SecretID uint64 `gorm:"index;not null" json:"secret_id"`
	Action   string `gorm:"size:64;not null;index" json:"action"`
	ActorID  uint64 `gorm:"index" json:"actor_id"`
	ActorIP  string `gorm:"size:45" json:"actor_ip"`
	ActorUA  string `gorm:"size:512" json:"actor_ua"`
	Metadata string `gorm:"type:text" json:"metadata"`
}

func (SecretAuditLog) TableName() string {
	return "secret_audit_logs"
}

// AuditAction 审计动作常量
const (
	AuditActionCreate  = "create"
	AuditActionUpdate  = "update"
	AuditActionDelete  = "delete"
	AuditActionView    = "view"
	AuditActionUse     = "use"
	AuditActionExport  = "export"
	AuditActionDisable = "disable"
	AuditActionEnable  = "enable"
)
