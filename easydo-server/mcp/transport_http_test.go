package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

type recordedAudit struct {
	RequestID string
	UserID    uint64
	ToolName  string
	Status    string
	ErrorCode string
}

type fakeAuditRecorder struct {
	mu      sync.Mutex
	records []recordedAudit
}

func (r *fakeAuditRecorder) RecordMCPCall(_ context.Context, record models.MCPCallAudit) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, recordedAudit{
		RequestID: record.RequestID,
		UserID:    record.UserID,
		ToolName:  record.ToolName,
		Status:    record.Status,
		ErrorCode: record.ErrorCode,
	})
	return nil
}

func (r *fakeAuditRecorder) snapshot() []recordedAudit {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedAudit, len(r.records))
	copy(out, r.records)
	return out
}

func newTestHTTPServer(t *testing.T, opts ServerOptions) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewServer(opts).RegisterRoutes(router)
	return router
}

func validMCPBearer(t *testing.T, userID uint64) string {
	t.Helper()
	setupMCPAuthTestRedis(t)
	user := &models.User{BaseModel: models.BaseModel{ID: userID}, Username: "mcp-user", Role: "admin"}
	token, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}
	return "Bearer " + token
}

func postJSONRPC(t *testing.T, router http.Handler, authorization string, payload string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeRPCResponse(t *testing.T, w *httptest.ResponseRecorder) JSONRPCResponse {
	t.Helper()
	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v; body=%s", err, w.Body.String())
	}
	return resp
}

func TestStreamableHTTPInitializeIncludesProtocolVersion(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit})

	w := postJSONRPC(t, router, validMCPBearer(t, 7011), `{"jsonrpc":"2.0","id":"init","method":"initialize"}`, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	resp := decodeRPCResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type=%T, want map[string]any", resp.Result)
	}
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion=%v, want 2025-06-18", result["protocolVersion"])
	}
}

func TestStreamableHTTPRejectsMissingAuth(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit})

	w := postJSONRPC(t, router, "", `{"jsonrpc":"2.0","id":"missing-auth","method":"initialize"}`, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	resp := decodeRPCResponse(t, w)
	if resp.Error == nil || resp.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeUnauthorized) {
		t.Fatalf("error=%#v, want unauthorized", resp.Error)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeUnauthorized) {
		t.Fatalf("audit=%#v, want one unauthorized failure", records)
	}
}

func TestStreamableHTTPNotificationAuthFailureDoesNotRespond(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit})

	w := postJSONRPC(t, router, "", `{"jsonrpc":"2.0","method":"initialize"}`, nil)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body=%q, want empty notification response", w.Body.String())
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeUnauthorized) {
		t.Fatalf("audit=%#v, want one unauthorized failure", records)
	}
}

func TestStreamableHTTPToolsListRequiresValidAuth(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{Name: "easydo_test", Description: "Test tool", OperationType: OperationRead, TargetType: "test", InputSchema: map[string]any{"type": "object"}, Handler: noopToolHandler}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	router := newTestHTTPServer(t, ServerOptions{Registry: registry, AuditRecorder: &fakeAuditRecorder{}})

	w := postJSONRPC(t, router, validMCPBearer(t, 7001), `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	resp := decodeRPCResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "easydo_test" {
		t.Fatalf("tools=%#v, want registered tool metadata", tools)
	}
}

func TestStreamableHTTPToolsCallRecordsAudit(t *testing.T) {
	audit := &fakeAuditRecorder{}
	registry := NewRegistry()
	if err := registry.Register(Tool{Name: "easydo_echo", Description: "Echo", OperationType: OperationRead, TargetType: "echo", Handler: func(_ context.Context, invocation Invocation) (ToolResult, error) {
		actor, ok := invocation.Actor.(services.ActorContext)
		if !ok || actor.UserID != 7002 {
			return ToolResult{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
		}
		if invocation.Protocol != "streamable_http" || invocation.RequestID != "call-1" {
			return ToolResult{}, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "bad invocation context"}
		}
		return ToolResult{StructuredContent: map[string]any{"ok": true, "input": invocation.Arguments["message"]}}, nil
	}}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	router := newTestHTTPServer(t, ServerOptions{Registry: registry, AuditRecorder: audit})

	w := postJSONRPC(t, router, validMCPBearer(t, 7002), `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"easydo_echo","arguments":{"message":"hello"}}}`, nil)

	resp := decodeRPCResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content=%#v, want one MCP content item", result["content"])
	}
	contentItem, ok := content[0].(map[string]any)
	if !ok || contentItem["type"] != "text" || contentItem["text"] == "" {
		t.Fatalf("content item=%#v, want non-empty text content", content[0])
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != true || structured["input"] != "hello" {
		t.Fatalf("structuredContent=%#v, want echoed structured result", structured)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].ToolName != "easydo_echo" || records[0].UserID != 7002 || records[0].Status != string(AuditStatusSuccess) {
		t.Fatalf("audit=%#v, want successful tool audit", records)
	}
}

func TestStreamableHTTPRejectsOversizedBody(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, MaxBodySizeBytes: 64})

	w := postJSONRPC(t, router, validMCPBearer(t, 7003), `{"jsonrpc":"2.0","id":"big","method":"tools/list","params":{"x":"`+strings.Repeat("a", 128)+`"}}`, nil)

	resp := decodeRPCResponse(t, w)
	if resp.Error == nil || resp.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeInvalidArgument) {
		t.Fatalf("error=%#v, want invalid_argument", resp.Error)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) {
		t.Fatalf("audit=%#v, want schema/body failure", records)
	}
}

func TestStreamableHTTPRejectsWhenPerUserConcurrencyExceeded(t *testing.T) {
	audit := &fakeAuditRecorder{}
	started := make(chan struct{})
	release := make(chan struct{})
	registry := NewRegistry()
	if err := registry.Register(Tool{Name: "easydo_block", Description: "Block", OperationType: OperationRead, TargetType: "test", Handler: func(ctx context.Context, _ Invocation) (ToolResult, error) {
		close(started)
		select {
		case <-release:
			return ToolResult{StructuredContent: map[string]any{"ok": true}}, nil
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	}}); err != nil {
		t.Fatalf("register tool failed: %v", err)
	}
	router := newTestHTTPServer(t, ServerOptions{Registry: registry, AuditRecorder: audit, MaxInFlightPerUser: 1, MaxInFlightPerIP: 10, RequestTimeout: time.Second})
	auth := validMCPBearer(t, 7004)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"block-1","method":"tools/call","params":{"name":"easydo_block","arguments":{}}}`, nil)
	}()
	<-started

	w := postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"block-2","method":"tools/list"}`, nil)
	close(release)
	<-firstDone

	resp := decodeRPCResponse(t, w)
	if resp.Error == nil || resp.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeConcurrencyLimited) {
		t.Fatalf("error=%#v, want concurrency_limited", resp.Error)
	}
	found := false
	for _, record := range audit.snapshot() {
		if record.RequestID == "block-2" && record.Status == string(AuditStatusConcurrencyLimited) {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit=%#v, want concurrency limited audit for block-2", audit.snapshot())
	}
}

func TestStreamableHTTPRejectsCrossOriginWhenAllowlistUnset(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit})

	w := postJSONRPC(t, router, validMCPBearer(t, 7005), `{"jsonrpc":"2.0","id":"origin-default-deny","method":"tools/list"}`, map[string]string{"Origin": "https://evil.example"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want forbidden origin failure", records)
	}
}

func TestStreamableHTTPRejectsDisallowedOrigin(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}})

	w := postJSONRPC(t, router, validMCPBearer(t, 7005), `{"jsonrpc":"2.0","id":"origin","method":"tools/list"}`, map[string]string{"Origin": "https://evil.example"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want forbidden origin failure", records)
	}
}

func TestStreamableHTTPRejectsDisallowedOriginBeforeAuth(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}})

	w := postJSONRPC(t, router, "", `{"jsonrpc":"2.0","id":"origin-before-auth","method":"tools/list"}`, map[string]string{"Origin": "https://evil.example"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want forbidden origin failure", records)
	}
}

func TestStreamableHTTPRejectsDisallowedOriginBeforeBodyParsing(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}, MaxBodySizeBytes: 64})

	w := postJSONRPC(t, router, validMCPBearer(t, 7007), `{"jsonrpc":"2.0","id":"origin-before-parse","method":"tools/list","params":{"x":"`+strings.Repeat("a", 128)+`"}}`, map[string]string{"Origin": "https://evil.example"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want forbidden origin failure", records)
	}
}

func TestStreamableHTTPNotificationOriginFailureReturnsForbidden(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}})

	w := postJSONRPC(t, router, validMCPBearer(t, 7006), `{"jsonrpc":"2.0","method":"tools/list"}`, map[string]string{"Origin": "https://evil.example"})

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body=%q, want empty transport-level response", w.Body.String())
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want forbidden origin failure", records)
	}
}
