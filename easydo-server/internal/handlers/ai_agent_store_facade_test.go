package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
)

func seedAIAgentStoreOwner(t *testing.T) (*models.User, *models.Workspace) {
	t.Helper()
	db := models.DB
	owner := &models.User{Username: "ai-agent-store-owner", Role: "user", Status: "active"}
	if err := db.Create(owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := &models.Workspace{
		Name:       "ai-agent-store-workspace",
		Slug:       "ai-agent-store-workspace",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       models.WorkspaceKindNormal,
		CreatedBy:  owner.ID,
	}
	if err := db.Create(workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Role:        models.WorkspaceRoleOwner,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   owner.ID,
	}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	return owner, workspace
}

func TestAIAgentStoreCreateProfileForwardsToRuntime(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const requestID = "client.store.profile.create"
	const runtimeRequestID = "runtime.store.profile.create"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-profiles" {
			t.Fatalf("runtime path=%s, want /v1/agent-profiles", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Internal-Token"); got != "runtime-secret" {
			t.Fatalf("runtime internal token=%q", got)
		}
		if got := r.Header.Get("X-Request-ID"); got != requestID {
			t.Fatalf("runtime X-Request-ID=%q, want %q", got, requestID)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		actor := envelope["actor"].(map[string]any)
		if uint64(actor["workspace_id"].(float64)) != workspace.ID {
			t.Fatalf("workspace_id=%v, want %d", actor["workspace_id"], workspace.ID)
		}
		if actor["workspace_role"] != models.WorkspaceRoleOwner {
			t.Fatalf("workspace_role=%v, want owner", actor["workspace_role"])
		}
		if envelope["request_id"] != requestID {
			t.Fatalf("envelope request_id=%v, want %q", envelope["request_id"], requestID)
		}
		payload := envelope["payload"].(map[string]any)
		if payload["name"] != "General Agent" {
			t.Fatalf("payload name=%v", payload["name"])
		}
		if payload["profile_kind"] != "generic" {
			t.Fatalf("profile_kind=%v, want generic", payload["profile_kind"])
		}
		if _, exists := payload["supported_scene_types"]; exists {
			t.Fatalf("payload must not forward legacy supported_scene_types: %v", payload)
		}
		tags := payload["context_tags"].([]any)
		if len(tags) != 0 {
			t.Fatalf("context_tags=%v, want empty", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", runtimeRequestID)
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":91,"name":"General Agent","profile_kind":"generic","context_tags":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "General Agent",
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Request.Header.Set("X-Request-ID", requestID)

	h.CreateProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
	if got := rec.Header().Get("X-Request-ID"); got != runtimeRequestID {
		t.Fatalf("X-Request-ID=%q, want %q", got, runtimeRequestID)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data := body["data"].(map[string]any)
	if data["id"].(float64) != 91 {
		t.Fatalf("forwarded profile id=%v, want 91", data["id"])
	}
	if models.DB.Migrator().HasTable("ai_runtime_profiles") {
		t.Fatal("server facade tests must not create legacy ai_runtime_profiles table")
	}
}

func TestAIAgentStoreOperationsSummaryForwardsRuntimeResponse(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const requestID = "client.store.operations.summary"
	const runtimeRequestID = "runtime.store.operations.summary"

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/operations/summary" {
			t.Fatalf("runtime path=%s, want /v1/operations/summary", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		if got := r.Header.Get("X-Request-ID"); got != requestID {
			t.Fatalf("runtime X-Request-ID=%q, want %q", got, requestID)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.RequestID != requestID {
			t.Fatalf("envelope request_id=%q, want %q", envelope.RequestID, requestID)
		}
		if envelope.Actor.UserID != owner.ID || envelope.Actor.WorkspaceID != workspace.ID {
			t.Fatalf("actor=%+v, want user=%d workspace=%d", envelope.Actor, owner.ID, workspace.ID)
		}
		if len(envelope.Payload) != 0 {
			t.Fatalf("payload=%v, want empty", envelope.Payload)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "15")
		w.Header().Set("X-Request-ID", runtimeRequestID)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"provider_rate_limit","category":"provider_rate_limit","retryable":true}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL: runtime.URL,
		}),
	}
	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/store/ai-agents/operations/summary", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, nil)
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Request.Header.Set("X-Request-ID", requestID)

	h.GetOperationsSummary(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d, want 429 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q, want application/json", got)
	}
	if got := rec.Header().Get("Retry-After"); got != "15" {
		t.Fatalf("Retry-After=%q, want 15", got)
	}
	if got := rec.Header().Get("X-Request-ID"); got != runtimeRequestID {
		t.Fatalf("X-Request-ID=%q, want %q", got, runtimeRequestID)
	}
	if got := rec.Body.String(); got != `{"code":"provider_rate_limit","category":"provider_rate_limit","retryable":true}` {
		t.Fatalf("body=%s", got)
	}
}

func TestAIAgentStoreVersionAndDependencyQueriesForwardToRuntime(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	paths := make(chan string, 3)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"dependencies":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB:            models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL}),
	}
	tests := []struct {
		name   string
		path   string
		invoke func(*gin.Context)
		want   string
	}{
		{name: "profile dependencies", path: "/api/store/ai-agents/profiles/31/dependencies", invoke: h.GetProfileDependencies, want: "/v1/agent-profiles/31/dependencies/query"},
		{name: "resource versions", path: "/api/store/ai-agents/resources/41/versions", invoke: h.ListResourceVersions, want: "/v1/agent-resources/41/versions/query"},
		{name: "resource dependencies", path: "/api/store/ai-agents/resources/41/dependencies", invoke: h.GetResourceDependencies, want: "/v1/agent-resources/41/dependencies/query"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := newAIManagementTestContext(t, http.MethodGet, test.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, nil)
			c.Params = append(c.Params, gin.Param{Key: "id", Value: map[bool]string{true: "31", false: "41"}[strings.Contains(test.name, "profile")]})
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
			test.invoke(c)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if got := <-paths; got != test.want {
				t.Fatalf("runtime path=%s, want %s", got, test.want)
			}
		})
	}
}

func TestAIAgentStoreUpdateProfileUsesRuntimePathIdempotencyKeys(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	idempotencyKeys := make(chan struct {
		path string
		key  string
	}, 2)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		idempotencyKeys <- struct {
			path string
			key  string
		}{path: r.URL.Path, key: envelope.IdempotencyKey}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/agent-profiles/103/query" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":103,"name":"General Agent","context_tags":[]}}`))
		case r.URL.Path == "/v1/agent-profiles/103" && r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":103,"name":"Updated Agent","context_tags":[]}}`))
		default:
			t.Fatalf("unexpected runtime request=%s %s", r.Method, r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{BaseURL: runtime.URL})}
	c, rec := newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-agents/profiles/103", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "Updated Agent",
		"context_tags": []any{},
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "103"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	keysByPath := map[string]string{}
	for range 2 {
		entry := <-idempotencyKeys
		keysByPath[entry.path] = entry.key
	}
	for path, expected := range map[string]string{
		"/v1/agent-profiles/103/query": "runtime:w" + strconv.FormatUint(workspace.ID, 10) + ":v1-agent-profiles-103-query",
		"/v1/agent-profiles/103":       "runtime:w" + strconv.FormatUint(workspace.ID, 10) + ":v1-agent-profiles-103",
	} {
		if got := keysByPath[path]; got != expected {
			t.Fatalf("idempotency key for %s=%q, want %q", path, got, expected)
		}
	}
}

func TestAIAgentStoreListWorkspacesForwardsOwnedRuntimeScope(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-workspaces/query" || r.Method != http.MethodPost {
			t.Fatalf("runtime request=%s %s", r.Method, r.URL.Path)
		}
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Actor.UserID != owner.ID || envelope.Actor.WorkspaceID != workspace.ID {
			t.Fatalf("runtime actor=%+v", envelope.Actor)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":[{"workspace_runtime_id":"aws-test","status":"ready"}]}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{DB: models.DB, RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
		BaseURL: runtime.URL, InternalToken: "runtime-secret",
	})}
	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/store/ai-agents/workspaces", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, nil)
	h.ListWorkspaces(c)

	if rec.Code != http.StatusOK || !sawRequest.Load() {
		t.Fatalf("status=%d body=%s saw_request=%v", rec.Code, rec.Body.String(), sawRequest.Load())
	}
}

func TestAIAgentStoreCreateProfileBlocksViewer(t *testing.T) {
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

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "Blocked",
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for non-owner profile mutation")
	}
}

func TestAIAgentStoreCreateProfileAllowsDeveloperOnlyForEmptyContextTags(t *testing.T) {
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
		tags := payload["context_tags"].([]any)
		if len(tags) != 0 {
			t.Fatalf("context_tags=%v, want empty", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":101,"name":"General Agent","context_tags":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":         "General Agent",
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected empty context tag profile mutation to reach runtime")
	}
}

func TestAIAgentStoreCreateProfileBlocksDeveloperPageAssistantTag(t *testing.T) {
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

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":         "page-ai-assistant",
		"context_tags": []any{"page-assistant"},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called when developer mutates page-assistant tag")
	}
}

func TestAIAgentStoreCreateProfileAllowsAdminForReservedPageAssistant(t *testing.T) {
	_ = openHandlerTestDB(t)
	_, workspace := seedAIAgentStoreOwner(t)
	admin := &models.User{Username: "ai-agent-store-admin", Role: "admin", Status: "active"}
	if err := models.DB.Create(admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		actor := envelope["actor"].(map[string]any)
		if actor["system_role"] != "admin" {
			t.Fatalf("system_role=%v, want admin", actor["system_role"])
		}
		if actor["workspace_role"] != models.WorkspaceRoleOwner {
			t.Fatalf("workspace_role=%v, want owner", actor["workspace_role"])
		}
		payload := envelope["payload"].(map[string]any)
		if payload["name"] != "page-ai-assistant" {
			t.Fatalf("name=%v, want page-ai-assistant", payload["name"])
		}
		tags := payload["context_tags"].([]any)
		if len(tags) != 1 || tags[0] != "page-assistant" {
			t.Fatalf("context_tags=%v, want [page-assistant]", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":102,"name":"page-ai-assistant","context_tags":["page-assistant"]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "page-ai-assistant",
		"context_tags": []any{"page-assistant"},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected admin reserved page assistant mutation to reach runtime")
	}
}

func TestAIAgentStoreRejectsLegacySupportedSceneTypesPayload(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":                  "Legacy",
		"supported_scene_types": []any{"common"},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for legacy supported_scene_types payload")
	}
}

func TestAIAgentStoreRejectsReservedPageAssistantWithoutRequiredTag(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "page-ai-assistant",
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProfile(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for page-ai-assistant without page-assistant tag")
	}
}

func TestAIAgentStoreRejectsReservedPageAssistantRenameAndTagRemoval(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var mutationRequests atomic.Int32
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/agent-profiles/102/query":
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":102,"name":"page-ai-assistant","context_tags":["page-assistant"]}}`))
		case "/v1/agent-profiles/102":
			mutationRequests.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":102}}`))
		default:
			t.Fatalf("unexpected runtime path=%s", r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-agents/profiles/102", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "renamed",
		"context_tags": []any{"page-assistant"},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "102"})
	h.UpdateProfile(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected rename 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	c, rec = newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-agents/profiles/102", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":         "page-ai-assistant",
		"context_tags": []any{},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "102"})
	h.UpdateProfile(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected tag removal 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	if mutationRequests.Load() != 0 {
		t.Fatalf("runtime update should not be called for invalid reserved profile mutations, got %d", mutationRequests.Load())
	}
}

func TestAIAgentStoreRejectsReservedPageAssistantDelete(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var deleteRequests atomic.Int32
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/agent-profiles/102/query":
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":102,"name":"page-ai-assistant","context_tags":["page-assistant"]}}`))
		case "/v1/agent-profiles/102":
			deleteRequests.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":102}}`))
		default:
			t.Fatalf("unexpected runtime path=%s", r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodDelete, "/api/store/ai-agents/profiles/102", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "102"})

	h.DeleteProfile(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if deleteRequests.Load() != 0 {
		t.Fatalf("runtime delete should not be called for reserved page assistant, got %d", deleteRequests.Load())
	}
}

func TestAIAgentStoreCreateResourceForwardsToRuntime(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-resources" {
			t.Fatalf("runtime path=%s, want /v1/agent-resources", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		actor := envelope["actor"].(map[string]any)
		if uint64(actor["workspace_id"].(float64)) != workspace.ID {
			t.Fatalf("workspace_id=%v, want %d", actor["workspace_id"], workspace.ID)
		}
		payload := envelope["payload"].(map[string]any)
		if payload["resource_kind"] != "skill" {
			t.Fatalf("resource_kind=%v, want skill", payload["resource_kind"])
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":7,"resource_kind":"skill","resource_key":"explain-page"}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/resources", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"resource_kind": "skill",
		"resource_key":  "explain-page",
		"name":          "Explain Page",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateResource(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
}

func TestAIAgentStoreListResourcesInjectsReadonlyEasyDoMCPServer(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-resources/query" {
			t.Fatalf("runtime path=%s, want /v1/agent-resources/query", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"mcp_servers":[],"skills":[],"subagent_profiles":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/store/ai-agents/resources", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Request.Header.Set("Authorization", "Bearer user-workspace-token")
	c.Request.Header.Set("Referer", "https://easydo.example.com/store/ai-agents")

	h.ListResources(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data := body["data"].(map[string]any)
	servers := data["mcp_servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("mcp_servers length=%d, want 1", len(servers))
	}
	easydo := servers[0].(map[string]any)
	if easydo["resource_key"] != "easydo" || easydo["name"] != "easydo" {
		t.Fatalf("easydo resource identity=%v/%v, want easydo", easydo["resource_key"], easydo["name"])
	}
	if easydo["readonly"] != true || easydo["builtin"] != true {
		t.Fatalf("easydo readonly flags=%v/%v, want true", easydo["readonly"], easydo["builtin"])
	}
	spec := easydo["spec"].(map[string]any)
	mcpServers := spec["mcpServers"].(map[string]any)
	config := mcpServers["easydo"].(map[string]any)
	if config["type"] != "http" || config["url"] != "https://easydo.example.com/mcp" {
		t.Fatalf("easydo mcp config=%v, want http https://easydo.example.com/mcp", config)
	}
	if _, exists := config["headers"]; exists {
		t.Fatalf("browser-visible built-in MCP config must not contain headers: %v", config["headers"])
	}
	secretRef := easydo["secret_ref"].(map[string]any)
	if secretRef["configured"] != true || secretRef["scope"] != "current_user_workspace" || secretRef["auth_mode"] != "delegated_user_session" {
		t.Fatalf("secret_ref=%v, want configured delegated auth metadata", secretRef)
	}
	responseText := rec.Body.String()
	for _, forbidden := range []string{"Authorization", "Bearer", services.MCPTokenPlainPrefix, "user-workspace-token"} {
		if strings.Contains(responseText, forbidden) {
			t.Fatalf("ListResources response leaked forbidden credential material %q: %s", forbidden, responseText)
		}
	}
	var tokenCount int64
	if err := models.DB.Model(&models.MCPToken{}).
		Where("workspace_id = ? AND user_id = ?", workspace.ID, owner.ID).
		Count(&tokenCount).Error; err != nil {
		t.Fatalf("count mcp tokens failed: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("ListResources created %d MCP tokens, want 0", tokenCount)
	}
}

func TestAIAgentStoreRejectsBuiltinEasyDoMCPResourceMutations(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		sawRequest.Store(true)
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		id     string
		body   map[string]any
		call   func(*gin.Context)
	}{
		{
			name:   "create reserved easydo",
			method: http.MethodPost,
			path:   "/api/store/ai-agents/resources",
			body: map[string]any{
				"resource_kind": "mcp_server",
				"resource_key":  "easydo",
				"name":          "easydo",
			},
			call: h.CreateResource,
		},
		{name: "update builtin easydo", method: http.MethodPut, path: "/api/store/ai-agents/resources/easydo", id: "easydo", body: map[string]any{"name": "changed"}, call: h.UpdateResource},
		{name: "scan builtin easydo", method: http.MethodPost, path: "/api/store/ai-agents/resources/easydo/scan", id: "easydo", body: map[string]any{}, call: h.ScanResource},
		{name: "delete builtin easydo", method: http.MethodDelete, path: "/api/store/ai-agents/resources/easydo", id: "easydo", body: map[string]any{}, call: h.DeleteResource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sawRequest.Store(false)
			c, rec := newAIManagementTestContext(t, tc.method, tc.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, tc.body)
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
			if tc.id != "" {
				c.Params = append(c.Params, gin.Param{Key: "id", Value: tc.id})
			}
			tc.call(c)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
			}
			if sawRequest.Load() {
				t.Fatal("runtime should not be called for builtin easydo MCP mutations")
			}
		})
	}
}

func TestAIAgentStoreScanResourceForwardsToRuntime(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-resources/7/scan" {
			t.Fatalf("runtime path=%s, want /v1/agent-resources/7/scan", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		actor := envelope["actor"].(map[string]any)
		if uint64(actor["workspace_id"].(float64)) != workspace.ID {
			t.Fatalf("workspace_id=%v, want %d", actor["workspace_id"], workspace.ID)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"resource":{"id":7},"discovered_skills":[{"key":"workspace-skills/explain-page"}]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentStoreHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-agents/resources/7/scan", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "7"})

	h.ScanResource(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected scan request to reach runtime")
	}
}
