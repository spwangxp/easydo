package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"gorm.io/gorm"
)

type AuditStatus string

const (
	AuditStatusSuccess            AuditStatus = "success"
	AuditStatusFailure            AuditStatus = "failure"
	AuditStatusPanic              AuditStatus = "panic"
	AuditStatusTimeout            AuditStatus = "timeout"
	AuditStatusRateLimited        AuditStatus = "rate_limited"
	AuditStatusConcurrencyLimited AuditStatus = "concurrency_limited"
)

type AuditRecordInput struct {
	RequestID     string
	UserID        uint64
	WorkspaceID   uint64
	ToolName      string
	OperationType OperationType
	TargetType    string
	TargetID      string
	ClientName    string
	Protocol      string
	InputSummary  any
}

func (i AuditRecordInput) WithRequestID(requestID string) AuditRecordInput {
	i.RequestID = requestID
	return i
}

type AuditOutcomeInput struct {
	AuditRecordInput
	Status        AuditStatus
	OutputSummary any
	Err           error
	StartedAt     time.Time
	FinishedAt    time.Time
}

type AuditResult struct {
	OutputSummary any
}

type AuditRecorder interface {
	RecordMCPCall(context.Context, models.MCPCallAudit) error
}

type GormAuditRecorder struct {
	db *gorm.DB
}

func NewGormAuditRecorder(db *gorm.DB) *GormAuditRecorder {
	return &GormAuditRecorder{db: db}
}

func (r *GormAuditRecorder) RecordMCPCall(ctx context.Context, record models.MCPCallAudit) error {
	if r == nil || r.db == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "audit db is required"}
	}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		return services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to persist audit record"}
	}
	return nil
}

func WrapAudit(ctx context.Context, recorder AuditRecorder, input AuditRecordInput, fn func(context.Context) (AuditResult, error)) (result AuditResult, err error) {
	startedAt := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = services.ServiceError{Code: services.ErrorCodeInternalError, Message: "internal error"}
			_ = RecordAuditOutcome(ctx, recorder, AuditOutcomeInput{
				AuditRecordInput: input,
				Status:           AuditStatusPanic,
				Err:              fmt.Errorf("panic: %v", recovered),
				OutputSummary:    result.OutputSummary,
				StartedAt:        startedAt,
				FinishedAt:       time.Now(),
			})
		}
	}()

	result, err = fn(ctx)
	status := classifyAuditStatus(ctx, err)
	if recordErr := RecordAuditOutcome(ctx, recorder, AuditOutcomeInput{
		AuditRecordInput: input,
		Status:           status,
		OutputSummary:    result.OutputSummary,
		Err:              err,
		StartedAt:        startedAt,
		FinishedAt:       time.Now(),
	}); recordErr != nil && err == nil {
		return result, recordErr
	}
	return result, err
}

func RecordAuditOutcome(ctx context.Context, recorder AuditRecorder, input AuditOutcomeInput) error {
	if recorder == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "audit recorder is required"}
	}
	finishedAt := input.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() || finishedAt.Before(startedAt) {
		startedAt = finishedAt
	}
	record := models.MCPCallAudit{
		RequestID:     input.RequestID,
		UserID:        input.UserID,
		WorkspaceID:   input.WorkspaceID,
		ToolName:      input.ToolName,
		OperationType: string(input.OperationType),
		TargetType:    input.TargetType,
		TargetID:      input.TargetID,
		ClientName:    input.ClientName,
		Protocol:      input.Protocol,
		Status:        string(input.Status),
		DurationMS:    finishedAt.Sub(startedAt).Milliseconds(),
		InputSummary:  marshalAuditSummary(input.InputSummary),
		OutputSummary: marshalAuditSummary(input.OutputSummary),
	}
	if record.DurationMS < 0 {
		record.DurationMS = 0
	}
	code, message := auditErrorDetails(input.Status, input.Err)
	if code != "" {
		record.ErrorCode = code
	}
	if message != "" {
		record.ErrorMessage = message
	}
	return recorder.RecordMCPCall(ctx, record)
}

func classifyAuditStatus(ctx context.Context, err error) AuditStatus {
	if err == nil {
		return AuditStatusSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return AuditStatusTimeout
	}
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case services.ErrorCodeRateLimited:
			return AuditStatusRateLimited
		case services.ErrorCodeConcurrencyLimited:
			return AuditStatusConcurrencyLimited
		}
	}
	var svcErrPtr *services.ServiceError
	if errors.As(err, &svcErrPtr) && svcErrPtr != nil {
		switch svcErrPtr.Code {
		case services.ErrorCodeRateLimited:
			return AuditStatusRateLimited
		case services.ErrorCodeConcurrencyLimited:
			return AuditStatusConcurrencyLimited
		}
	}
	return AuditStatusFailure
}

func auditErrorDetails(status AuditStatus, err error) (string, string) {
	if err == nil {
		return "", ""
	}
	if status == AuditStatusTimeout || errors.Is(err, context.DeadlineExceeded) {
		return string(services.ErrorCodeTimeout), sanitizeAuditText("request timed out")
	}
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		return string(svcErr.Code), sanitizeAuditText(svcErr.Message)
	}
	var svcErrPtr *services.ServiceError
	if errors.As(err, &svcErrPtr) && svcErrPtr != nil {
		return string(svcErrPtr.Code), sanitizeAuditText(svcErrPtr.Message)
	}
	return string(services.ErrorCodeInternalError), "internal error"
}

func marshalAuditSummary(value any) string {
	if value == nil {
		return ""
	}
	sanitized := scrubBearerValues(SanitizeSummary(value))
	payload, err := json.Marshal(sanitized)
	if err != nil {
		return sanitizeAuditText(fmt.Sprintf("%v", sanitized))
	}
	return sanitizeAuditText(string(payload))
}

func scrubBearerValues(value any) any {
	switch v := value.(type) {
	case string:
		return sanitizeAuditText(v)
	case []any:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, scrubBearerValues(item))
		}
		return items
	case map[string]any:
		items := make(map[string]any, len(v))
		for key, item := range v {
			items[key] = scrubBearerValues(item)
		}
		return items
	default:
		return v
	}
}

func sanitizeAuditText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, sensitive := range []string{"bearer ", "authorization", "token", "secret", "password", "credential", "api_key"} {
		if strings.Contains(lower, sensitive) {
			return "[REDACTED]"
		}
	}
	return trimmed
}
