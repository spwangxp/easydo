package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

const (
	BuiltinMCPTokenName   = "easydo"
	MCPTokenPlainPrefix   = "edmcp_"
	mcpTokenRandomBytes   = 32
	mcpTokenHashHexLength = 64
)

type MCPTokenService struct {
	DB *gorm.DB
}

type MCPConfig struct {
	Name        string `json:"name"`
	Token       string `json:"token"`
	WorkspaceID uint64 `json:"workspace_id"`
	UserID      uint64 `json:"user_id"`
}

func NewMCPTokenService(db *gorm.DB) *MCPTokenService {
	return &MCPTokenService{DB: db}
}

func (s *MCPTokenService) GetOrCreateBuiltinToken(ctx context.Context, actor ActorContext, workspaceID uint64) (MCPConfig, error) {
	if s == nil || s.DB == nil {
		return MCPConfig{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if actor.UserID == 0 {
		return MCPConfig{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	if workspaceID == 0 {
		return MCPConfig{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace_id is required"}
	}
	if _, err := ResolveWorkspaceForActor(ctx, s.DB, actor, workspaceID); err != nil {
		return MCPConfig{}, err
	}

	record, token, err := s.loadActiveToken(ctx, workspaceID, actor.UserID, BuiltinMCPTokenName)
	if err == nil {
		return MCPConfig{Name: record.Name, Token: token, WorkspaceID: workspaceID, UserID: actor.UserID}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return MCPConfig{}, err
	}

	plainToken, err := GenerateMCPTokenPlaintext()
	if err != nil {
		return MCPConfig{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to generate mcp token"}
	}
	encrypted, err := models.EncryptCredentialPayload(plainToken)
	if err != nil {
		return MCPConfig{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to encrypt mcp token"}
	}
	now := time.Now()
	record = models.MCPToken{
		WorkspaceID:    workspaceID,
		UserID:         actor.UserID,
		Name:           BuiltinMCPTokenName,
		TokenHash:      HashMCPToken(plainToken),
		TokenEncrypted: encrypted,
		Status:         models.MCPTokenStatusActive,
		BaseModel:      models.BaseModel{CreatedAt: now, UpdatedAt: now},
	}
	if createErr := s.DB.WithContext(ctx).Create(&record).Error; createErr != nil {
		// Another server replica may have won the unique-key race; load the row it created.
		if existing, existingToken, loadErr := s.loadActiveToken(ctx, workspaceID, actor.UserID, BuiltinMCPTokenName); loadErr == nil {
			return MCPConfig{Name: existing.Name, Token: existingToken, WorkspaceID: workspaceID, UserID: actor.UserID}, nil
		}
		return MCPConfig{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to persist mcp token"}
	}
	return MCPConfig{Name: record.Name, Token: plainToken, WorkspaceID: workspaceID, UserID: actor.UserID}, nil
}

func (s *MCPTokenService) Authenticate(ctx context.Context, token string) (ActorContext, error) {
	if s == nil || s.DB == nil {
		return ActorContext{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	plain := strings.TrimSpace(token)
	if !strings.HasPrefix(plain, MCPTokenPlainPrefix) {
		return ActorContext{}, ServiceError{Code: ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	hash := HashMCPToken(plain)
	if len(hash) != mcpTokenHashHexLength {
		return ActorContext{}, ServiceError{Code: ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	now := time.Now()
	var record models.MCPToken
	err := s.DB.WithContext(ctx).
		Where("token_hash = ? AND status = ?", hash, models.MCPTokenStatusActive).
		Where("expires_at IS NULL OR expires_at > ?", now).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ActorContext{}, ServiceError{Code: ErrorCodeUnauthorized, Message: "unauthorized"}
		}
		return ActorContext{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to authenticate mcp token"}
	}

	var user models.User
	if err := s.DB.WithContext(ctx).Where("id = ? AND status = ?", record.UserID, "active").First(&user).Error; err != nil {
		return ActorContext{}, ServiceError{Code: ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	usedAt := now
	_ = s.DB.WithContext(ctx).Model(&models.MCPToken{}).Where("id = ?", record.ID).Update("last_used_at", usedAt).Error
	return ActorContext{
		UserID:             user.ID,
		Username:           user.Username,
		SystemRole:         user.Role,
		CurrentWorkspaceID: record.WorkspaceID,
		SessionID:          fmt.Sprintf("mcp:%d:%d", record.WorkspaceID, record.UserID),
	}, nil
}

func (s *MCPTokenService) loadActiveToken(ctx context.Context, workspaceID, userID uint64, name string) (models.MCPToken, string, error) {
	var record models.MCPToken
	err := s.DB.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ? AND name = ? AND status = ?", workspaceID, userID, name, models.MCPTokenStatusActive).
		First(&record).Error
	if err != nil {
		return models.MCPToken{}, "", err
	}
	plain, err := models.DecryptCredentialPayload(record.TokenEncrypted)
	if err != nil {
		return models.MCPToken{}, "", ServiceError{Code: ErrorCodeInternalError, Message: "failed to decrypt mcp token"}
	}
	return record, string(plain), nil
}

func GenerateMCPTokenPlaintext() (string, error) {
	random := make([]byte, mcpTokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return MCPTokenPlainPrefix + hex.EncodeToString(random), nil
}

func HashMCPToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
