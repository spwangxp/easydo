package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&models.MCPCallAudit{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func TestAuditRecorderWritesSuccessFailurePanicTimeoutAndRateLimit(t *testing.T) {
	db := openAuditTestDB(t)
	recorder := NewGormAuditRecorder(db)
	base := AuditRecordInput{
		RequestID:     "req-base",
		UserID:        10,
		WorkspaceID:   20,
		ToolName:      "pipelines.list",
		OperationType: OperationRead,
		TargetType:    "pipeline",
		TargetID:      "20",
		ClientName:    "tests",
		Protocol:      "streamable_http",
		InputSummary:  map[string]any{"query": "ok"},
	}

	if _, err := WrapAudit(context.Background(), recorder, base, func(ctx context.Context) (AuditResult, error) {
		return AuditResult{OutputSummary: map[string]any{"items": []any{"a", "b"}}}, nil
	}); err != nil {
		t.Fatalf("success wrap returned error: %v", err)
	}

	failureErr := services.ServiceError{Code: services.ErrorCodeForbidden, Message: "access denied"}
	_, err := WrapAudit(context.Background(), recorder, base.WithRequestID("req-failure"), func(ctx context.Context) (AuditResult, error) {
		return AuditResult{OutputSummary: map[string]any{"workspace_id": 20}}, failureErr
	})
	if err == nil {
		t.Fatal("expected failure error")
	}

	_, err = WrapAudit(context.Background(), recorder, base.WithRequestID("req-timeout"), func(ctx context.Context) (AuditResult, error) {
		return AuditResult{}, context.DeadlineExceeded
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error=%v, want deadline exceeded", err)
	}

	_, err = WrapAudit(context.Background(), recorder, base.WithRequestID("req-panic"), func(ctx context.Context) (AuditResult, error) {
		panic("boom")
	})
	if err == nil {
		t.Fatal("expected panic to return safe internal error")
	}
	var panicErr services.ServiceError
	if !errors.As(err, &panicErr) {
		t.Fatalf("panic error type=%T, want ServiceError", err)
	}
	if panicErr.Code != services.ErrorCodeInternalError {
		t.Fatalf("panic error code=%q, want %q", panicErr.Code, services.ErrorCodeInternalError)
	}

	if err := RecordAuditOutcome(context.Background(), recorder, AuditOutcomeInput{AuditRecordInput: base.WithRequestID("req-rate-limit"), Status: AuditStatusRateLimited, Err: services.ServiceError{Code: services.ErrorCodeRateLimited, Message: "too many requests"}}); err != nil {
		t.Fatalf("record rate limit outcome failed: %v", err)
	}
	if err := RecordAuditOutcome(context.Background(), recorder, AuditOutcomeInput{AuditRecordInput: base.WithRequestID("req-concurrency-limit"), Status: AuditStatusConcurrencyLimited, Err: services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many in flight requests"}}); err != nil {
		t.Fatalf("record concurrency limit outcome failed: %v", err)
	}

	var audits []models.MCPCallAudit
	if err := db.Order("request_id asc").Find(&audits).Error; err != nil {
		t.Fatalf("load audits failed: %v", err)
	}
	if len(audits) != 6 {
		t.Fatalf("audit count=%d, want 6", len(audits))
	}

	statuses := map[string]string{}
	errorCodes := map[string]string{}
	for _, audit := range audits {
		statuses[audit.RequestID] = audit.Status
		errorCodes[audit.RequestID] = audit.ErrorCode
		if strings.Contains(strings.ToLower(audit.InputSummary), "bearer ") || strings.Contains(strings.ToLower(audit.OutputSummary), "bearer ") || strings.Contains(strings.ToLower(audit.ErrorMessage), "bearer ") {
			t.Fatalf("audit should not persist bearer token details: %+v", audit)
		}
		if audit.DurationMS < 0 {
			t.Fatalf("duration_ms=%d, want >= 0", audit.DurationMS)
		}
	}

	if statuses["req-base"] != string(AuditStatusSuccess) {
		t.Fatalf("success status=%q, want %q", statuses["req-base"], AuditStatusSuccess)
	}
	if statuses["req-failure"] != string(AuditStatusFailure) {
		t.Fatalf("failure status=%q, want %q", statuses["req-failure"], AuditStatusFailure)
	}
	if statuses["req-timeout"] != string(AuditStatusTimeout) {
		t.Fatalf("timeout status=%q, want %q", statuses["req-timeout"], AuditStatusTimeout)
	}
	if statuses["req-panic"] != string(AuditStatusPanic) {
		t.Fatalf("panic status=%q, want %q", statuses["req-panic"], AuditStatusPanic)
	}
	if statuses["req-rate-limit"] != string(AuditStatusRateLimited) {
		t.Fatalf("rate limit status=%q, want %q", statuses["req-rate-limit"], AuditStatusRateLimited)
	}
	if statuses["req-concurrency-limit"] != string(AuditStatusConcurrencyLimited) {
		t.Fatalf("concurrency status=%q, want %q", statuses["req-concurrency-limit"], AuditStatusConcurrencyLimited)
	}
	if errorCodes["req-rate-limit"] != string(services.ErrorCodeRateLimited) {
		t.Fatalf("rate limit error_code=%q, want %q", errorCodes["req-rate-limit"], services.ErrorCodeRateLimited)
	}
	if errorCodes["req-concurrency-limit"] != string(services.ErrorCodeConcurrencyLimited) {
		t.Fatalf("concurrency error_code=%q, want %q", errorCodes["req-concurrency-limit"], services.ErrorCodeConcurrencyLimited)
	}
}

func TestAuditUnknownErrorPersistsGenericInternalError(t *testing.T) {
	db := openAuditTestDB(t)
	recorder := NewGormAuditRecorder(db)
	input := AuditRecordInput{
		RequestID:     "req-unknown-secret",
		UserID:        10,
		WorkspaceID:   20,
		ToolName:      "pipelines.list",
		OperationType: OperationRead,
		TargetType:    "pipeline",
		Protocol:      "streamable_http",
	}

	_, err := WrapAudit(context.Background(), recorder, input, func(ctx context.Context) (AuditResult, error) {
		return AuditResult{}, errors.New("database failed password=hunter2 api_key=abc123")
	})
	if err == nil {
		t.Fatal("expected wrapped unknown error")
	}

	var audit models.MCPCallAudit
	if err := db.Where("request_id = ?", "req-unknown-secret").First(&audit).Error; err != nil {
		t.Fatalf("load audit failed: %v", err)
	}
	if audit.ErrorCode != string(services.ErrorCodeInternalError) {
		t.Fatalf("error_code=%q, want %q", audit.ErrorCode, services.ErrorCodeInternalError)
	}
	if audit.ErrorMessage != "internal error" {
		t.Fatalf("error_message=%q, want internal error", audit.ErrorMessage)
	}
	joined := strings.ToLower(audit.InputSummary + audit.OutputSummary + audit.ErrorMessage)
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, "abc123") || strings.Contains(joined, "password=") || strings.Contains(joined, "api_key=") {
		t.Fatalf("audit leaked unknown error secrets: %+v", audit)
	}
}

func TestAuditRecorderNilDBReturnsInvalidArgument(t *testing.T) {
	recorder := NewGormAuditRecorder(nil)
	err := recorder.RecordMCPCall(context.Background(), models.MCPCallAudit{RequestID: "req-nil-db"})
	if err == nil {
		t.Fatal("expected invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeInvalidArgument)
	}
}

func TestAuditRecorderClosedDBReturnsInternalError(t *testing.T) {
	db := openAuditTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}
	recorder := NewGormAuditRecorder(db)
	err = recorder.RecordMCPCall(context.Background(), models.MCPCallAudit{RequestID: "req-closed-db"})
	if err == nil {
		t.Fatal("expected internal error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeInternalError {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeInternalError)
	}
}

func TestAuditSummaryDoesNotContainBearerToken(t *testing.T) {
	db := openAuditTestDB(t)
	recorder := NewGormAuditRecorder(db)
	input := AuditRecordInput{
		RequestID:     "req-secret",
		UserID:        10,
		WorkspaceID:   20,
		ToolName:      "pipelines.list",
		OperationType: OperationRead,
		TargetType:    "pipeline",
		Protocol:      "streamable_http",
		InputSummary: map[string]any{
			"authorization": "Bearer top-secret-token",
			"nested":        map[string]any{"token": "Bearer hidden-secret"},
		},
	}

	if _, err := WrapAudit(context.Background(), recorder, input, func(ctx context.Context) (AuditResult, error) {
		return AuditResult{OutputSummary: map[string]any{"authorization": "Bearer output-secret"}}, nil
	}); err != nil {
		t.Fatalf("WrapAudit returned error: %v", err)
	}

	var audit models.MCPCallAudit
	if err := db.Where("request_id = ?", "req-secret").First(&audit).Error; err != nil {
		t.Fatalf("load audit failed: %v", err)
	}
	joined := strings.ToLower(audit.InputSummary + audit.OutputSummary + audit.ErrorMessage)
	if strings.Contains(joined, "bearer") || strings.Contains(joined, "top-secret-token") || strings.Contains(joined, "output-secret") || strings.Contains(joined, "hidden-secret") {
		t.Fatalf("audit summaries leaked bearer token details: %+v", audit)
	}
}

var _ = time.Second
