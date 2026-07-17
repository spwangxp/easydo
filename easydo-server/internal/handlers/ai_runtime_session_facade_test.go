package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
)

type aiRuntimeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn aiRuntimeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func assertAIRuntimeCorrelatedLocalError(t *testing.T, rec *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("expected %d, got %d body=%s", status, rec.Code, rec.Body.String())
	}
	requestID := rec.Header().Get("X-Request-ID")
	if !isValidAIRuntimeRequestID(requestID) {
		t.Fatalf("X-Request-ID=%q, want a valid request id", requestID)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if got := int(numericID(body["code"])); got != status {
		t.Fatalf("body code=%d, want %d", got, status)
	}
	if got := toString(body["message"]); got != message {
		t.Fatalf("body message=%q, want %q", got, message)
	}
	if got := toString(body["request_id"]); got != requestID {
		t.Fatalf("body request_id=%q, want header request id %q", got, requestID)
	}
}

type aiRuntimeFailingBody struct {
	content []byte
	err     error
	read    bool
}

func (body *aiRuntimeFailingBody) Read(buffer []byte) (int, error) {
	if !body.read && len(body.content) > 0 {
		body.read = true
		return copy(buffer, body.content), nil
	}
	return 0, body.err
}

func (*aiRuntimeFailingBody) Close() error { return nil }

func TestAIRuntimeSessionFacadePreservesRequestCorrelationAndRuntimeErrorResponse(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const incomingRequestID = "client.req_123:attempt-1"
	const runtimeRequestID = "runtime.req_456:error"
	const responseBody = `{"code":"profile_invalid","message":"invalid profile"}`

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode runtime envelope failed: %v", err)
			return
		}
		if envelope.RequestID != incomingRequestID {
			t.Errorf("envelope request_id=%q, want %q", envelope.RequestID, incomingRequestID)
		}
		if got := r.Header.Get("X-Request-ID"); got != incomingRequestID {
			t.Errorf("runtime X-Request-ID=%q, want %q", got, incomingRequestID)
		}
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.Header().Set("X-Request-ID", runtimeRequestID)
		w.Header().Set("Retry-After", "30")
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1750000000")
		w.Header().Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		w.Header().Set("Tracestate", "vendor=test")
		w.Header().Set("Set-Cookie", "runtime_secret=must-not-leak")
		w.Header().Set("X-Internal-Token", "must-not-leak")
		w.Header().Set("X-Upstream-Secret", "must-not-leak")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL: runtime.URL,
	})}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"profile_id":   91,
		"context_tags": []any{},
	})
	c.Request.Header.Set("X-Request-ID", incomingRequestID)
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := rec.Header().Get("X-Request-ID"); got != runtimeRequestID {
		t.Fatalf("X-Request-ID=%q, want %q", got, runtimeRequestID)
	}
	for header, expected := range map[string]string{
		"Retry-After":         "30",
		"RateLimit-Remaining": "0",
		"X-RateLimit-Reset":   "1750000000",
		"Traceparent":         "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"Tracestate":          "vendor=test",
	} {
		if got := rec.Header().Get(header); got != expected {
			t.Fatalf("%s=%q, want %q", header, got, expected)
		}
	}
	for _, header := range []string{"Set-Cookie", "X-Internal-Token", "X-Upstream-Secret"} {
		if got := rec.Header().Values(header); len(got) != 0 {
			t.Fatalf("unsafe upstream header %s leaked: %v", header, got)
		}
	}
	if rec.Body.String() != responseBody {
		t.Fatalf("body=%q, want %q", rec.Body.String(), responseBody)
	}
}

func TestAIRuntimeSessionFacadeRejectsMalformedRuntimeRequestID(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const requestID = "client.valid.request"

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "malformed runtime request id")
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":44}}`)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL})}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"profile_id":   91,
		"context_tags": []any{},
	})
	c.Request.Header.Set("X-Request-ID", requestID)
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-ID"); got != requestID {
		t.Fatalf("X-Request-ID=%q, want fallback %q", got, requestID)
	}
}

func TestAIRuntimeResponseHeaderAllowlistExcludesHopByHopAndSecretHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	upstream := make(http.Header)
	for header, value := range map[string]string{
		"Retry-After":       "10",
		"RateLimit-Limit":   "100",
		"Traceparent":       "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"Connection":        "keep-alive",
		"Keep-Alive":        "timeout=5",
		"Transfer-Encoding": "chunked",
		"Set-Cookie":        "runtime_secret=must-not-leak",
		"Authorization":     "Bearer must-not-leak",
		"X-Internal-Token":  "must-not-leak",
	} {
		upstream.Set(header, value)
	}
	copyAIRuntimeAllowedResponseHeaders(c, upstream)

	for header, expected := range map[string]string{
		"Retry-After":     "10",
		"RateLimit-Limit": "100",
		"Traceparent":     "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	} {
		if got := rec.Header().Get(header); got != expected {
			t.Fatalf("%s=%q, want %q", header, got, expected)
		}
	}
	for _, header := range []string{"Connection", "Keep-Alive", "Transfer-Encoding", "Set-Cookie", "Authorization", "X-Internal-Token"} {
		if got := rec.Header().Values(header); len(got) != 0 {
			t.Fatalf("unsafe header %s leaked: %v", header, got)
		}
	}
}

func TestAIRuntimeSessionFacadeGeneratesUniqueRequestIDsForInvalidInput(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	requestIDs := make(chan string, 2)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode runtime envelope failed: %v", err)
			return
		}
		if got := r.Header.Get("X-Request-ID"); got != envelope.RequestID {
			t.Errorf("runtime X-Request-ID=%q envelope=%q", got, envelope.RequestID)
		}
		requestIDs <- envelope.RequestID
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", envelope.RequestID)
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":44}}`)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL: runtime.URL,
	})}
	for range 2 {
		c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
			"profile_id":   91,
			"context_tags": []any{},
		})
		c.Request.Header.Set("X-Request-ID", "invalid request id")
		c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
		h.CurrentSession(c)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	first := <-requestIDs
	second := <-requestIDs
	if first == "" || second == "" || first == second {
		t.Fatalf("generated request IDs must be non-empty and unique: %q %q", first, second)
	}
	if first == "invalid request id" || second == "invalid request id" {
		t.Fatalf("invalid incoming request ID was preserved: %q %q", first, second)
	}
}

func TestAIRuntimeSessionFacadeMapsOnlyConnectionFailureToStableTransportError(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	runtimeURL := runtime.URL
	runtime.Close()
	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL: runtimeURL,
		Timeout: 100 * time.Millisecond,
	})}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"profile_id":   91,
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502 body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body["code"] != "ai_runtime_transport_error" {
		t.Fatalf("code=%v body=%s", body["code"], rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), runtimeURL) {
		t.Fatalf("transport descriptor leaked connection details: %s", rec.Body.String())
	}
}

func TestAIRuntimeSessionFacadeReportsBufferedResponseReadFailure(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	readFailure := errors.New("forced response read failure")
	client := &http.Client{Transport: aiRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       &aiRuntimeFailingBody{err: readFailure},
		}, nil
	})}
	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL:    "http://ai-runtime.test",
		HTTPClient: client,
	})}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"profile_id":   91,
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500 body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body["code"] != "ai_runtime_response_error" {
		t.Fatalf("code=%v body=%s", body["code"], rec.Body.String())
	}
}

func TestAIRuntimeSessionStreamPreservesNonSuccessRuntimeResponse(t *testing.T) {
	const runtimeRequestID = "runtime.stream.conflict"
	const responseBody = `{"code":"run_conflict","message":"run already active"}`
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Request-ID", runtimeRequestID)
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL})}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/sessions/44/entries/stream", nil)
	c.Request.Header.Set("X-Request-ID", "client.stream.request")
	h.forwardStream(c, GovernanceContext{UserID: 1, WorkspaceID: 1}, http.MethodPost, "/v1/sessions/44/entries/stream", map[string]any{})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := rec.Header().Get("X-Request-ID"); got != runtimeRequestID {
		t.Fatalf("X-Request-ID=%q", got)
	}
	if rec.Body.String() != responseBody {
		t.Fatalf("body=%q, want %q", rec.Body.String(), responseBody)
	}
}

func TestAIRuntimeSessionStreamReportsCopyFailure(t *testing.T) {
	copyFailure := errors.New("forced stream copy failure")
	client := &http.Client{Transport: aiRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-ID": []string{"runtime.stream.failure"},
			},
			Body: &aiRuntimeFailingBody{content: []byte("event: heartbeat\ndata: {}\n\n"), err: copyFailure},
		}, nil
	})}
	h := &AIRuntimeSessionHandler{RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL:    "http://ai-runtime.test",
		HTTPClient: client,
	})}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/sessions/44/entries/stream", nil)
	h.forwardStream(c, GovernanceContext{UserID: 1, WorkspaceID: 1}, http.MethodPost, "/v1/sessions/44/entries/stream", map[string]any{})

	if rec.Code != http.StatusOK || rec.Body.String() != "event: heartbeat\ndata: {}\n\n" {
		t.Fatalf("unexpected streamed response status=%d body=%q", rec.Code, rec.Body.String())
	}
	if len(c.Errors) != 1 || !errors.Is(c.Errors[0].Err, copyFailure) {
		t.Fatalf("stream copy failure was not reported: %v", c.Errors)
	}
}

func TestAIRuntimeSessionFacadeForwardsCurrentSession(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/current" {
			t.Fatalf("runtime path=%s, want /v1/sessions/current", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "runtime-secret" {
			t.Fatalf("runtime internal token=%q", got)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		expectedIdempotencyKey := fmt.Sprintf("runtime:w%d:v1-sessions-current", workspace.ID)
		if envelope["idempotency_key"] != expectedIdempotencyKey {
			t.Fatalf("idempotency_key=%v, want %s", envelope["idempotency_key"], expectedIdempotencyKey)
		}
		expectedRequestID := "client.current-session.request"
		if envelope["request_id"] != expectedRequestID {
			t.Fatalf("request_id=%v, want %s", envelope["request_id"], expectedRequestID)
		}
		if r.Header.Get("X-Request-ID") != expectedRequestID {
			t.Fatalf("X-Request-ID=%q, want %s", r.Header.Get("X-Request-ID"), expectedRequestID)
		}
		actor := envelope["actor"].(map[string]any)
		if uint64(actor["workspace_id"].(float64)) != workspace.ID {
			t.Fatalf("workspace_id=%v, want %d", actor["workspace_id"], workspace.ID)
		}
		payload := envelope["payload"].(map[string]any)
		if _, exists := payload["scene"]; exists {
			t.Fatalf("payload must not forward legacy scene: %v", payload)
		}
		if _, exists := payload["scene_type"]; exists {
			t.Fatalf("payload must not forward legacy scene_type: %v", payload)
		}
		if payload["profile_id"].(float64) != 91 {
			t.Fatalf("profile_id=%v, want 91", payload["profile_id"])
		}
		tags := payload["context_tags"].([]any)
		if len(tags) != 1 || tags[0] != "page-assistant" {
			t.Fatalf("context_tags=%v, want [page-assistant]", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":44,"profile_id":91,"context_tags":["page-assistant"]}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"profile_id":    91,
		"context_tags":  []any{"page-assistant"},
		"business_type": "workspace",
		"business_id":   strconv.FormatUint(workspace.ID, 10),
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Request.Header.Set("X-Request-ID", "client.current-session.request")

	h.CurrentSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeAllowsDeveloperEmptyContextTags(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	if err := models.DB.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ?", workspace.ID, owner.ID).
		Update("role", models.WorkspaceRoleDeveloper).Error; err != nil {
		t.Fatalf("downgrade member failed: %v", err)
	}

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		payload := envelope["payload"].(map[string]any)
		if _, exists := payload["scene"]; exists {
			t.Fatalf("payload must not forward legacy scene: %v", payload)
		}
		tags := payload["context_tags"].([]any)
		if len(tags) != 0 {
			t.Fatalf("context_tags=%v, want empty", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":45,"profile_id":91,"context_tags":[]}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"profile_id":    91,
		"context_tags":  []any{},
		"business_type": "workspace",
		"business_id":   strconv.FormatUint(workspace.ID, 10),
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected empty context tag request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeBlocksDeveloperPageAssistantContextTag(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	if err := models.DB.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ?", workspace.ID, owner.ID).
		Update("role", models.WorkspaceRoleDeveloper).Error; err != nil {
		t.Fatalf("downgrade member failed: %v", err)
	}

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"profile_id":    91,
		"context_tags":  []any{"page-assistant"},
		"business_type": "workspace",
		"business_id":   strconv.FormatUint(workspace.ID, 10),
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for developer page-assistant context")
	}
}

func TestAIRuntimeSessionFacadeBlocksViewerEmptyContextTags(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	if err := models.DB.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ?", workspace.ID, owner.ID).
		Update("role", models.WorkspaceRoleViewer).Error; err != nil {
		t.Fatalf("downgrade member failed: %v", err)
	}

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{
		"profile_id":    91,
		"context_tags":  []any{},
		"business_type": "workspace",
		"business_id":   strconv.FormatUint(workspace.ID, 10),
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for viewer empty context tags")
	}
}

func TestAIRuntimeSessionFacadeBlocksDeveloperPageAssistantSessionList(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	if err := models.DB.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ?", workspace.ID, owner.ID).
		Update("role", models.WorkspaceRoleDeveloper).Error; err != nil {
		t.Fatalf("downgrade member failed: %v", err)
	}

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/sessions", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10) + "&context_tags=page-assistant"

	h.ListSessions(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for developer page-assistant session list")
	}
}

func TestAIRuntimeSessionFacadeUpdateSessionModelForwardsRuntimeModelOverride(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	sessionID := "44"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/"+sessionID+"/model" {
			t.Fatalf("runtime path=%s, want /v1/sessions/%s/model", r.URL.Path, sessionID)
		}
		if r.Method != http.MethodPut {
			t.Fatalf("runtime method=%s, want PUT", r.Method)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Actor.WorkspaceID != workspace.ID {
			t.Fatalf("workspace_id=%d, want %d", envelope.Actor.WorkspaceID, workspace.ID)
		}
		model, _ := envelope.Payload["model"].(map[string]any)
		if model["provider_model_key"] != "claude-sonnet-4" {
			t.Fatalf("model override not forwarded: %#v", envelope.Payload)
		}
		inference, _ := envelope.Payload["inference"].(map[string]any)
		if inference["thinking_level"] != "high" {
			t.Fatalf("thinking_level not forwarded: %#v", envelope.Payload)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"session":{"id":44,"model_override":{"model":{"provider_model_key":"claude-sonnet-4"},"inference":{"thinking_level":"high"}}}}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPut, "/api/ai/sessions/"+sessionID+"/model", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"model": map[string]any{
			"provider_model_key": "claude-sonnet-4",
		},
		"inference": map[string]any{
			"thinking_level": "high",
		},
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: sessionID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateSessionModel(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeActionDecisionStreamsContinuation(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	actionID := "a_w3_000001_000001_000009"

	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/actions/"+actionID+"/decision/stream" {
			t.Fatalf("runtime path=%s, want /v1/actions/%s/decision/stream", r.URL.Path, actionID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		payload, _ := envelope["payload"].(map[string]any)
		if payload["decision"] != "approve_session" {
			t.Fatalf("forwarded decision=%v, want approve_session", payload["decision"])
		}
		sawDecision.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: tool.executed\ndata: {\"action_id\":%q,\"tool_name\":\"easydo_pipeline_trigger\"}\n\n", actionID)
		_, _ = fmt.Fprint(w, "event: answer_delta\ndata: {\"delta\":\"已触发\"}\n\n")
		_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/actions/"+actionID+"/decision/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_session",
		"client_decision_id": actionID + "-approve",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: actionID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DecideActionStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type=%q, want text/event-stream", got)
	}
	if !sawDecision.Load() || !strings.Contains(rec.Body.String(), "event: tool.executed") {
		t.Fatalf("expected action decision continuation stream, got %s", rec.Body.String())
	}
}

func TestAIRuntimeSessionFacadeForwardsPiApprovalDecision(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/pi-approval/decision" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/pi-approval/decision", r.URL.Path, runtimeRunID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		payload, _ := envelope["payload"].(map[string]any)
		if payload["decision"] != "approve_once" {
			t.Fatalf("forwarded decision=%v, want approve_once", payload["decision"])
		}
		sawDecision.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":200,"data":{"run":{"status":"completed"},"assistant_entry":{"status":"completed"}}}`)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/runs/"+runtimeRunID+"/pi-approval/decision", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_once",
		"client_decision_id": "pi-approve",
	})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DecidePiApproval(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawDecision.Load() || !strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("expected pi approval response, got %s", rec.Body.String())
	}
}

func TestAIRuntimeSessionFacadeForwardsPiApprovalDecisionStream(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"

	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/pi-approval/decision/stream" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/pi-approval/decision/stream", r.URL.Path, runtimeRunID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "runtime-secret" {
			t.Fatalf("runtime internal token=%q", got)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Payload["decision"] != "approve_once" || envelope.Payload["client_decision_id"] != "pi-approve-stream" {
			t.Fatalf("unexpected decision payload: %#v", envelope.Payload)
		}
		sawDecision.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: runtime_event\ndata: {\"event\":{\"type\":\"permission.resolved\"}}\n\n"))
		_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/runs/"+runtimeRunID+"/pi-approval/decision/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_once",
		"client_decision_id": "pi-approve-stream",
	})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DecidePiApprovalStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawDecision.Load() || !strings.Contains(rec.Body.String(), "permission.resolved") {
		t.Fatalf("expected pi approval stream response, got %s", rec.Body.String())
	}
}

func TestAIRuntimeSessionFacadeForwardsRunEventReplay(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	afterEventID := runtimeRunID + ":ev000001"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/events/query" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/events/query", r.URL.Path, runtimeRunID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if got := envelope.Payload["after_event_id"]; got != afterEventID {
			t.Fatalf("after_event_id=%q, want %q", got, afterEventID)
		}
		if envelope.Actor.UserID != owner.ID {
			t.Fatalf("runtime actor user_id=%d, want %d", envelope.Actor.UserID, owner.ID)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"events":[]}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/runs/"+runtimeRunID+"/events", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10) + "&after_event_id=" + url.QueryEscape(afterEventID)

	h.ListRunEvents(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeRunEventValidationReturnsCorrelatedErrors(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var requestCount atomic.Int64
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	tests := []struct {
		name       string
		method     string
		path       string
		paramKey   string
		paramValue string
		status     int
		message    string
		invoke     func(*gin.Context)
	}{
		{name: "list missing run id", method: http.MethodGet, path: "/api/ai/runs//events", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.ListRunEvents},
		{name: "list malformed run id", method: http.MethodGet, path: "/api/ai/runs/r_missing_p113/events", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.ListRunEvents},
		{name: "stream missing run id", method: http.MethodPost, path: "/api/ai/runs//events/stream", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.StreamRunEvents},
		{name: "stream malformed run id", method: http.MethodPost, path: "/api/ai/runs/r_missing_p113/events/stream", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.StreamRunEvents},
		{name: "approval missing run id", method: http.MethodPost, path: "/api/ai/runs//pi-approval/decision", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.DecidePiApproval},
		{name: "approval malformed run id", method: http.MethodPost, path: "/api/ai/runs/r_missing_p113/pi-approval/decision", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.DecidePiApproval},
		{name: "approval stream missing run id", method: http.MethodPost, path: "/api/ai/runs//pi-approval/decision/stream", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.DecidePiApprovalStream},
		{name: "approval stream malformed run id", method: http.MethodPost, path: "/api/ai/runs/r_missing_p113/pi-approval/decision/stream", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.DecidePiApprovalStream},
		{name: "artifact missing id", method: http.MethodGet, path: "/api/ai/artifacts/", paramKey: "artifact_id", status: http.StatusBadRequest, message: "artifact_id is required", invoke: h.GetArtifact},
		{name: "artifact malformed id", method: http.MethodGet, path: "/api/ai/artifacts/art_missing_p113", paramKey: "artifact_id", paramValue: "art_missing_p113", status: http.StatusNotFound, message: "Runtime artifact not found", invoke: h.GetArtifact},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := newAIManagementTestContext(t, test.method, test.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
			c.Params = append(c.Params, gin.Param{Key: test.paramKey, Value: test.paramValue})
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

			test.invoke(c)

			assertAIRuntimeCorrelatedLocalError(t, rec, test.status, test.message)
		})
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("runtime request count=%d, want 0", got)
	}
}

func TestAIRuntimeExecutionInvalidJSONReturnsCorrelatedError(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var requestCount atomic.Int64
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer runtime.Close()

	runtimeHandler := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL, InternalToken: "runtime-secret"})}
	chatboxHandler := &AIAgentChatboxHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL, InternalToken: "runtime-secret"})}
	tests := []struct {
		name          string
		workspaceRole string
		invoke        func(*gin.Context)
	}{
		{name: "runtime cancel", workspaceRole: models.WorkspaceRoleOwner, invoke: runtimeHandler.CancelSession},
		{name: "chatbox cancel", workspaceRole: models.WorkspaceRoleDeveloper, invoke: chatboxHandler.CancelSession},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/44/cancel", owner.ID, owner.Role, workspace.ID, test.workspaceRole, models.WorkspaceKindNormal, nil)
			c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
			c.Request.Body = io.NopCloser(strings.NewReader("{"))
			c.Request.Header.Set("Content-Type", "application/json")

			test.invoke(c)

			assertAIRuntimeCorrelatedLocalError(t, rec, http.StatusBadRequest, "请求参数无效")
		})
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("runtime request count=%d, want 0", got)
	}
}

func TestAIRuntimeSessionFacadeForwardsChildRunEventReplayWithParentContext(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	childRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000002_000003"
	parentRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	parentActionID := "a_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001_000001"
	childRunLinkID := "cl_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001_000001_000001"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+childRunID+"/events/query" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/events/query", r.URL.Path, childRunID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Payload["parent_runtime_run_id"] != parentRunID || envelope.Payload["parent_action_id"] != parentActionID || envelope.Payload["child_run_link_id"] != childRunLinkID {
			t.Fatalf("missing child run parent context: %#v", envelope.Payload)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"events":[]}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/runs/"+childRunID+"/events", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: childRunID})
	query := url.Values{}
	query.Set("workspace_id", strconv.FormatUint(workspace.ID, 10))
	query.Set("parent_runtime_run_id", parentRunID)
	query.Set("parent_action_id", parentActionID)
	query.Set("child_run_link_id", childRunLinkID)
	c.Request.URL.RawQuery = query.Encode()

	h.ListRunEvents(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeStreamsRunEventsWithResumeCursor(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	afterEventID := "s_w1_000001:7"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/events/stream" {
			t.Fatalf("runtime path=%s", r.URL.Path)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Payload["after_event_id"] != afterEventID {
			t.Fatalf("after_event_id=%v, want %s", envelope.Payload["after_event_id"], afterEventID)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: s_w1_000001:8\nevent: runtime_event\ndata: {}\n\n"))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL: runtime.URL, InternalToken: "runtime-secret",
	})}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/runs/"+runtimeRunID+"/events/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"after_event_id": afterEventID,
	})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.StreamRunEvents(c)

	if rec.Code != http.StatusOK || !sawRequest.Load() {
		t.Fatalf("expected streamed runtime response, code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAIRuntimeSessionFacadeForwardsRuntimeArtifact(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	artifactID := "art_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001_000001"
	parentRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	parentActionID := "a_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001_000001"
	childRunLinkID := "cl_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001_000001_000001"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/artifacts/"+artifactID+"/query" {
			t.Fatalf("runtime path=%s, want /v1/artifacts/%s/query", r.URL.Path, artifactID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "runtime-secret" {
			t.Fatalf("runtime internal token=%q", got)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Actor.WorkspaceID != workspace.ID {
			t.Fatalf("workspace_id=%d, want %d", envelope.Actor.WorkspaceID, workspace.ID)
		}
		if envelope.Payload["parent_runtime_run_id"] != parentRunID || envelope.Payload["parent_action_id"] != parentActionID || envelope.Payload["child_run_link_id"] != childRunLinkID {
			t.Fatalf("missing parent artifact context: %#v", envelope.Payload)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"artifact_id":"` + artifactID + `","artifact_type":"subagent_transcript"}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/artifacts/"+artifactID, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Params = append(c.Params, gin.Param{Key: "artifact_id", Value: artifactID})
	query := url.Values{}
	query.Set("workspace_id", strconv.FormatUint(workspace.ID, 10))
	query.Set("parent_runtime_run_id", parentRunID)
	query.Set("parent_action_id", parentActionID)
	query.Set("child_run_link_id", childRunLinkID)
	c.Request.URL.RawQuery = query.Encode()

	h.GetArtifact(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIRuntimeSessionFacadeRejectsLegacyScenePayload(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/current", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"scene": map[string]any{
			"type": "page-ai-assistant",
			"code": "page-ai-assistant:global",
		},
		"profile_id": 91,
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CurrentSession(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for legacy scene payload")
	}
}

func TestAIRuntimeSessionFacadeStreamsEntries(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/44/entries/stream" {
			t.Fatalf("runtime path=%s, want /v1/sessions/44/entries/stream", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "runtime-secret" {
			t.Fatalf("runtime internal token=%q", got)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: reasoning_delta\ndata: {\"delta\":\"thinking\",\"elapsed_ms\":7}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: answer_delta\ndata: {\"delta\":\"answer\",\"elapsed_ms\":11}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: done\ndata: {\"timings\":{\"total_ms\":12}}\n\n")
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/44/entries/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"content":         "ebpf 有什么功能",
		"client_entry_id": "stream-client-1",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateEntryStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type=%q, want text/event-stream", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("cache-control=%q, want no-cache, no-transform", got)
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("x-accel-buffering=%q, want no", got)
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: reasoning_delta") || !strings.Contains(body, "event: answer_delta") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream body missing expected events: %s", body)
	}
}

func TestAIRuntimeSessionFacadeCancelsSessionRun(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/44/cancel" {
			t.Fatalf("runtime path=%s, want /v1/sessions/44/cancel", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Payload["runtime_run_id"] != "r_w1_000001_000001" {
			t.Fatalf("runtime_run_id=%v", envelope.Payload["runtime_run_id"])
		}
		sawRequest.Store(true)
		_, _ = w.Write([]byte(`{"code":200,"data":{"runtime_run_id":"r_w1_000001_000001","status":"cancelled"}}`))
	}))
	defer runtime.Close()

	h := &AIRuntimeSessionHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/sessions/44/cancel", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"runtime_run_id": "r_w1_000001_000001",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})

	h.CancelSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected cancel request to reach runtime")
	}
}
