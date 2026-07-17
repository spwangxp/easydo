package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func openAIManagementTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openHandlerTestDB(t)
	if err := db.AutoMigrate(
		&models.AIProvider{},
		&models.AIModelBinding{},
	); err != nil {
		t.Fatalf("auto migrate ai management tables failed: %v", err)
	}
	return db
}

func newAIManagementTestContext(t *testing.T, method, path string, actorID uint64, actorRole string, workspaceID uint64, workspaceRole, workspaceKind string, payload map[string]any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", actorID)
	c.Set("role", actorRole)
	c.Set("workspace_id", workspaceID)
	c.Set("workspace_role", workspaceRole)
	c.Set("workspace_kind", workspaceKind)
	return c, w
}

func seedAIManagementWorkspaceMember(t *testing.T, db *gorm.DB, username string, workspaceRole string) (models.User, models.Workspace) {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: username + "-space", Slug: username + "-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: workspaceRole, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace membership failed: %v", err)
	}
	return user, workspace
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestAIProviderMutationAllowsDeveloperWorkspaceMember(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-developer", models.WorkspaceRoleDeveloper)

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":          "developer-provider",
		"provider_type": "openai",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProvider(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var provider models.AIProvider
	if err := db.Where("workspace_id = ? AND name = ?", workspace.ID, "developer-provider").First(&provider).Error; err != nil {
		t.Fatalf("provider should be created in current workspace: %v", err)
	}
	if provider.CreatedBy != developer.ID {
		t.Fatalf("provider created_by=%d, want=%d", provider.CreatedBy, developer.ID)
	}
	if provider.CredentialID != nil {
		t.Fatalf("provider credential_id=%v, want nil when request omits credential_id", *provider.CredentialID)
	}
}

func TestAIProviderTestConnectionFetchesModelsWithCredential(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-test-connection", models.WorkspaceRoleDeveloper)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, developer.ID, models.TypeToken, map[string]interface{}{"token": "or-test-token"})

	var seenAuth string
	var seenReferer string
	var seenAcceptEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected provider models path: %s", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		seenReferer = r.Header.Get("HTTP-Referer")
		seenAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-4.1","name":"GPT-4.1","context_length":1048576,"top_provider":{"max_completion_tokens":32768},"supported_parameters":["tools","response_format"]}]}`))
	}))
	defer upstream.Close()

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/test-connection", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":          "OpenRouter",
		"provider_type": "openrouter",
		"base_url":      upstream.URL + "/v1",
		"credential_id": credential.ID,
		"headers_json": map[string]any{
			"HTTP-Referer": "https://easydo.local",
		},
		"settings_json": map[string]any{
			"models_endpoint": "/models",
		},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.TestConnection(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if seenAuth != "Bearer or-test-token" {
		t.Fatalf("provider Authorization header=%q, want bearer credential", seenAuth)
	}
	if seenReferer != "https://easydo.local" {
		t.Fatalf("provider HTTP-Referer=%q, want configured header", seenReferer)
	}
	if seenAcceptEncoding != "identity" {
		t.Fatalf("provider Accept-Encoding=%q, want identity", seenAcceptEncoding)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	data := body["data"].(map[string]any)
	if data["ok"] != true {
		t.Fatalf("expected ok=true, got=%v body=%s", data["ok"], w.Body.String())
	}
	if data["models_count"].(float64) != 1 {
		t.Fatalf("expected models_count=1, got=%v body=%s", data["models_count"], w.Body.String())
	}
}

func TestAIProviderDiscoverModelsNormalizesOpenRouterPayload(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-discover-models", models.WorkspaceRoleDeveloper)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, developer.ID, models.TypeToken, map[string]interface{}{"token": "or-discover-token"})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer or-discover-token" {
			t.Fatalf("provider Authorization header=%q, want bearer credential", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"google/gemini-2.5-pro","name":"Google: Gemini 2.5 Pro","context_length":1000000,"architecture":{"modality":"text+image->text"},"pricing":{"prompt":"0.00000125","completion":"0.00001"},"top_provider":{"max_completion_tokens":65536},"supported_parameters":["tools","tool_choice","response_format","reasoning"]}]}`))
	}))
	defer upstream.Close()

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/discover-models", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":          "OpenRouter",
		"provider_type": "openrouter",
		"base_url":      upstream.URL,
		"credential_id": credential.ID,
		"settings_json": map[string]any{
			"models_endpoint": "/models",
		},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DiscoverModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Models []struct {
				ProviderModelKey string   `json:"provider_model_key"`
				DisplayName      string   `json:"provider_display_name"`
				ModelName        string   `json:"model_name"`
				ContextWindow    int      `json:"context_window"`
				MaxOutputTokens  int      `json:"max_output_tokens"`
				Modalities       []string `json:"modalities"`
				Capabilities     []string `json:"capabilities"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(body.Data.Models) != 1 {
		t.Fatalf("expected one discovered model, got=%d body=%s", len(body.Data.Models), w.Body.String())
	}
	model := body.Data.Models[0]
	if model.ProviderModelKey != "google/gemini-2.5-pro" || model.DisplayName != "Google: Gemini 2.5 Pro" {
		t.Fatalf("unexpected discovered model identity: %+v", model)
	}
	if model.ContextWindow != 1000000 || model.MaxOutputTokens != 65536 {
		t.Fatalf("unexpected token limits: %+v", model)
	}
	if !containsString(model.Modalities, "vision") || !containsString(model.Capabilities, "tool") || !containsString(model.Capabilities, "json") {
		t.Fatalf("expected normalized modality/capability hints, got modalities=%v capabilities=%v", model.Modalities, model.Capabilities)
	}
}

func TestAIProviderDiscoverModelsNormalizesVLLMMaxModelLen(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-discover-vllm", models.WorkspaceRoleDeveloper)
	credential := seedResourceVerificationCredential(t, db, workspace.ID, developer.ID, models.TypeToken, map[string]interface{}{"token": "vllm-discover-token"})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vllm-discover-token" {
			t.Fatalf("provider Authorization header=%q, want bearer credential", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3.6-27b-local","object":"model","created":1783562803,"owned_by":"vllm","root":"/models/qwen3.6","parent":null,"max_model_len":262144}]}`))
	}))
	defer upstream.Close()

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/discover-models", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":          "Local vLLM",
		"provider_type": "openai-compatible",
		"base_url":      upstream.URL,
		"credential_id": credential.ID,
		"settings_json": map[string]any{
			"models_endpoint": "/models",
		},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.DiscoverModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Models []struct {
				ProviderModelKey string `json:"provider_model_key"`
				ContextWindow    int64  `json:"context_window"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(body.Data.Models) != 1 {
		t.Fatalf("expected one discovered model, got=%d body=%s", len(body.Data.Models), w.Body.String())
	}
	model := body.Data.Models[0]
	if model.ProviderModelKey != "qwen3.6-27b-local" {
		t.Fatalf("unexpected provider model key=%q", model.ProviderModelKey)
	}
	if model.ContextWindow != 262144 {
		t.Fatalf("context_window=%d, want 262144", model.ContextWindow)
	}
}

func TestAIProviderMutationBlocksViewerWorkspaceMember(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	viewer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-viewer", models.WorkspaceRoleViewer)

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers", viewer.ID, viewer.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{
		"name":          "viewer-provider",
		"provider_type": "openai",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProvider(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.AIProvider{}).Where("workspace_id = ?", workspace.ID).Count(&count).Error; err != nil {
		t.Fatalf("count providers failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("provider count=%d, want=0", count)
	}
}

func TestAIProviderUpdateAllowsDeveloperWorkspaceMember(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-provider-update-developer", models.WorkspaceRoleDeveloper)
	provider := models.AIProvider{WorkspaceID: workspace.ID, Name: "before-update", ProviderType: "openai", Status: models.AIProviderStatusActive, CreatedBy: developer.ID}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPut, "/api/store/ai-providers/1", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"name":          "after-update",
		"provider_type": "openrouter",
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.UpdateProvider(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var updated models.AIProvider
	if err := db.First(&updated, provider.ID).Error; err != nil {
		t.Fatalf("reload provider failed: %v", err)
	}
	if updated.Name != "after-update" || updated.ProviderType != "openrouter" {
		t.Fatalf("provider update not persisted: name=%s type=%s", updated.Name, updated.ProviderType)
	}
}

func TestAIProviderBindingMutationFollowsProviderWritePermission(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-binding-developer", models.WorkspaceRoleDeveloper)
	model := models.AIModelCatalog{Name: "Qwen", Source: "manual", SourceModelID: "qwen", ImportedBy: developer.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create model failed: %v", err)
	}
	provider := models.AIProvider{WorkspaceID: workspace.ID, Name: "binding-provider", ProviderType: "openai", Status: models.AIProviderStatusActive, CreatedBy: developer.ID}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers/1/bindings", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"model_id":               model.ID,
		"provider_model_key":     "qwen",
		"context_window_tokens":  131072,
		"max_output_tokens":      8192,
		"supports_tool_use":      true,
		"supports_streaming":     true,
		"capability_source":      "manual_override",
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(provider.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateBinding(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var binding models.AIModelBinding
	if err := db.Where("workspace_id = ? AND provider_id = ? AND model_id = ?", workspace.ID, provider.ID, model.ID).First(&binding).Error; err != nil {
		t.Fatalf("binding should be created in current workspace: %v", err)
	}
	if binding.CreatedBy != developer.ID {
		t.Fatalf("binding created_by=%d, want=%d", binding.CreatedBy, developer.ID)
	}
	if binding.ContextWindowTokens == nil || *binding.ContextWindowTokens != 131072 {
		t.Fatalf("binding context_window_tokens=%v, want 131072", binding.ContextWindowTokens)
	}
	if binding.CapabilitySource != "manual_override" || binding.CapabilitySnapshotHash == "" {
		t.Fatalf("binding capability snapshot incomplete: source=%s hash=%s", binding.CapabilitySource, binding.CapabilitySnapshotHash)
	}
}

func TestAIModelImportAllowsDeveloperForWorkspaceSources(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIModelCatalogHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-model-import-developer", models.WorkspaceRoleDeveloper)
	viewer := models.User{Username: "ai-model-import-viewer", Role: "user", Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: viewer.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: developer.ID}).Error; err != nil {
		t.Fatalf("create viewer membership failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-models/import", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"source":          "provider-discovered",
		"source_model_id": "openrouter/qwen3-32b",
		"name":            "Qwen3 32B",
		"display_name":    "Qwen3 32B",
		"metadata": map[string]any{
			"provider_model_key": "qwen/qwen3-32b",
			"provider_type":      "openrouter",
		},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ImportModel(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected developer provider-discovered import success, got=%d body=%s", w.Code, w.Body.String())
	}
	var imported models.AIModelCatalog
	if err := db.Where("source = ? AND source_model_id = ?", "provider-discovered", "openrouter/qwen3-32b").First(&imported).Error; err != nil {
		t.Fatalf("provider-discovered model should be imported: %v", err)
	}
	if imported.ImportedBy != developer.ID {
		t.Fatalf("imported_by=%d, want=%d", imported.ImportedBy, developer.ID)
	}

	viewerContext, viewerResponse := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-models/import", viewer.ID, viewer.Role, workspace.ID, models.WorkspaceRoleViewer, models.WorkspaceKindNormal, map[string]any{
		"source":          "provider-discovered",
		"source_model_id": "openrouter/gpt-4.1",
		"name":            "GPT-4.1",
	})
	viewerContext.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ImportModel(viewerContext)

	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("expected viewer provider-discovered import forbidden, got=%d body=%s", viewerResponse.Code, viewerResponse.Body.String())
	}
}

func TestAIModelImportKeepsPublicCatalogAdminOnly(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIModelCatalogHandler{DB: db}

	developer, workspace := seedAIManagementWorkspaceMember(t, db, "ai-public-import-developer", models.WorkspaceRoleDeveloper)

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-models/import", developer.ID, developer.Role, workspace.ID, models.WorkspaceRoleDeveloper, models.WorkspaceKindNormal, map[string]any{
		"source":          "huggingface",
		"source_model_id": "Qwen/Qwen2.5-7B-Instruct",
		"name":            "Qwen2.5-7B-Instruct",
		"metadata": map[string]any{
			"id": "Qwen/Qwen2.5-7B-Instruct",
		},
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.ImportModel(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected developer public catalog import forbidden, got=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.AIModelCatalog{}).Where("source = ?", "huggingface").Count(&count).Error; err != nil {
		t.Fatalf("count model catalogs failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("public model count=%d, want=0", count)
	}
}
