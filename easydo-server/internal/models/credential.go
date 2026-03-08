package models

import (
	"time"
)

// =============================================================================
// 身份验证方式汇总表
// =============================================================================
// 方式名称        | 核心原理              | 适用场景                    | 代表系统
// ---------------|----------------------|---------------------------|---------------------------
// 密码           | 知识因素             | 网页登录、旧版数据库       | GitLab, MySQL, Windows
// SSH 密钥       | 非对称加密           | 远程登录、Git 推送         | GitLab, GitHub, Linux Server
// 个人访问令牌   | 短期/长期 Token      | API 调用、插件接入         | GitHub, GitLab, Jira, ADO
// OAuth2/OIDC    | 代理授权/身份层      | 第三方登录、微服务交互     | Google, GitHub, AWS, Okta
// Passkeys       | 生物识别+硬件密钥    | 消除密码的网页登录         | Apple, Google, GitHub
// 双因子认证     | 知识+拥有因素        | 高安全性账户保护           | 几乎所有现代 SaaS 系统
// mTLS           | 证书双向校验         | 零信任网络、微服务         | Kubernetes(Istio), 银行 API
// IAM 角色       | 临时凭证分发         | 云原生资源访问             | AWS, Google Cloud, Azure

// =============================================================================
// CredentialType - 身份验证类型定义
// =============================================================================
// 定义系统支持的所有身份验证方式，每种类型对应不同的认证机制和使用场景

type CredentialType string

const (
	// TypePassword - 密码认证
	// 核心原理：基于用户名+密码的知识因素认证
	// 适用场景：传统数据库登录、旧系统集成、内部服务认证
	// 典型系统：GitLab, MySQL, Windows Server, 企业内部系统
	TypePassword CredentialType = "PASSWORD"

	// TypeSSHKey - SSH 密钥对认证
	// 核心原理：基于 RSA/Ed25519 的非对称加密认证
	// 适用场景：远程服务器登录、Git 仓库推送、自动化部署
	// 典型系统：GitLab, GitHub, Linux Server, AWS EC2
	TypeSSHKey CredentialType = "SSH_KEY"

	// TypeToken - API 访问令牌认证
	// 核心原理：基于 Bearer/Basic Token 的 API 授权
	// 适用场景：REST API 调用、CLI 工具认证、第三方集成
	// 典型系统：GitHub Personal Access Token, GitLab API Token, Jira API Token
	TypeToken CredentialType = "TOKEN"

	// TypeOAuth2 - OAuth2 授权认证
	// 核心原理：基于授权码/客户端凭证的代理授权模式
	// 适用场景：第三方应用登录、跨平台 SSO 集成、微服务间认证
	// 典型系统：Google OAuth, GitHub OAuth, AWS Cognito, Okta
	TypeOAuth2 CredentialType = "OAUTH2"

	// TypeCertificate - X.509 证书认证
	// 核心原理：基于数字证书的双向 TLS 认证
	// 适用场景：mTLS 通信、VPN 接入、企业 PKI 体系
	// 典型系统：Istio mTLS, VPN Gateway, 企业内部 CA
	TypeCert CredentialType = "CERTIFICATE"

	// TypePasskey - WebAuthn/Passkey 认证
	// 核心原理：基于 FIDO2 标准的硬件安全密钥或生物识别
	// 适用场景：无密码登录、高安全性企业应用、消费者产品
	// 典型系统：Apple Passkey, Google Passkey, GitHub Passkey
	TypePasskey CredentialType = "PASSKEY"

	// TypeMFA - 多因素认证
	// 核心原理：知识因素（密码）+ 拥有因素（TOTP/硬件令牌）
	// 适用场景：增强账户安全、合规要求、敏感操作二次验证
	// 典型系统：所有现代 SaaS 系统的二次验证
	TypeMFA CredentialType = "MFA"

	// TypeIAMRole - 云平台 IAM 角色认证
	// 核心原理：基于云平台临时安全凭证的角色授权
	// 适用场景：云原生应用、跨服务访问、自动化 CI/CD
	// 典型系统：AWS IAM Role, Google Cloud Service Account, Azure Managed Identity
	TypeIAM CredentialType = "IAM_ROLE"
)

// =============================================================================
// CredentialCategory - 凭据分类定义
// =============================================================================
// 按目标系统/平台对凭据进行分类，便于管理和筛选

type CredentialCategory string

const (
	// 代码托管平台
	CategoryGitHub CredentialCategory = "github" // GitHub
	CategoryGitLab CredentialCategory = "gitlab" // GitLab
	CategoryGitee  CredentialCategory = "gitee"  // Gitee

	// 容器平台
	CategoryDocker     CredentialCategory = "docker"     // Docker Registry
	CategoryKubernetes CredentialCategory = "kubernetes" // Kubernetes

	// 协作平台
	CategoryDingTalk CredentialCategory = "dingtalk" // 钉钉
	CategoryWeChat   CredentialCategory = "wechat"   // 企业微信

	// 云平台
	CategoryAWS   CredentialCategory = "aws"   // Amazon Web Services
	CategoryGCP   CredentialCategory = "gcp"   // Google Cloud Platform
	CategoryAzure CredentialCategory = "azure" // Microsoft Azure

	// 其他
	CategoryEmail  CredentialCategory = "email"  // 邮件服务
	CategoryCustom CredentialCategory = "custom" // 自定义
)

// =============================================================================
// CredentialScope - 凭据使用范围定义
// =============================================================================
// 定义凭据的可见性和使用权限范围

type CredentialScope string

const (
	ScopeUser    CredentialScope = "user"    // 仅创建者可用
	ScopeProject CredentialScope = "project" // 项目成员可用
	ScopeGlobal  CredentialScope = "global"  // 全局共享（管理员控制）
)

// =============================================================================
// CredentialStatus - 凭据状态定义
// =============================================================================
// 定义凭据的生命周期状态

type CredentialStatus string

const (
	CredentialStatusActive   CredentialStatus = "active"   // 正常使用中
	CredentialStatusInactive CredentialStatus = "inactive" // 已禁用
	CredentialStatusExpired  CredentialStatus = "expired"  // 已过期
	CredentialStatusRevoked  CredentialStatus = "revoked"  // 已撤销/吊销
)

type Credential struct {
	BaseModel
	Name           string             `gorm:"size:128;not null;index" json:"name"`
	Description    string             `gorm:"size:512" json:"description"`
	Type           CredentialType     `gorm:"size:32;not null;index" json:"type"`
	Category       CredentialCategory `gorm:"size:32;index" json:"category"`
	EncryptedData  string             `gorm:"type:longtext;not null" json:"-"`
	EncryptionIV   string             `gorm:"size:32" json:"-"`
	EncryptionAlgo string             `gorm:"size:16;default:'aes-256-gcm'" json:"-"`
	Metadata       string             `gorm:"type:text" json:"metadata"`
	Scope          CredentialScope    `gorm:"size:32;default:'user'" json:"scope"`
	ProjectID      uint64             `gorm:"index" json:"project_id"`
	IsShared       bool               `gorm:"default:false" json:"is_shared"`
	SharedWith     string             `gorm:"type:text" json:"shared_with"`
	OwnerID        uint64             `gorm:"not null;index" json:"owner_id"`
	LastUsedAt     int64              `json:"last_used_at"`
	UsedCount      int64              `gorm:"default:0" json:"used_count"`
	Version        int                `gorm:"default:1" json:"version"`
	Status         CredentialStatus   `gorm:"size:32;default:'active';index" json:"status"`
	ExpiresAt      *int64             `json:"expires_at"`
	ExpiryWarning  int                `gorm:"default:7" json:"expiry_warning"`
	AutoRotate     bool               `gorm:"default:false" json:"auto_rotate"`
	RotatePeriod   int                `gorm:"default:0" json:"rotate_period"`
}

func (Credential) TableName() string {
	return "credentials"
}

// CredentialResponse - 凭据响应结构
type CredentialResponse struct {
	ID           uint64             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Type         CredentialType     `json:"type"`
	Category     CredentialCategory `json:"category"`
	Scope        CredentialScope    `json:"scope"`
	ProjectID    uint64             `json:"project_id"`
	OwnerID      uint64             `json:"owner_id"`
	Status       CredentialStatus   `json:"status"`
	TypeInfo     TypeInfo           `json:"type_info"`
	CategoryInfo CategoryInfo       `json:"category_info"`
	StatusInfo   StatusInfo         `json:"status_info"`
	ExpiresAt    *int64             `json:"expires_at"`
	UsedCount    int64              `json:"used_count"`
	LastUsedAt   int64              `json:"last_used_at"`
	Version      int                `json:"version"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// ToResponse - 将模型转换为响应结构
func (c *Credential) ToResponse() CredentialResponse {
	return CredentialResponse{
		ID:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		Type:         c.Type,
		Category:     c.Category,
		Scope:        c.Scope,
		ProjectID:    c.ProjectID,
		OwnerID:      c.OwnerID,
		Status:       c.Status,
		TypeInfo:     c.Type.GetTypeInfo(),
		CategoryInfo: c.Category.GetCategoryInfo(),
		StatusInfo:   c.Status.GetStatusInfo(),
		ExpiresAt:    c.ExpiresAt,
		UsedCount:    c.UsedCount,
		LastUsedAt:   c.LastUsedAt,
		Version:      c.Version,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

type CredentialUsage struct {
	BaseModel
	CredentialID uint64 `gorm:"index;not null" json:"credential_id"`
	UsedByType   string `gorm:"size:32;index" json:"used_by_type"`
	UsedByID     uint64 `gorm:"index" json:"used_by_id"`
	UsedByName   string `gorm:"size:256" json:"used_by_name"`
	UsedAt       int64  `gorm:"not null;index" json:"used_at"`
	Result       string `gorm:"size:32" json:"result"`
	ErrorMsg     string `gorm:"type:text" json:"error_msg"`
}

func (CredentialUsage) TableName() string {
	return "credential_usages"
}

type CredentialAuditLog struct {
	BaseModel
	CredentialID uint64 `gorm:"index;not null" json:"credential_id"`
	Action       string `gorm:"size:64;not null;index" json:"action"`
	ActorID      uint64 `gorm:"index" json:"actor_id"`
	ActorIP      string `gorm:"size:45" json:"actor_ip"`
	ActorUA      string `gorm:"size:512" json:"actor_ua"`
	Metadata     string `gorm:"type:text" json:"metadata"`
}

func (CredentialAuditLog) TableName() string {
	return "credential_audit_logs"
}

// =============================================================================
// 审计操作常量
// =============================================================================

const (
	AuditActionShare   = "share"   // 分享凭据
	AuditActionUnshare = "unshare" // 取消分享
	AuditActionVerify  = "verify"  // 验证凭据
	AuditActionRotate  = "rotate"  // 轮换凭据
)

// =============================================================================
// Payload 结构定义 - 各认证方式的敏感数据结构
// =============================================================================
// 注意：所有 Payload 结构中的字段在存储前会被加密
// 验证规则使用 JSON Schema 风格的结构标签

// PasswordPayload - 密码认证载荷
// 用于传统的用户名+密码认证方式
type PasswordPayload struct {
	// Username - 登录用户名
	// 验证：非空，最小长度 1，最大长度 128
	Username string `json:"username" validate:"required,min=1,max=128"`

	// Password - 登录密码
	// 验证：非空，最小长度 1（具体最小长度由安全策略决定）
	Password string `json:"password" validate:"required,min=1"`

	// Domain - 认证域（可选）
	// 用于 Windows LDAP、企业内部域等场景
	Domain string `json:"domain,omitempty" validate:"max=256"`

	// Port - 服务端口（可选）
	// 用于指定特定的服务端口，如 MySQL(3306), PostgreSQL(5432)
	Port int `json:"port,omitempty" validate:"min=1,max=65535"`
}

// SSHKeyPayload - SSH 密钥认证载荷
// 用于 SSH 密钥对认证，支持 RSA/Ed25519/ECDSA
type SSHKeyPayload struct {
	// PrivateKey - SSH 私钥内容
	// 验证：非空，必须是有效的 SSH 私钥格式
	PrivateKey string `json:"private_key" validate:"required"`

	// PublicKey - SSH 公钥内容（可选）
	// 验证：必须是有效的 SSH 公钥格式
	PublicKey string `json:"public_key,omitempty" validate:"omitempty"`

	// Passphrase - 私钥密码（可选）
	// 用于加密私钥的密码，如果私钥已加密则需要提供
	Passphrase string `json:"passphrase,omitempty" validate:"max=256"`

	// KeyType - 密钥类型
	// 可选值：rsa, ed25519, ecdsa
	KeyType string `json:"key_type" validate:"required,oneof=rsa ed25519 ecdsa"`
}

// TokenPayload - API 令牌认证载荷
// 用于 Personal Access Token (PAT) 或 API Token 认证
type TokenPayload struct {
	// Token - 令牌值
	// 验证：非空，通常是 ghp_/glptt_/eyJ... 等格式
	Token string `json:"token" validate:"required,min=1"`

	// TokenType - 令牌类型
	// 可选值：bearer, basic
	TokenType string `json:"token_type" validate:"required,oneof=bearer basic"`

	// Scopes - 权限范围（可选）
	// 用于细粒度的权限控制，如 repo, workflow, admin:repo_hook
	Scopes []string `json:"scopes,omitempty"`

	// ExpiresAt - 过期时间戳（可选）
	// Unix 时间戳，-1 表示永不过期
	ExpiresAt int64 `json:"expires_at,omitempty"`
}

// OAuth2Payload - OAuth2 认证载荷
// 用于 OAuth2 授权码模式或客户端凭证模式
type OAuth2Payload struct {
	// ClientID - OAuth2 客户端 ID
	// 验证：非空
	ClientID string `json:"client_id" validate:"required"`

	// ClientSecret - OAuth2 客户端密钥
	// 验证：非空
	ClientSecret string `json:"client_secret" validate:"required"`

	// AccessToken - 访问令牌（可选）
	// 授权流程获取的访问令牌
	AccessToken string `json:"access_token,omitempty"`

	// RefreshToken - 刷新令牌（可选）
	// 用于刷新访问令牌的刷新令牌
	RefreshToken string `json:"refresh_token,omitempty"`

	// TokenType - 令牌类型（可选）
	// 通常为 Bearer
	TokenType string `json:"token_type,omitempty"`

	// ExpiresAt - 过期时间戳（可选）
	// Unix 时间戳
	ExpiresAt int64 `json:"expires_at,omitempty"`

	// ProviderURL - OAuth2 提供者 URL
	// 验证：有效的 URL 格式
	ProviderURL string `json:"provider_url" validate:"required,url"`

	// Scope - 授权范围（可选）
	// OAuth2 授权范围，多个用空格分隔
	Scope string `json:"scope,omitempty" validate:"max=1024"`
}

// CertificatePayload - X.509 证书认证载荷
// 用于 mTLS 客户端证书认证
type CertificatePayload struct {
	// CertPEM - 客户端证书 PEM 格式
	// 验证：非空，必须是有效的 X.509 证书
	CertPEM string `json:"cert_pem" validate:"required"`

	// KeyPEM - 私钥 PEM 格式
	// 验证：非空，必须是有效的私钥格式
	KeyPEM string `json:"key_pem" validate:"required"`

	// CertType - 证书类型
	// 可选值：x509, pkcs12, pem
	CertType string `json:"cert_type" validate:"required,oneof=x509 pkcs12 pem"`

	// KeyPassword - 私钥密码（可选）
	// 用于解密 PKCS12 或加密的私钥
	KeyPassword string `json:"key_password,omitempty" validate:"max=256"`

	// CACert - CA 证书（可选）
	// 用于验证服务端证书的 CA 证书
	CACert string `json:"ca_cert,omitempty"`

	// CommonName - 证书主题 CN（可选）
	// X.509 证书的主题通用名称
	CommonName string `json:"common_name,omitempty" validate:"max=64"`

	// NotBefore - 证书生效时间（可选）
	// Unix 时间戳
	NotBefore int64 `json:"not_before,omitempty"`

	// NotAfter - 证书过期时间（可选）
	// Unix 时间戳
	NotAfter int64 `json:"not_after,omitempty"`
}

// PasskeyPayload - Passkey/WebAuthn 认证载荷
// 用于 FIDO2/WebAuthn 无密码认证
type PasskeyPayload struct {
	// CredentialID - 凭据 ID
	// WebAuthn 凭据的唯一标识
	CredentialID string `json:"credential_id" validate:"required"`

	// PublicKey - 公钥凭证
	// COSE 格式的公钥
	PublicKey string `json:"public_key" validate:"required"`

	// AttestationType - 认证类型
	// 可选值：none, basic, self, attCa, anonCa
	AttestationType string `json:"attestation_type" validate:"omitempty,oneof=none basic self attCa anonCa"`

	// AAGUID - 认证器认证 GUID
	// 标识认证器类型
	AAGUID string `json:"aaguid,omitempty"`

	// LastUsedAt - 最后使用时间（可选）
	// Unix 时间戳
	LastUsedAt int64 `json:"last_used_at,omitempty"`
}

// MFAPayload - 多因素认证载荷
// 用于 TOTP 或 SMS 等二次验证
type MFAPayload struct {
	// Secret - TOTP 密钥
	// Base32 编码的 TOTP 密钥
	Secret string `json:"secret" validate:"required"`

	// MFAType - MFA 类型
	// 可选值：totp, sms, email, hardware
	MFAType string `json:"mfa_type" validate:"required,oneof=totp sms email hardware"`

	// Issuer - 签发者名称
	// 用于 TOTP 令牌显示，如 "GitHub", "Google"
	Issuer string `json:"issuer,omitempty" validate:"max=128"`

	// Account - 账户标识
	// 如邮箱地址或手机号
	Account string `json:"account,omitempty" validate:"max=256"`

	// BackupCodes - 备用验证码（可选）
	// 用于 MFA 恢复的一组备用码
	BackupCodes []string `json:"backup_codes,omitempty"`

	// ExpiresAt - 过期时间戳（可选）
	// Unix 时间戳
	ExpiresAt int64 `json:"expires_at,omitempty"`
}

// IAMRolePayload - 云平台 IAM 角色认证载荷
// 用于 AWS/GCP/Azure 的临时安全凭证
type IAMRolePayload struct {
	// RoleARN - 角色 ARN
	// AWS 格式：arn:aws:iam::123456789012:role/MyRole
	// GCP 格式：projects/-/serviceAccounts/my-sa@project.iam.gserviceaccount.com
	// Azure 格式：https://management.azure.com/...（托管身份）
	RoleARN string `json:"role_arn" validate:"required"`

	// Provider - 云平台提供商
	// 可选值：aws, gcp, azure
	Provider string `json:"provider" validate:"required,oneof=aws gcp azure"`

	// AccessKeyID - 访问密钥 ID（可选）
	// AWS 临时凭证的 AccessKeyId
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey - 密钥访问密钥（可选）
	// AWS 临时凭证的 SecretAccessKey
	SecretAccessKey string `json:"secret_access_key,omitempty"`

	// SessionToken - 会话令牌（可选）
	// AWS 临时凭证的 SessionToken
	SessionToken string `json:"session_token,omitempty"`

	// ExpiresAt - 过期时间戳（可选）
	// Unix 时间戳，临时凭证的过期时间
	ExpiresAt int64 `json:"expires_at,omitempty"`

	// Region - 区域（可选）
	// 云服务区域，如 us-east-1, cn-north-1
	Region string `json:"region,omitempty" validate:"max=32"`
}

// ChineseToEnglishTypeMap - 中文类型名到英文类型值的映射
// 前端 API 返回中文类型名，后端需要支持中文类型名验证
var ChineseToEnglishTypeMap = map[string]CredentialType{
	"密码":      TypePassword,
	"SSH 密钥":  TypeSSHKey,
	"API 令牌":  TypeToken,
	"OAuth2":  TypeOAuth2,
	"证书":      TypeCert,
	"Passkey": TypePasskey,
	"多因素认证":   TypeMFA,
	"IAM 角色":  TypeIAM,
}

// NormalizeType - 将中文类型名转换为英文类型值
// 如果已经是英文值则直接返回
func NormalizeType(t CredentialType) CredentialType {
	// 如果是中文类型名，转换为英文
	if english, ok := ChineseToEnglishTypeMap[string(t)]; ok {
		return english
	}
	// 如果是英文类型值，直接返回
	return t
}

// IsValidType - 验证凭据类型是否有效
// 支持中文类型名和英文类型值
func IsValidType(t CredentialType) bool {
	// 标准化类型（中文转英文）
	normalized := NormalizeType(t)
	switch normalized {
	case TypePassword, TypeSSHKey, TypeToken, TypeOAuth2, TypeCert, TypePasskey, TypeMFA, TypeIAM:
		return true
	default:
		return false
	}
}

// GetTypeInfo - 获取凭据类型的详细信息
// 返回类型的显示名称、图标、描述和适用场景
func (t CredentialType) GetTypeInfo() TypeInfo {
	infos := map[CredentialType]TypeInfo{
		TypePassword: {
			Name:        "密码",
			Icon:        "Lock",
			Description: "基于用户名和密码的传统认证方式",
			UseCases:    []string{"数据库登录", "内部系统认证", "传统 API 认证"},
		},
		TypeSSHKey: {
			Name:        "SSH 密钥",
			Icon:        "Key",
			Description: "基于 RSA/Ed25519 非对称加密的密钥认证",
			UseCases:    []string{"服务器远程登录", "Git 仓库操作", "自动化部署"},
		},
		TypeToken: {
			Name:        "API 令牌",
			Icon:        "Ticket",
			Description: "基于 Bearer/Basic Token 的 API 访问令牌",
			UseCases:    []string{"GitHub/GitLab API", "第三方服务集成", "CLI 工具认证"},
		},
		TypeOAuth2: {
			Name:        "OAuth2",
			Icon:        "Connection",
			Description: "基于 OAuth2 协议的授权认证",
			UseCases:    []string{"第三方登录", "SSO 集成", "微服务认证"},
		},
		TypeCert: {
			Name:        "证书",
			Icon:        "Document",
			Description: "基于 X.509 证书的双向 TLS 认证",
			UseCases:    []string{"mTLS 通信", "VPN 接入", "企业 PKI"},
		},
		TypePasskey: {
			Name:        "Passkey",
			Icon:        "Shield",
			Description: "基于 FIDO2/WebAuthn 的无密码认证",
			UseCases:    []string{"无密码登录", "高安全场景", "消费者应用"},
		},
		TypeMFA: {
			Name:        "多因素认证",
			Icon:        "Lock",
			Description: "基于 TOTP/SMS/硬件令牌的二次验证",
			UseCases:    []string{"账户二次验证", "敏感操作确认", "合规要求"},
		},
		TypeIAM: {
			Name:        "IAM 角色",
			Icon:        "User",
			Description: "基于云平台 IAM 的临时凭证认证",
			UseCases:    []string{"AWS/GCP/Azure", "云原生应用", "CI/CD 流水线"},
		},
	}
	if info, ok := infos[t]; ok {
		return info
	}
	return TypeInfo{
		Name:        string(t),
		Icon:        "Document",
		Description: "未知类型",
		UseCases:    []string{},
	}
}

// TypeInfo - 凭据类型详细信息
type TypeInfo struct {
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	UseCases    []string `json:"use_cases"`
}

func (t CredentialType) GetTypeLabel() string {
	labels := map[CredentialType]string{
		TypePassword: "密码",
		TypeSSHKey:   "SSH 密钥",
		TypeToken:    "API 令牌",
		TypeOAuth2:   "OAuth2",
		TypeCert:     "证书",
		TypePasskey:  "Passkey",
		TypeMFA:      "MFA",
		TypeIAM:      "IAM 角色",
	}
	if label, ok := labels[t]; ok {
		return label
	}
	return string(t)
}

func (c CredentialCategory) GetCategoryInfo() CategoryInfo {
	infos := map[CredentialCategory]CategoryInfo{
		CategoryGitHub: {
			Name:             "GitHub",
			Icon:             "Brand",
			Description:      "GitHub 代码托管平台",
			RecommendedTypes: []CredentialType{TypeToken, TypeOAuth2},
		},
		CategoryGitLab: {
			Name:             "GitLab",
			Icon:             "Brand",
			Description:      "GitLab 代码托管平台",
			RecommendedTypes: []CredentialType{TypeToken, TypePassword},
		},
		CategoryGitee: {
			Name:             "Gitee",
			Icon:             "Brand",
			Description:      "Gitee 代码托管平台",
			RecommendedTypes: []CredentialType{TypeToken, TypePassword},
		},
		CategoryDocker: {
			Name:             "Docker",
			Icon:             "Box",
			Description:      "Docker 镜像仓库",
			RecommendedTypes: []CredentialType{TypePassword, TypeCert},
		},
		CategoryKubernetes: {
			Name:             "Kubernetes",
			Icon:             "Grid",
			Description:      "Kubernetes 集群",
			RecommendedTypes: []CredentialType{TypeCert, TypeToken, TypeIAM},
		},
		CategoryDingTalk: {
			Name:             "钉钉",
			Icon:             "Chat",
			Description:      "钉钉企业协作平台",
			RecommendedTypes: []CredentialType{TypeOAuth2, TypeToken},
		},
		CategoryWeChat: {
			Name:             "企业微信",
			Icon:             "Chat",
			Description:      "企业微信协作平台",
			RecommendedTypes: []CredentialType{TypeOAuth2, TypeToken},
		},
		CategoryEmail: {
			Name:             "邮件",
			Icon:             "Message",
			Description:      "邮件服务",
			RecommendedTypes: []CredentialType{TypePassword, TypeCert},
		},
		CategoryAWS: {
			Name:             "AWS",
			Icon:             "Cloud",
			Description:      "Amazon Web Services",
			RecommendedTypes: []CredentialType{TypeIAM, TypeCert, TypeToken},
		},
		CategoryGCP: {
			Name:             "Google Cloud",
			Icon:             "Cloud",
			Description:      "Google Cloud Platform",
			RecommendedTypes: []CredentialType{TypeIAM, TypeOAuth2},
		},
		CategoryAzure: {
			Name:             "Azure",
			Icon:             "Cloud",
			Description:      "Microsoft Azure",
			RecommendedTypes: []CredentialType{TypeIAM, TypeCert},
		},
		CategoryCustom: {
			Name:             "自定义",
			Icon:             "Setting",
			Description:      "自定义服务",
			RecommendedTypes: []CredentialType{TypePassword, TypeToken, TypeCert},
		},
	}
	if info, ok := infos[c]; ok {
		return info
	}
	return CategoryInfo{
		Name:             string(c),
		Icon:             "Document",
		Description:      "未知分类",
		RecommendedTypes: []CredentialType{},
	}
}

// CategoryInfo - 分类详细信息
type CategoryInfo struct {
	Name             string           `json:"name"`
	Icon             string           `json:"icon"`
	Description      string           `json:"description"`
	RecommendedTypes []CredentialType `json:"recommended_types"`
}

func (c CredentialCategory) GetCategoryLabel() string {
	labels := map[CredentialCategory]string{
		CategoryGitHub:     "GitHub",
		CategoryGitLab:     "GitLab",
		CategoryGitee:      "Gitee",
		CategoryDocker:     "Docker",
		CategoryKubernetes: "Kubernetes",
		CategoryDingTalk:   "钉钉",
		CategoryWeChat:     "企业微信",
		CategoryEmail:      "邮件",
		CategoryAWS:        "AWS",
		CategoryGCP:        "Google Cloud",
		CategoryAzure:      "Azure",
		CategoryCustom:     "自定义",
	}
	if label, ok := labels[c]; ok {
		return label
	}
	return string(c)
}

func (c CredentialCategory) GetLabel() string {
	return c.GetCategoryLabel()
}

func (s CredentialStatus) GetStatusInfo() StatusInfo {
	infos := map[CredentialStatus]StatusInfo{
		CredentialStatusActive: {
			Label: "活跃",
			Type:  "success",
			Desc:  "凭据处于正常可用状态",
		},
		CredentialStatusInactive: {
			Label: "已禁用",
			Type:  "info",
			Desc:  "凭据已被管理员禁用",
		},
		CredentialStatusExpired: {
			Label: "已过期",
			Type:  "warning",
			Desc:  "凭据已超过有效期，需要更新",
		},
		CredentialStatusRevoked: {
			Label: "已撤销",
			Type:  "danger",
			Desc:  "凭据已被吊销，不可使用",
		},
	}
	if info, ok := infos[s]; ok {
		return info
	}
	return StatusInfo{
		Label: string(s),
		Type:  "info",
		Desc:  "未知状态",
	}
}

// StatusInfo - 状态详细信息
type StatusInfo struct {
	Label string `json:"label"`
	Type  string `json:"type"`
	Desc  string `json:"desc"`
}

func (s CredentialStatus) GetStatusLabel() string {
	labels := map[CredentialStatus]string{
		CredentialStatusActive:   "活跃",
		CredentialStatusInactive: "已禁用",
		CredentialStatusExpired:  "已过期",
		CredentialStatusRevoked:  "已撤销",
	}
	if label, ok := labels[s]; ok {
		return label
	}
	return string(s)
}

func (s CredentialStatus) GetLabel() string {
	return s.GetStatusLabel()
}

// =============================================================================
// Payload 类型断言和验证方法
// =============================================================================

// ParsePayload - 根据凭据类型解析 SecretData 为对应的 Payload 结构
// 返回解析后的 Payload 和可能的错误
func (c *Credential) ParsePayload() (interface{}, error) {
	if c.EncryptedData == "" {
		return nil, nil
	}

	// 这里需要配合加密服务解密后解析
	// 实际实现需要在 services 层调用 DecryptCredentialData 后使用
	return nil, nil
}

// GetPayloadType - 根据凭据类型返回对应的 Payload 结构体类型
func (t CredentialType) GetPayloadType() string {
	types := map[CredentialType]string{
		TypePassword: "PasswordPayload",
		TypeSSHKey:   "SSHKeyPayload",
		TypeToken:    "TokenPayload",
		TypeOAuth2:   "OAuth2Payload",
		TypeCert:     "CertificatePayload",
		TypePasskey:  "PasskeyPayload",
		TypeMFA:      "MFAPayload",
		TypeIAM:      "IAMRolePayload",
	}
	if pt, ok := types[t]; ok {
		return pt
	}
	return "Unknown"
}

// IsExpiringSoon - 检查凭据是否即将过期
// warningDays: 提前多少天提醒
func (c *Credential) IsExpiringSoon(warningDays int) bool {
	if c.ExpiresAt == nil || *c.ExpiresAt <= 0 {
		return false
	}
	now := time.Now().Unix()
	threshold := now + int64(warningDays*24*60*60)
	return *c.ExpiresAt <= threshold && *c.ExpiresAt > now
}

// IsExpired - 检查凭据是否已过期
func (c *Credential) IsExpired() bool {
	if c.ExpiresAt == nil || *c.ExpiresAt <= 0 {
		return false
	}
	return time.Now().Unix() > *c.ExpiresAt
}

// =============================================================================
// Pipeline Credential - 流水线凭据引用
// =============================================================================

// PipelineCredentialRef - 流水线中引用凭据的配置结构
type PipelineCredentialRef struct {
	ID             uint64    `json:"id" gorm:"primaryKey"`
	PipelineID     uint64    `json:"pipeline_id" gorm:"index;not null"`
	NodeID         string    `json:"node_id" gorm:"size:64;index"` // 流水线节点 ID
	TaskType       string    `json:"task_type" gorm:"size:64;index"`
	CredentialSlot string    `json:"credential_slot" gorm:"size:64;index"`
	CredentialID   uint64    `json:"credential_id" gorm:"index;not null"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	Credential *Credential `json:"credential,omitempty" gorm:"foreignKey:CredentialID"`
}

func (PipelineCredentialRef) TableName() string {
	return "pipeline_credential_refs"
}

// =============================================================================
// 凭据使用统计和审计
// =============================================================================

// CredentialUsageStat - 凭据使用统计
type CredentialUsageStat struct {
	CredentialID uint64 `json:"credential_id" gorm:"primaryKey"`
	UsedCount    int64  `json:"used_count"`
	LastUsedAt   int64  `json:"last_used_at"`
	SuccessCount int64  `json:"success_count"`
	FailedCount  int64  `json:"failed_count"`
}

// CredentialRotationLog - 凭据轮换日志
type CredentialRotationLog struct {
	BaseModel
	CredentialID uint64 `json:"credential_id" gorm:"index;not null"`
	RotatedBy    uint64 `json:"rotated_by" gorm:"index"`
	OldVersion   int    `json:"old_version"`
	NewVersion   int    `json:"new_version"`
	Reason       string `json:"reason" gorm:"size:512"`
	Metadata     string `json:"metadata" gorm:"type:text"`
}

func (CredentialRotationLog) TableName() string {
	return "credential_rotation_logs"
}
