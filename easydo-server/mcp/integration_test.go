package mcp

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
)

func toolsListNames(t *testing.T, resp JSONRPCResponse) []string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type=%T, want map[string]any", resp.Result)
	}
	items, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools type=%T, want []any", result["tools"])
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("tool type=%T, want map[string]any", item)
		}
		name, ok := tool["name"].(string)
		if !ok {
			t.Fatalf("tool name type=%T, want string", tool["name"])
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestToolsListContainsFirstPhaseToolsOnly(t *testing.T) {
	router := newTestHTTPServer(t, ServerOptions{AuditRecorder: &fakeAuditRecorder{}})

	w := postJSONRPC(t, router, validMCPBearer(t, 7201), `{"jsonrpc":"2.0","id":"tools-list","method":"tools/list"}`, nil)
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	got := toolsListNames(t, decodeRPCResponse(t, w))
	want := []string{
		"easydo_pipeline_get",
		"easydo_pipeline_list",
		"easydo_pipeline_run_cancel",
		"easydo_pipeline_run_get",
		"easydo_pipeline_run_list",
		"easydo_pipeline_task_get",
		"easydo_pipeline_task_retry",
		"easydo_pipeline_trigger",
		"easydo_resource_get",
		"easydo_resource_list",
		"easydo_resource_status",
		"easydo_workspace_get",
		"easydo_workspace_list",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool names=%#v, want %#v", got, want)
	}
}

func TestUnknownToolIsRejected(t *testing.T) {
	audit := &fakeAuditRecorder{}
	router := newTestHTTPServer(t, ServerOptions{AuditRecorder: audit})

	w := postJSONRPC(t, router, validMCPBearer(t, 7202), `{"jsonrpc":"2.0","id":"unknown-tool","method":"tools/call","params":{"name":"easydo_unknown_tool","arguments":{}}}`, nil)
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	resp := decodeRPCResponse(t, w)
	if resp.Error == nil || resp.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("error=%#v, want not_found", resp.Error)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].RequestID != "unknown-tool" || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeNotFound) {
		t.Fatalf("audit=%#v, want one not_found failure", records)
	}
}

func TestNoAIChatbotOrAgentRuntimeToolsAreRegistered(t *testing.T) {
	router := newTestHTTPServer(t, ServerOptions{AuditRecorder: &fakeAuditRecorder{}})

	w := postJSONRPC(t, router, validMCPBearer(t, 7203), `{"jsonrpc":"2.0","id":"no-ai-tools","method":"tools/list"}`, nil)
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	names := toolsListNames(t, decodeRPCResponse(t, w))
	joined := strings.Join(names, " ")
	for _, forbidden := range []string{"chatbot", "agent", "assistant", "scene", "copilot", "sre"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("registered tools=%q, should not contain %q", joined, forbidden)
		}
	}
}

func TestToolResponseDoesNotLeakSecrets(t *testing.T) {
	db := openResourceToolTestDB(t)
	user, workspace := seedResourceToolWorkspaceMember(t, db, "mcp-integration-resource-user", models.WorkspaceRoleViewer)
	large := strings.Repeat("x", 5000)
	resource := models.Resource{
		WorkspaceID:      workspace.ID,
		Name:             "integration-resource",
		Type:             models.ResourceTypeK8sCluster,
		Status:           models.ResourceStatusOnline,
		CreatedBy:        user.ID,
		Endpoint:         "https://cluster.example",
		Metadata:         `{"credential_binding":{"token":"hidden"},"safe":"ok"}`,
		BaseInfo:         `{"nodes":[{"name":"node-1","password":"hidden","measure":"` + large + `"}],"authorization":"Bearer hidden"}`,
		LastCheckResult:  "secret=health-secret",
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	router := newTestHTTPServer(t, ServerOptions{DB: db, AuditRecorder: &fakeAuditRecorder{}})

	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":"resource-status","method":"tools/call","params":{"name":"easydo_resource_status","arguments":{"workspace_id":%d,"resource_id":%d}}}`, workspace.ID, resource.ID)
	w := postJSONRPC(t, router, validMCPBearer(t, user.ID), payload, nil)
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	resp := decodeRPCResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}
	body := strings.ToLower(w.Body.String())
	for _, forbidden := range []string{"credential", "token", "secret", "password", "authorization", "bearer hidden", "health-secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked forbidden text %q: %s", forbidden, w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), "[truncated]") {
		t.Fatalf("response=%s, want truncated marker", w.Body.String())
	}
}

func TestAuditExistsForSuccessFailureTimeoutAndRateLimit(t *testing.T) {
	audit := &fakeAuditRecorder{}
	registry := NewRegistry()
	if err := registry.Register(Tool{Name: "easydo_ok", Description: "Ok", OperationType: OperationRead, TargetType: "test", Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
		return ToolResult{StructuredContent: map[string]any{"ok": true}}, nil
	}}); err != nil {
		t.Fatalf("register ok tool failed: %v", err)
	}
	if err := registry.Register(Tool{Name: "easydo_timeout", Description: "Timeout", OperationType: OperationRead, TargetType: "test", Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
		return ToolResult{}, context.DeadlineExceeded
	}}); err != nil {
		t.Fatalf("register timeout tool failed: %v", err)
	}
	if err := registry.Register(Tool{Name: "easydo_rate_limited", Description: "Rate limited", OperationType: OperationRead, TargetType: "test", Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
		return ToolResult{}, services.ServiceError{Code: services.ErrorCodeRateLimited, Message: "too many requests"}
	}}); err != nil {
		t.Fatalf("register rate-limited tool failed: %v", err)
	}
	router := newTestHTTPServer(t, ServerOptions{Registry: registry, AuditRecorder: audit})
	auth := validMCPBearer(t, 7204)

	decodeRPCResponse(t, postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"audit-success","method":"tools/call","params":{"name":"easydo_ok","arguments":{}}}`, nil))
	decodeRPCResponse(t, postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"audit-failure","method":"tools/call","params":{"name":"easydo_unknown_tool","arguments":{}}}`, nil))
	decodeRPCResponse(t, postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"audit-timeout","method":"tools/call","params":{"name":"easydo_timeout","arguments":{}}}`, nil))
	decodeRPCResponse(t, postJSONRPC(t, router, auth, `{"jsonrpc":"2.0","id":"audit-rate-limit","method":"tools/call","params":{"name":"easydo_rate_limited","arguments":{}}}`, nil))

	statusByRequestID := map[string]string{}
	errorCodeByRequestID := map[string]string{}
	for _, record := range audit.snapshot() {
		statusByRequestID[record.RequestID] = record.Status
		errorCodeByRequestID[record.RequestID] = record.ErrorCode
	}
	if statusByRequestID["audit-success"] != string(AuditStatusSuccess) {
		t.Fatalf("success status=%q, want %q", statusByRequestID["audit-success"], AuditStatusSuccess)
	}
	if statusByRequestID["audit-failure"] != string(AuditStatusFailure) || errorCodeByRequestID["audit-failure"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("failure audit=%#v / %#v, want failure/not_found", statusByRequestID, errorCodeByRequestID)
	}
	if statusByRequestID["audit-timeout"] != string(AuditStatusTimeout) || errorCodeByRequestID["audit-timeout"] != string(services.ErrorCodeTimeout) {
		t.Fatalf("timeout audit=%#v / %#v, want timeout/timeout", statusByRequestID, errorCodeByRequestID)
	}
	if statusByRequestID["audit-rate-limit"] != string(AuditStatusRateLimited) || errorCodeByRequestID["audit-rate-limit"] != string(services.ErrorCodeRateLimited) {
		t.Fatalf("rate-limit audit=%#v / %#v, want rate_limited/rate_limited", statusByRequestID, errorCodeByRequestID)
	}
}
