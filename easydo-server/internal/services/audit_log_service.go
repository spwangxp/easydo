package services

import (
	"context"
	"encoding/json"
	"strings"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

type AuditInput struct {
	WorkspaceID      *uint64
	ActorUserID      uint64
	ActorRole        string
	ActorWorkspaceID *uint64
	Action           string
	TargetType       string
	TargetID         uint64
	Before           any
	After            any
	Metadata         any
	IP               string
	UserAgent        string
}

type AuditLogService struct {
	DB *gorm.DB
}

type ListAuditLogsRequest struct {
	Actor             ActorContext
	WorkspaceID       uint64
	TargetWorkspaceID uint64
	Action            string
	TargetType        string
	TargetID          uint64
	Page              int
	Limit             int
}

type AuditLogListResult struct {
	List  []models.AuditLog `json:"list"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

func AppendAuditLog(ctx context.Context, db *gorm.DB, input AuditInput) error {
	if db == nil {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if input.ActorUserID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	action := strings.TrimSpace(input.Action)
	if action == "" {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "audit action is required"}
	}
	targetType := strings.TrimSpace(input.TargetType)
	if targetType == "" {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "audit target type is required"}
	}
	if input.TargetID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "audit target id is required"}
	}

	beforeJSON, err := marshalAuditPayload(input.Before)
	if err != nil {
		return err
	}
	afterJSON, err := marshalAuditPayload(input.After)
	if err != nil {
		return err
	}
	metadataJSON, err := marshalAuditPayload(input.Metadata)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	record := models.AuditLog{
		WorkspaceID:      input.WorkspaceID,
		ActorUserID:      input.ActorUserID,
		ActorRole:        strings.TrimSpace(input.ActorRole),
		ActorWorkspaceID: input.ActorWorkspaceID,
		Action:           action,
		TargetType:       targetType,
		TargetID:         input.TargetID,
		BeforeJSON:       beforeJSON,
		AfterJSON:        afterJSON,
		MetadataJSON:     metadataJSON,
		IP:               strings.TrimSpace(input.IP),
		UserAgent:        strings.TrimSpace(input.UserAgent),
	}
	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		return ServiceError{Code: ErrorCodeInternalError, Message: "failed to append audit log"}
	}
	return nil
}

func (s *AuditLogService) ListAuditLogs(ctx context.Context, req ListAuditLogsRequest) (AuditLogListResult, error) {
	if s == nil || s.DB == nil {
		return AuditLogListResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db := s.DB
	query := db.WithContext(ctx).Model(&models.AuditLog{})
	if req.TargetWorkspaceID > 0 {
		targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.TargetWorkspaceID)
		if err != nil {
			return AuditLogListResult{}, err
		}
		query = query.Where("workspace_id = ?", targetWorkspaceID)
	} else if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return AuditLogListResult{}, err
	}
	if action := strings.TrimSpace(req.Action); action != "" {
		query = query.Where("action = ?", action)
	}
	if targetType := strings.TrimSpace(req.TargetType); targetType != "" {
		query = query.Where("target_type = ?", targetType)
	}
	if req.TargetID > 0 {
		query = query.Where("target_id = ?", req.TargetID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return AuditLogListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count audit logs"}
	}
	page, limit := normalizePagination(req.Page, req.Limit)
	var list []models.AuditLog
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return AuditLogListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list audit logs"}
	}
	return AuditLogListResult{List: list, Total: total, Page: page, Limit: limit}, nil
}

func marshalAuditPayload(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", ServiceError{Code: ErrorCodeInvalidArgument, Message: "audit payload must be JSON serializable"}
	}
	return string(payload), nil
}
