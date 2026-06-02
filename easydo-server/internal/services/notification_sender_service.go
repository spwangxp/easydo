package services

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/smtpclient"

	"gorm.io/gorm"
)

type NotificationSenderService struct {
	DB *gorm.DB
}

type SaveNotificationSenderConfigRequest struct {
	Actor             ActorContext
	WorkspaceID       uint64
	Scope             string
	TargetWorkspaceID uint64
	Enabled           bool
	FromName          string
	FromAddress       string
	SMTPHost          string
	SMTPPort          int
	SMTPUsername      string
	SMTPPassword      string
	SMTPTLSMode       string
}

type GetEffectiveNotificationSenderConfigRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
}

type TestNotificationSenderConfigRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	ToAddress   string
}

type NotificationSenderConfigSummary struct {
	ID                     uint64 `json:"id"`
	Scope                  string `json:"scope"`
	WorkspaceID            uint64 `json:"workspace_id"`
	Enabled                bool   `json:"enabled"`
	FromName               string `json:"from_name"`
	FromAddress            string `json:"from_address"`
	SMTPHost               string `json:"smtp_host"`
	SMTPPort               int    `json:"smtp_port"`
	SMTPUsername           string `json:"smtp_username"`
	SMTPPassword           string `json:"-"`
	SMTPPasswordConfigured bool   `json:"smtp_password_configured"`
	SMTPTLSMode            string `json:"smtp_tls_mode"`
	LastTestedAt           int64  `json:"last_tested_at"`
	LastTestStatus         string `json:"last_test_status"`
	LastTestError          string `json:"last_test_error"`
}

type NotificationSenderTestResult struct {
	Status   string `json:"status"`
	Error    string `json:"error"`
	TestedAt int64  `json:"tested_at"`
}

var notificationSenderSendMail = smtpclient.Send

func (s *NotificationSenderService) SaveConfig(ctx context.Context, req SaveNotificationSenderConfigRequest) (NotificationSenderConfigSummary, error) {
	db, err := notificationSenderDB(s)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scope, workspaceID, err := resolveNotificationSenderScope(ctx, db, req)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	normalized := normalizeNotificationSenderInput(req)
	if err := validateNotificationSenderConfig(normalized); err != nil {
		return NotificationSenderConfigSummary{}, err
	}

	var saved models.NotificationSenderConfig
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.NotificationSenderConfig
		err := tx.Where("scope = ? AND workspace_id = ?", scope, workspaceID).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to load notification sender config"}
		}
		encryptedPassword := existing.SMTPPasswordEncrypted
		if strings.TrimSpace(normalized.SMTPPassword) != "" {
			var encryptErr error
			encryptedPassword, encryptErr = models.EncryptCredentialPayload(normalized.SMTPPassword)
			if encryptErr != nil {
				return ServiceError{Code: ErrorCodeInternalError, Message: "failed to encrypt smtp password"}
			}
		}
		saved = models.NotificationSenderConfig{
			BaseModel:             existing.BaseModel,
			Scope:                 scope,
			WorkspaceID:           workspaceID,
			Enabled:               normalized.Enabled,
			FromName:              normalized.FromName,
			FromAddress:           normalized.FromAddress,
			SMTPHost:              normalized.SMTPHost,
			SMTPPort:              normalized.SMTPPort,
			SMTPUsername:          normalized.SMTPUsername,
			SMTPPasswordEncrypted: encryptedPassword,
			SMTPTLSMode:           normalized.SMTPTLSMode,
			UpdatedBy:             req.Actor.UserID,
			LastTestedAt:          existing.LastTestedAt,
			LastTestStatus:        existing.LastTestStatus,
			LastTestError:         existing.LastTestError,
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := tx.Create(&saved).Error; createErr != nil {
				return ServiceError{Code: ErrorCodeInternalError, Message: "failed to create notification sender config"}
			}
		} else if saveErr := tx.Save(&saved).Error; saveErr != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to save notification sender config"}
		}
		return AppendAuditLog(ctx, tx, AuditInput{
			WorkspaceID:      &workspaceID,
			ActorUserID:      req.Actor.UserID,
			ActorRole:        strings.TrimSpace(req.Actor.SystemRole),
			ActorWorkspaceID: &req.WorkspaceID,
			Action:           "notification_sender.save",
			TargetType:       "notification_sender_config",
			TargetID:         saved.ID,
			After:            buildNotificationSenderSummary(saved, false),
		})
	})
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	return buildNotificationSenderSummary(saved, false), nil
}

func (s *NotificationSenderService) GetEffectiveConfig(ctx context.Context, workspaceID uint64) (NotificationSenderConfigSummary, error) {
	db, err := notificationSenderDB(s)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if workspaceID > 0 {
		config, ok, err := loadEnabledNotificationSenderConfig(ctx, db, models.NotificationSenderScopeWorkspace, workspaceID)
		if err != nil {
			return NotificationSenderConfigSummary{}, err
		}
		if ok {
			return buildNotificationSenderSummaryWithPassword(config)
		}
	}
	config, ok, err := loadEnabledNotificationSenderConfig(ctx, db, models.NotificationSenderScopePlatform, 0)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	if !ok {
		return NotificationSenderConfigSummary{Enabled: false}, nil
	}
	return buildNotificationSenderSummaryWithPassword(config)
}

func (s *NotificationSenderService) GetEffectiveConfigForActor(ctx context.Context, req GetEffectiveNotificationSenderConfigRequest) (NotificationSenderConfigSummary, error) {
	db, err := notificationSenderDB(s)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req.WorkspaceID == 0 {
		return NotificationSenderConfigSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace id is required"}
	}

	workspace, err := loadUserManagementWorkspace(ctx, db, req.WorkspaceID)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	if normalizeUserManagementWorkspaceKind(workspace.Kind) == models.WorkspaceKindAdmin {
		if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
			return NotificationSenderConfigSummary{}, err
		}
		return s.GetEffectiveConfig(ctx, 0)
	}

	targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.WorkspaceID)
	if err != nil {
		return NotificationSenderConfigSummary{}, err
	}
	return s.GetEffectiveConfig(ctx, targetWorkspaceID)
}

func (s *NotificationSenderService) TestEffectiveConfig(ctx context.Context, req TestNotificationSenderConfigRequest) (NotificationSenderTestResult, error) {
	db, err := notificationSenderDB(s)
	if err != nil {
		return NotificationSenderTestResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	recipient, err := normalizeNotificationSenderTestRecipient(req.ToAddress)
	if err != nil {
		return NotificationSenderTestResult{}, err
	}
	effective, err := s.GetEffectiveConfigForActor(ctx, GetEffectiveNotificationSenderConfigRequest{
		Actor:       req.Actor,
		WorkspaceID: req.WorkspaceID,
	})
	if err != nil {
		return NotificationSenderTestResult{}, err
	}
	if !notificationSenderConfigReady(effective) {
		return NotificationSenderTestResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "smtp sender is not configured"}
	}

	message := buildNotificationSenderTestMessage(effective.FromAddress, effective.FromName, recipient)

	result := NotificationSenderTestResult{Status: "success", TestedAt: time.Now().Unix()}
	if sendErr := notificationSenderSendMail(smtpclient.SendRequest{
		Host:     effective.SMTPHost,
		Port:     effective.SMTPPort,
		TLSMode:  effective.SMTPTLSMode,
		Username: effective.SMTPUsername,
		Password: effective.SMTPPassword,
		From:     effective.FromAddress,
		To:       []string{recipient},
		Message:  []byte(message),
	}); sendErr != nil {
		result.Status = "failed"
		result.Error = sendErr.Error()
	}
	if effective.ID > 0 {
		if err := db.WithContext(ctx).Model(&models.NotificationSenderConfig{}).Where("id = ?", effective.ID).Updates(map[string]interface{}{
			"last_tested_at":   result.TestedAt,
			"last_test_status": result.Status,
			"last_test_error":  result.Error,
		}).Error; err != nil {
			return NotificationSenderTestResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to update smtp test result"}
		}
	}
	return result, nil
}

func notificationSenderDB(s *NotificationSenderService) (*gorm.DB, error) {
	if s == nil || s.DB == nil {
		return nil, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	return s.DB, nil
}

func resolveNotificationSenderScope(ctx context.Context, db *gorm.DB, req SaveNotificationSenderConfigRequest) (string, uint64, error) {
	switch strings.ToLower(strings.TrimSpace(req.Scope)) {
	case models.NotificationSenderScopePlatform:
		if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
			return "", 0, err
		}
		return models.NotificationSenderScopePlatform, 0, nil
	case models.NotificationSenderScopeWorkspace:
		targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.TargetWorkspaceID)
		if err != nil {
			return "", 0, err
		}
		return models.NotificationSenderScopeWorkspace, targetWorkspaceID, nil
	default:
		return "", 0, ServiceError{Code: ErrorCodeInvalidArgument, Message: "invalid notification sender scope"}
	}
}

func normalizeNotificationSenderInput(req SaveNotificationSenderConfigRequest) SaveNotificationSenderConfigRequest {
	req.FromName = strings.TrimSpace(req.FromName)
	req.FromAddress = strings.ToLower(strings.TrimSpace(req.FromAddress))
	req.SMTPHost = strings.TrimSpace(req.SMTPHost)
	req.SMTPUsername = strings.TrimSpace(req.SMTPUsername)
	req.SMTPPassword = strings.TrimSpace(req.SMTPPassword)
	req.SMTPTLSMode = strings.ToLower(strings.TrimSpace(req.SMTPTLSMode))
	if req.SMTPTLSMode == "" {
		req.SMTPTLSMode = "plain"
	}
	return req
}

func validateNotificationSenderConfig(req SaveNotificationSenderConfigRequest) error {
	if !req.Enabled {
		return nil
	}
	if req.FromAddress == "" {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "from address is required"}
	}
	if req.SMTPHost == "" {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "smtp host is required"}
	}
	if req.SMTPPort <= 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "smtp port is required"}
	}
	return nil
}

func loadEnabledNotificationSenderConfig(ctx context.Context, db *gorm.DB, scope string, workspaceID uint64) (models.NotificationSenderConfig, bool, error) {
	var config models.NotificationSenderConfig
	err := db.WithContext(ctx).
		Where("scope = ? AND workspace_id = ? AND enabled = ?", scope, workspaceID, true).
		First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.NotificationSenderConfig{}, false, nil
		}
		return models.NotificationSenderConfig{}, false, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load notification sender config"}
	}
	return config, true, nil
}

func buildNotificationSenderSummaryWithPassword(config models.NotificationSenderConfig) (NotificationSenderConfigSummary, error) {
	summary := buildNotificationSenderSummary(config, false)
	if strings.TrimSpace(config.SMTPPasswordEncrypted) != "" {
		plaintext, err := models.DecryptCredentialPayload(config.SMTPPasswordEncrypted)
		if err != nil {
			return NotificationSenderConfigSummary{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to decrypt smtp password"}
		}
		summary.SMTPPassword = string(plaintext)
		summary.SMTPPasswordConfigured = true
	}
	return summary, nil
}

func buildNotificationSenderSummary(config models.NotificationSenderConfig, includePassword bool) NotificationSenderConfigSummary {
	summary := NotificationSenderConfigSummary{
		ID:                     config.ID,
		Scope:                  config.Scope,
		WorkspaceID:            config.WorkspaceID,
		Enabled:                config.Enabled,
		FromName:               config.FromName,
		FromAddress:            config.FromAddress,
		SMTPHost:               config.SMTPHost,
		SMTPPort:               config.SMTPPort,
		SMTPUsername:           config.SMTPUsername,
		SMTPPasswordConfigured: strings.TrimSpace(config.SMTPPasswordEncrypted) != "",
		SMTPTLSMode:            config.SMTPTLSMode,
		LastTestedAt:           config.LastTestedAt,
		LastTestStatus:         config.LastTestStatus,
		LastTestError:          config.LastTestError,
	}
	if includePassword {
		summary.SMTPPassword = config.SMTPPasswordEncrypted
	}
	return summary
}

func notificationSenderConfigReady(config NotificationSenderConfigSummary) bool {
	return config.Enabled &&
		strings.TrimSpace(config.FromAddress) != "" &&
		strings.TrimSpace(config.SMTPHost) != "" &&
		config.SMTPPort > 0
}

func normalizeNotificationSenderTestRecipient(raw string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(parsed.Address) == "" {
		return "", ServiceError{Code: ErrorCodeInvalidArgument, Message: "test recipient email is required"}
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
}

func buildNotificationSenderTestMessage(fromAddress, fromName, toAddress string) string {
	fromHeader := strings.TrimSpace(fromAddress)
	if strings.TrimSpace(fromName) != "" {
		fromHeader = fmt.Sprintf("%s <%s>", strings.TrimSpace(fromName), fromHeader)
	}
	subject := "EasyDo 邮件发送配置测试"
	return strings.Join([]string{
		fmt.Sprintf("From: %s", fromHeader),
		fmt.Sprintf("To: %s", toAddress),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"这是一封 EasyDo 邮件发送配置测试邮件。",
		"",
	}, "\r\n")
}
