package services

import (
	"errors"
	"strconv"
	"strings"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

type ExternalSecretService interface {
	GetSecret(name string) (string, error)
	GetSecretByID(id uint64) (string, error)
}

type VaultIntegration struct {
	addr  string
	token string
}

func NewVaultIntegration(addr, token string) *VaultIntegration {
	return &VaultIntegration{addr: addr, token: token}
}

func (v *VaultIntegration) GetSecret(path string) (string, error) {
	return "", errors.New("vault integration not implemented")
}

type AWSIntegration struct {
	region string
}

func NewAWSIntegration(region string) *AWSIntegration {
	return &AWSIntegration{region: region}
}

func (a *AWSIntegration) GetSecret(secretID string) (string, error) {
	return "", errors.New("aws integration not implemented")
}

type SecretResolver struct {
	db              *gorm.DB
	vault           *VaultIntegration
	aws             *AWSIntegration
	defaultProvider string
}

func NewSecretResolver(db *gorm.DB, config map[string]string) *SecretResolver {
	return &SecretResolver{
		db:              db,
		vault:           NewVaultIntegration(config["vault_addr"], config["vault_token"]),
		aws:             NewAWSIntegration(config["aws_region"]),
		defaultProvider: config["default_provider"],
	}
}

func (r *SecretResolver) GetSecretValue(secretRef string) (string, error) {
	parts := strings.SplitN(secretRef, "://", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid secret reference format")
	}

	provider := parts[0]
	secretID := parts[1]

	switch provider {
	case "internal":
		return r.getInternalSecret(secretID)
	case "vault":
		return r.vault.GetSecret(secretID)
	case "aws":
		return r.aws.GetSecret(secretID)
	default:
		return "", errors.New("unknown secret provider: " + provider)
	}
}

func (r *SecretResolver) getInternalSecret(secretID string) (string, error) {
	id, err := strconv.ParseUint(secretID, 10, 64)
	if err != nil {
		return "", err
	}

	var secret models.Secret
	if err := r.db.First(&secret, id).Error; err != nil {
		return "", err
	}

	if secret.Status != models.SecretStatusActive {
		return "", errors.New("secret is not active")
	}

	decrypted, err := models.DecryptSecret(secret.EncryptedValue)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}
