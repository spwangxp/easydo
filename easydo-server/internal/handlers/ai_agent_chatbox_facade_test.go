package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
)

func TestAIAgentChatboxListProfilesReturnsCallableCommonProfiles(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent-profiles/query" {
			t.Fatalf("runtime path=%s, want /v1/agent-profiles/query", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":[
			{"id":1,"name":"General Draft","status":"draft","context_tags":[]},
			{"id":2,"name":"General Active","status":"active","context_tags":[]},
			{"id":3,"name":"Page Assistant","status":"active","context_tags":["page-assistant"]},
			{"id":4,"name":"Disabled General","status":"disabled","context_tags":[]},
			{"id":5,"name":"Archived General","status":"archived","context_tags":[]}
		]}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/agent-chatbox/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, nil)
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ListProfiles(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	rows := body["data"].([]any)
	if len(rows) != 2 {
		t.Fatalf("profile count=%d, want 2: %s", len(rows), rec.Body.String())
	}
	if rows[0].(map[string]any)["name"] != "General Draft" || rows[1].(map[string]any)["name"] != "General Active" {
		t.Fatalf("unexpected profiles=%v", rows)
	}
}

func TestAIAgentChatboxOpenSessionBuildsCommonPayloadAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	idempotencyKeys := make(chan struct {
		path string
		key  string
	}, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sessions/query" {
			var envelope services.AIRuntimeEnvelope
			if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode runtime query envelope failed: %v", err)
			}
			idempotencyKeys <- struct {
				path string
				key  string
			}{path: r.URL.Path, key: envelope.IdempotencyKey}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":[]}`))
			return
		}
		if r.URL.Path != "/v1/sessions/current" {
			t.Fatalf("runtime path=%s, want /v1/sessions/current", r.URL.Path)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		idempotencyKeys <- struct {
			path string
			key  string
		}{path: r.URL.Path, key: strings.TrimSpace(fmt.Sprint(envelope["idempotency_key"]))}
		payload := envelope["payload"].(map[string]any)
		if _, exists := payload["scene"]; exists {
			t.Fatalf("payload must not include legacy scene: %v", payload)
		}
		if payload["profile_id"].(float64) != 91 {
			t.Fatalf("profile_id=%v, want 91", payload["profile_id"])
		}
		if payload["profile_version_id"] != "latest" {
			t.Fatalf("profile_version_id=%v, want latest", payload["profile_version_id"])
		}
		tags := payload["context_tags"].([]any)
		if len(tags) != 0 {
			t.Fatalf("context_tags=%v, want empty", tags)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":88,"source":"agent_chatbox","profile_id":91,"profile_version_key":"latest","context_tags":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/open", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"agent_profile_id":         91,
		"agent_profile_version_id": "latest",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.OpenSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
	keysByPath := map[string]string{}
	for range 2 {
		entry := <-idempotencyKeys
		keysByPath[entry.path] = entry.key
	}
	for path, expected := range map[string]string{
		"/v1/sessions/query":   fmt.Sprintf("runtime:w%d:v1-sessions-query", workspace.ID),
		"/v1/sessions/current": fmt.Sprintf("runtime:w%d:v1-sessions-current", workspace.ID),
	} {
		if got := keysByPath[path]; got != expected {
			t.Fatalf("idempotency key for %s=%q, want %q", path, got, expected)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data := body["data"].(map[string]any)
	if data["url"] != "/store/ai-agents/chat/88" {
		t.Fatalf("url=%v, want /store/ai-agents/chat/88", data["url"])
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.session.opened").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxOpenSessionRestoresRecentActiveSession(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var queryCount atomic.Int32
	var currentCount atomic.Int32
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions/query":
			queryCount.Add(1)
			var envelope map[string]any
			if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode runtime envelope failed: %v", err)
			}
			payload := envelope["payload"].(map[string]any)
			if _, exists := payload["scene_type"]; exists {
				t.Fatalf("query payload must not include legacy scene_type: %v", payload)
			}
			if payload["profile_id"].(float64) != 91 {
				t.Fatalf("query profile_id=%v, want 91", payload["profile_id"])
			}
			_, _ = w.Write([]byte(`{"code":200,"data":[{"id":77,"source":"agent_chatbox","status":"active","profile_id":91,"profile_version_key":"latest","context_tags":[]}]}`))
		case "/v1/sessions/current":
			currentCount.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":88}}`))
		default:
			t.Fatalf("unexpected runtime path=%s", r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/open", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"agent_profile_id":         91,
		"agent_profile_version_id": "latest",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.OpenSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if queryCount.Load() != 1 || currentCount.Load() != 0 {
		t.Fatalf("queryCount=%d currentCount=%d, want query only", queryCount.Load(), currentCount.Load())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data := body["data"].(map[string]any)
	if data["url"] != "/store/ai-agents/chat/77" {
		t.Fatalf("url=%v, want restored session URL", data["url"])
	}
}

func TestAIAgentChatboxCreateSessionAlwaysUsesFreshTimestamp(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawCurrent atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/current" {
			t.Fatalf("runtime path=%s, want /v1/sessions/current", r.URL.Path)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		payload := envelope["payload"].(map[string]any)
		if strings.TrimSpace(fmt.Sprint(payload["first_session_timestamp"])) == "" {
			t.Fatalf("first_session_timestamp is required for new sessions: %v", payload)
		}
		sawCurrent.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":99,"source":"agent_chatbox","profile_id":91,"profile_version_key":"latest","context_tags":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"agent_profile_id":         91,
		"agent_profile_version_id": "latest",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawCurrent.Load() {
		t.Fatal("expected runtime current session request")
	}
}

func TestAIAgentChatboxBlocksViewerAccess(t *testing.T) {
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

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/agent-chatbox/profiles", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, nil)
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ListProfiles(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for viewer")
	}
}

func TestAIAgentChatboxQueueRoutesProxyRuntimeContract(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const sessionID = "44"
	const queueItemID = "queue/item 1"

	tests := []struct {
		name               string
		method             string
		path               string
		payload            map[string]any
		runtimePath        string
		runtimeEscapedPath string
		runtimeRequestID   string
		runtimeStatus      int
		runtimeBody        string
		invoke             func(*AIAgentChatboxHandler, *gin.Context)
	}{
		{
			name:   "enqueue forwards payload with governed actor workspace",
			method: http.MethodPost,
			path:   "/api/ai/agent-chatbox/sessions/44/queue-items",
			payload: map[string]any{
				"workspace_id":   999999,
				"mode":           "follow_up",
				"content":        "queued question",
				"client_item_id": "client-queue-1",
			},
			runtimePath:      "/v1/sessions/44/queue/items",
			runtimeRequestID: "runtime.queue.enqueue",
			runtimeStatus:    http.StatusAccepted,
			runtimeBody:      `{"code":202,"request_id":"runtime.queue.enqueue","runtime_run_id":"run-queue-1","status":"queued","data":{"id":"queue-1"}}`,
			invoke: func(h *AIAgentChatboxHandler, c *gin.Context) {
				h.EnqueueQueueItem(c)
			},
		},
		{
			name:             "list posts empty query payload",
			method:           http.MethodGet,
			path:             "/api/ai/agent-chatbox/sessions/44/queue-items",
			payload:          nil,
			runtimePath:      "/v1/sessions/44/queue/query",
			runtimeRequestID: "runtime.queue.list",
			runtimeStatus:    http.StatusOK,
			runtimeBody:      `{"code":200,"request_id":"runtime.queue.list","runtime_run_id":"run-queue-1","status":"running","data":[]}`,
			invoke: func(h *AIAgentChatboxHandler, c *gin.Context) {
				h.ListQueueItems(c)
			},
		},
		{
			name:               "cancel preserves escaped queue item id and runtime error",
			method:             http.MethodDelete,
			path:               "/api/ai/agent-chatbox/sessions/44/queue-items/queue%2Fitem%201",
			payload:            nil,
			runtimePath:        "/v1/sessions/44/queue/items/queue/item 1/cancel",
			runtimeEscapedPath: "/v1/sessions/44/queue/items/queue%2Fitem%201/cancel",
			runtimeRequestID:   "runtime.queue.cancel",
			runtimeStatus:      http.StatusConflict,
			runtimeBody:        `{"code":"queue_item_not_cancellable","request_id":"runtime.queue.cancel","runtime_run_id":"run-queue-1","status":"running","message":"queue item is already running"}`,
			invoke: func(h *AIAgentChatboxHandler, c *gin.Context) {
				h.CancelQueueItem(c)
			},
		},
		{
			name:   "reorder forwards ordered queue ids",
			method: http.MethodPost,
			path:   "/api/ai/agent-chatbox/sessions/44/queue-items/reorder",
			payload: map[string]any{
				"queue_item_ids": []any{"queue-2", "queue-1"},
			},
			runtimePath:      "/v1/sessions/44/queue/reorder",
			runtimeRequestID: "runtime.queue.reorder",
			runtimeStatus:    http.StatusOK,
			runtimeBody:      `{"code":200,"request_id":"runtime.queue.reorder","runtime_run_id":"run-queue-1","status":"queued","data":{"queue_item_ids":["queue-2","queue-1"]}}`,
			invoke: func(h *AIAgentChatboxHandler, c *gin.Context) {
				h.ReorderQueueItems(c)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var callCount atomic.Int64
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				if r.URL.Path == "/v1/sessions/"+sessionID+"/query" {
					if callCount.Load() != 1 {
						t.Fatal("session query must be the first runtime call")
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":44,"source":"agent_chatbox","agent_profile_id":0,"agent_profile_version_id":0,"agent_profile_version_key":"","context_tags":[]}}`))
					return
				}
				if r.Method != http.MethodPost {
					t.Fatalf("runtime method=%s, want POST", r.Method)
				}
				if r.URL.Path != test.runtimePath {
					t.Fatalf("runtime path=%q, want %q", r.URL.Path, test.runtimePath)
				}
				if test.runtimeEscapedPath != "" && r.URL.EscapedPath() != test.runtimeEscapedPath {
					t.Fatalf("runtime escaped path=%q, want %q", r.URL.EscapedPath(), test.runtimeEscapedPath)
				}
				var envelope services.AIRuntimeEnvelope
				if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
					t.Fatalf("decode runtime envelope failed: %v", err)
				}
				if envelope.Actor.WorkspaceID != workspace.ID {
					t.Fatalf("actor workspace_id=%d, want governed workspace %d", envelope.Actor.WorkspaceID, workspace.ID)
				}
				if test.payload == nil {
					if len(envelope.Payload) != 0 {
						t.Fatalf("runtime payload=%v, want empty payload", envelope.Payload)
					}
				} else {
					wantPayload, err := json.Marshal(test.payload)
					if err != nil {
						t.Fatalf("marshal expected payload failed: %v", err)
					}
					gotPayload, err := json.Marshal(envelope.Payload)
					if err != nil {
						t.Fatalf("marshal runtime payload failed: %v", err)
					}
					if string(gotPayload) != string(wantPayload) {
						t.Fatalf("runtime payload=%s, want %s", gotPayload, wantPayload)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", test.runtimeRequestID)
				w.WriteHeader(test.runtimeStatus)
				_, _ = w.Write([]byte(test.runtimeBody))
			}))
			defer runtime.Close()

			h := &AIAgentChatboxHandler{
				DB: models.DB,
				RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
					BaseURL:       runtime.URL,
					InternalToken: "runtime-secret",
				}),
			}
			c, rec := newAIManagementTestContext(t, test.method, test.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, test.payload)
			c.Params = append(c.Params, gin.Param{Key: "id", Value: sessionID})
			if test.method == http.MethodDelete {
				c.Params = append(c.Params, gin.Param{Key: "queue_item_id", Value: queueItemID})
			}
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

			test.invoke(h, c)

			if rec.Code != test.runtimeStatus {
				t.Fatalf("status=%d, want %d body=%s", rec.Code, test.runtimeStatus, rec.Body.String())
			}
			if got := callCount.Load(); got < 2 {
				t.Fatalf("runtime call count=%d, want session query + queue request (2 calls)", got)
			}
			if rec.Body.String() != test.runtimeBody {
				t.Fatalf("body=%q, want unchanged runtime body %q", rec.Body.String(), test.runtimeBody)
			}
			if got := rec.Header().Get("X-Request-ID"); got != test.runtimeRequestID {
				t.Fatalf("X-Request-ID=%q, want unchanged runtime request id %q", got, test.runtimeRequestID)
			}
		})
	}
}

func TestAIAgentChatboxQueueRoutesUseChatboxAccessGate(t *testing.T) {
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

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/queue-items", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{
		"content": "must not reach runtime",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.EnqueueQueueItem(c)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRequest.Load() {
		t.Fatal("runtime should not be called when Chatbox access is denied")
	}
}

func TestAIAgentChatboxQueueRoutesRejectNonChatboxSource(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const sessionID = "44"
	const queueItemID = "queue/item 1"

	tests := []struct {
		name   string
		method string
		path   string
		invoke func(*AIAgentChatboxHandler, *gin.Context)
	}{
		{name: "enqueue rejects non-chatbox session", method: http.MethodPost, path: "/api/ai/agent-chatbox/sessions/44/queue-items", invoke: func(h *AIAgentChatboxHandler, c *gin.Context) { h.EnqueueQueueItem(c) }},
		{name: "list rejects non-chatbox session", method: http.MethodGet, path: "/api/ai/agent-chatbox/sessions/44/queue-items", invoke: func(h *AIAgentChatboxHandler, c *gin.Context) { h.ListQueueItems(c) }},
		{name: "cancel rejects non-chatbox session", method: http.MethodDelete, path: "/api/ai/agent-chatbox/sessions/44/queue-items/queue%2Fitem%201", invoke: func(h *AIAgentChatboxHandler, c *gin.Context) { h.CancelQueueItem(c) }},
		{name: "reorder rejects non-chatbox session", method: http.MethodPost, path: "/api/ai/agent-chatbox/sessions/44/queue-items/reorder", invoke: func(h *AIAgentChatboxHandler, c *gin.Context) { h.ReorderQueueItems(c) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var sawQueueRequest atomic.Bool
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/sessions/"+sessionID+"/query" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"code":200,"data":{"id":44,"source":"page_ai_assistant","agent_profile_id":0,"agent_profile_version_id":0,"agent_profile_version_key":"","context_tags":[]}}`))
					return
				}
				sawQueueRequest.Store(true)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer runtime.Close()

			h := &AIAgentChatboxHandler{
				DB: models.DB,
				RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
					BaseURL:       runtime.URL,
					InternalToken: "runtime-secret",
				}),
			}
			c, rec := newAIManagementTestContext(t, test.method, test.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{})
			c.Params = append(c.Params, gin.Param{Key: "id", Value: sessionID})
			if test.method == http.MethodDelete {
				c.Params = append(c.Params, gin.Param{Key: "queue_item_id", Value: queueItemID})
			}
			c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

			test.invoke(h, c)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response failed: %v", err)
			}
			if got := toString(body["message"]); got != "该会话不是 Agent Chatbox 会话" {
				t.Fatalf("message=%q, want 该会话不是 Agent Chatbox 会话", got)
			}
			if sawQueueRequest.Load() {
				t.Fatal("queue request should not reach runtime when session source is not agent_chatbox")
			}
		})
	}
}

func TestAIAgentChatboxStreamEntryEnforcesServerInputLimit(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var streamed atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/sessions/44/query":
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":44,"source":"agent_chatbox","agent_profile_id":91,"agent_profile_version_id":0,"agent_profile_version_key":"latest","context_tags":[]}}`))
		case "/v1/agent-profiles/91/query":
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":91,"status":"draft","context_tags":[],"inference":{"max_tokens":6}}}`))
		case "/v1/sessions/44/entries/stream":
			streamed.Store(true)
			_, _ = w.Write([]byte(`event: done\ndata: {}\n\n`))
		default:
			t.Fatalf("unexpected runtime path=%s", r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/entries/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"content":         "one two three four",
		"client_entry_id": "too-long",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateEntryStream(c)

	assertAIRuntimeCorrelatedLocalError(t, rec, http.StatusBadRequest, "输入超过 Agent Profile Max Tokens / 2 限制")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok || numericID(data["limit_tokens"]) != 3 || numericID(data["estimated_tokens"]) <= 3 {
		t.Fatalf("unexpected input limit data: %#v", body["data"])
	}
	if streamed.Load() {
		t.Fatal("runtime stream should not be called when server hard limit rejects")
	}
}

func TestAIAgentChatboxStreamEntryEmptyContentReturnsCorrelatedError(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest.Store(true)
		t.Fatalf("runtime should not be called for empty content: %s %s", r.Method, r.URL.Path)
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/entries/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"content": "   ",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateEntryStream(c)

	assertAIRuntimeCorrelatedLocalError(t, rec, http.StatusBadRequest, "content is required")
	if sawRequest.Load() {
		t.Fatal("runtime should not be called for empty content")
	}
}

func TestAIAgentChatboxStreamsEntriesAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	const requestID = "client.chatbox.stream"
	const runtimeRequestID = "runtime.chatbox.stream"

	var sawStream atomic.Bool
	var streamEnvelope services.AIRuntimeEnvelope
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/44/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":44,"source":"agent_chatbox","agent_profile_id":91,"agent_profile_version_id":0,"agent_profile_version_key":"latest","context_tags":[]}}`))
		case "/v1/agent-profiles/91/query":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":91,"status":"draft","context_tags":[],"inference":{"max_tokens":100}}}`))
		case "/v1/sessions/44/entries/stream":
			sawStream.Store(true)
			if err := json.NewDecoder(r.Body).Decode(&streamEnvelope); err != nil {
				t.Fatalf("decode runtime stream envelope: %v", err)
			}
			if streamEnvelope.RequestID != requestID || r.Header.Get("X-Request-ID") != requestID {
				t.Fatalf("stream correlation envelope=%q header=%q", streamEnvelope.RequestID, r.Header.Get("X-Request-ID"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Request-ID", runtimeRequestID)
			w.Header().Set("Retry-After", "5")
			w.Header().Set("RateLimit-Remaining", "12")
			w.Header().Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
			w.Header().Set("Set-Cookie", "runtime_secret=must-not-leak")
			w.Header().Set("X-Internal-Token", "must-not-leak")
			_, _ = fmt.Fprint(w, "event: answer_delta\ndata: {\"delta\":\"answer\"}\n\n")
			_, _ = fmt.Fprint(w, "event: assistant_entry\ndata: {\"entry\":{\"runtime_run_id\":\"run-1\"},\"run\":{\"agent_profile_snapshot_hash\":\"sha256:test\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: done\ndata: {\"timings\":{\"total_ms\":12}}\n\n")
		default:
			t.Fatalf("unexpected runtime path=%s", r.URL.Path)
		}
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/entries/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"content":         "hello",
		"client_entry_id": "stream-ok",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)
	c.Request.Header.Set("X-Request-ID", requestID)

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
	if got := rec.Header().Get("X-Request-ID"); got != runtimeRequestID {
		t.Fatalf("X-Request-ID=%q, want %q", got, runtimeRequestID)
	}
	for header, expected := range map[string]string{
		"Retry-After":         "5",
		"RateLimit-Remaining": "12",
		"Traceparent":         "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	} {
		if got := rec.Header().Get(header); got != expected {
			t.Fatalf("%s=%q, want %q", header, got, expected)
		}
	}
	for _, header := range []string{"Set-Cookie", "X-Internal-Token"} {
		if got := rec.Header().Values(header); len(got) != 0 {
			t.Fatalf("unsafe upstream header %s leaked: %v", header, got)
		}
	}
	if !sawStream.Load() || !strings.Contains(rec.Body.String(), "event: answer_delta") {
		t.Fatalf("expected stream body, got %s", rec.Body.String())
	}
	if got := toString(streamEnvelope.Payload["runtime_engine"]); got != "pi" {
		t.Fatalf("runtime_engine=%q, want pi", got)
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.message.sent").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxActionDecisionStreamsContinuationAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	actionID := "a_w3_000001_000001_000009"

	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/actions/"+actionID+"/decision/stream" {
			t.Fatalf("runtime path=%s, want /v1/actions/%s/decision/stream", r.URL.Path, actionID)
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
		_, _ = fmt.Fprintf(w, "event: tool.executed\ndata: {\"action_id\":%q}\n\n", actionID)
		_, _ = fmt.Fprint(w, "event: answer_delta\ndata: {\"delta\":\"done\"}\n\n")
		_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/actions/"+actionID+"/decision/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_session",
		"client_decision_id": actionID + "-approve",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: actionID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DecideActionStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("cache-control=%q, want no-cache, no-transform", got)
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("x-accel-buffering=%q, want no", got)
	}
	if !sawDecision.Load() || !strings.Contains(rec.Body.String(), "event: tool.executed") {
		t.Fatalf("expected action decision stream, got %s", rec.Body.String())
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.action.decided").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxForwardsPiApprovalDecision(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"

	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/pi-approval/decision" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/pi-approval/decision", r.URL.Path, runtimeRunID)
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
		_, _ = w.Write([]byte(`{"code":200,"data":{"run":{"runtime_run_id":"` + runtimeRunID + `","status":"completed"},"assistant_entry":{"runtime_run_id":"` + runtimeRunID + `"}}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/runs/"+runtimeRunID+"/pi-approval/decision", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_once",
		"client_decision_id": "approve-pi-tool",
	})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DecidePiApproval(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawDecision.Load() || !strings.Contains(rec.Body.String(), "completed") {
		t.Fatalf("expected pi approval response, got %s", rec.Body.String())
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.pi_approval.decided").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxForwardsPiApprovalDecisionStream(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"

	var sawDecision atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/pi-approval/decision/stream" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/pi-approval/decision/stream", r.URL.Path, runtimeRunID)
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
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: runtime_event\ndata: {\"event\":{\"type\":\"permission.resolved\"}}\n\n"))
		_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/runs/"+runtimeRunID+"/pi-approval/decision/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"decision":           "approve_once",
		"client_decision_id": "approve-pi-tool-stream",
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
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.pi_approval.decided").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxForwardsRuntimeArtifact(t *testing.T) {
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
		var envelope services.AIRuntimeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode runtime envelope failed: %v", err)
		}
		if envelope.Payload["parent_runtime_run_id"] != parentRunID || envelope.Payload["parent_action_id"] != parentActionID || envelope.Payload["child_run_link_id"] != childRunLinkID {
			t.Fatalf("missing parent artifact context: %#v", envelope.Payload)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"artifact_id":"` + artifactID + `"}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/agent-chatbox/artifacts/"+artifactID, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{})
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

func TestAIAgentChatboxForwardsScopedRunEventsForChildThread(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	childRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000002_000001"
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
			t.Fatalf("missing parent event context: %#v", envelope.Payload)
		}
		sawRequest.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"runtime_run_id":"` + childRunID + `","events":[]}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodGet, "/api/ai/agent-chatbox/runs/"+childRunID+"/events", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{})
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

func TestAIAgentChatboxRunEventValidationReturnsCorrelatedErrors(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var requestCount atomic.Int64
	runtime := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
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
		{name: "list missing run id", method: http.MethodGet, path: "/api/ai/agent-chatbox/runs//events", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.ListRunEvents},
		{name: "list malformed run id", method: http.MethodGet, path: "/api/ai/agent-chatbox/runs/r_missing_p113/events", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.ListRunEvents},
		{name: "stream missing run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs//events/stream", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.StreamRunEvents},
		{name: "stream malformed run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs/r_missing_p113/events/stream", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.StreamRunEvents},
		{name: "approval missing run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs//pi-approval/decision", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.DecidePiApproval},
		{name: "approval malformed run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs/r_missing_p113/pi-approval/decision", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.DecidePiApproval},
		{name: "approval stream missing run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs//pi-approval/decision/stream", paramKey: "runtime_run_id", status: http.StatusBadRequest, message: "runtime_run_id is required", invoke: h.DecidePiApprovalStream},
		{name: "approval stream malformed run id", method: http.MethodPost, path: "/api/ai/agent-chatbox/runs/r_missing_p113/pi-approval/decision/stream", paramKey: "runtime_run_id", paramValue: "r_missing_p113", status: http.StatusNotFound, message: "Runtime run not found", invoke: h.DecidePiApprovalStream},
		{name: "artifact missing id", method: http.MethodGet, path: "/api/ai/agent-chatbox/artifacts/", paramKey: "artifact_id", status: http.StatusBadRequest, message: "artifact_id is required", invoke: h.GetArtifact},
		{name: "artifact malformed id", method: http.MethodGet, path: "/api/ai/agent-chatbox/artifacts/art_missing_p113", paramKey: "artifact_id", paramValue: "art_missing_p113", status: http.StatusNotFound, message: "Runtime artifact not found", invoke: h.GetArtifact},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := newAIManagementTestContext(t, test.method, test.path, owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{})
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

func TestAIAgentChatboxStreamsRunEventsWithResumeCursor(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)
	runtimeRunID := "r_w" + strconv.FormatUint(workspace.ID, 36) + "_000001_000001"
	afterEventID := "s_w1_000001:7"

	var sawRequest atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs/"+runtimeRunID+"/events/stream" {
			t.Fatalf("runtime path=%s, want /v1/runs/%s/events/stream", r.URL.Path, runtimeRunID)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("runtime method=%s, want POST", r.Method)
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
		w.Header().Set("X-Accel-Buffering", "no")
		_, _ = w.Write([]byte("id: s_w1_000001:8\nevent: runtime_event\ndata: {\"event\":{\"event_id\":\"s_w1_000001:8\"}}\n\n"))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}
	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/runs/"+runtimeRunID+"/events/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"after_event_id": afterEventID,
	})
	c.Params = append(c.Params, gin.Param{Key: "runtime_run_id", Value: runtimeRunID})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.StreamRunEvents(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawRequest.Load() {
		t.Fatal("expected request to reach runtime")
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("content-type=%q, want text/event-stream", contentType)
	}
	if !strings.Contains(rec.Body.String(), "id: s_w1_000001:8") {
		t.Fatalf("stream body=%q, want persisted event id", rec.Body.String())
	}
}

func TestAIAgentChatboxCancelSessionForwardsAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawCancel atomic.Bool
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/44/cancel" {
			t.Fatalf("runtime path=%s, want /v1/sessions/44/cancel", r.URL.Path)
		}
		sawCancel.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"runtime_run_id":"run-cancel","status":"cancelled"}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/cancel", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"runtime_run_id": "run-cancel",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CancelSession(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawCancel.Load() {
		t.Fatal("expected cancel request to reach runtime")
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.generation.cancelled").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxUpdateSessionModelForwardsRuntimeModelOverrideAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawUpdate atomic.Bool
	var updateEnvelope services.AIRuntimeEnvelope
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("runtime method=%s, want PUT", r.Method)
		}
		if r.URL.Path != "/v1/sessions/44/model" {
			t.Fatalf("runtime path=%s, want /v1/sessions/44/model", r.URL.Path)
		}
		sawUpdate.Store(true)
		if err := json.NewDecoder(r.Body).Decode(&updateEnvelope); err != nil {
			t.Fatalf("decode runtime update envelope: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"session":{"id":44,"model_override":{"model":{"provider_model_key":"claude-sonnet-4-5"}}},"model_config":{"provider_id":"anthropic","id":"claude-sonnet-4-5","thinking_level":"high"}}}`))
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPut, "/api/ai/agent-chatbox/sessions/44/model", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"provider": map[string]any{"provider_id": "anthropic"},
		"model":    map[string]any{"provider_model_key": "claude-sonnet-4-5"},
		"inference": map[string]any{
			"thinking_level": "high",
		},
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateSessionModel(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !sawUpdate.Load() {
		t.Fatal("expected model update request to reach runtime")
	}
	if got := toString(updateEnvelope.Payload["model"].(map[string]any)["provider_model_key"]); got != "claude-sonnet-4-5" {
		t.Fatalf("provider_model_key=%q, want claude-sonnet-4-5", got)
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.model.switched").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}

func TestAIAgentChatboxContinueSessionStreamsAndAudits(t *testing.T) {
	_ = openHandlerTestDB(t)
	owner, workspace := seedAIAgentStoreOwner(t)

	var sawContinue atomic.Bool
	var continueEnvelope services.AIRuntimeEnvelope
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions/44/continue/stream" {
			t.Fatalf("runtime path=%s, want /v1/sessions/44/continue/stream", r.URL.Path)
		}
		sawContinue.Store(true)
		if err := json.NewDecoder(r.Body).Decode(&continueEnvelope); err != nil {
			t.Fatalf("decode runtime continue envelope: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: answer_delta\ndata: {\"delta\":\"continued\"}\n\n")
		_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer runtime.Close()

	h := &AIAgentChatboxHandler{
		DB: models.DB,
		RuntimeClient: services.NewAIRuntimeClient(services.AIRuntimeClientOptions{
			BaseURL:       runtime.URL,
			InternalToken: "runtime-secret",
		}),
	}

	c, rec := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agent-chatbox/sessions/44/continue/stream", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"content": "继续",
	})
	c.Params = append(c.Params, gin.Param{Key: "id", Value: "44"})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ContinueSessionStream(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("cache-control=%q, want no-cache, no-transform", got)
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("x-accel-buffering=%q, want no", got)
	}
	if !sawContinue.Load() || !strings.Contains(rec.Body.String(), "event: answer_delta") {
		t.Fatalf("expected continue stream, got %s", rec.Body.String())
	}
	if got := toString(continueEnvelope.Payload["runtime_engine"]); got != "pi" {
		t.Fatalf("runtime_engine=%q, want pi", got)
	}
	var auditCount int64
	if err := models.DB.Model(&models.AuditLog{}).Where("action = ?", "agent_chatbox.generation.continued").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d, want 1", auditCount)
	}
}
