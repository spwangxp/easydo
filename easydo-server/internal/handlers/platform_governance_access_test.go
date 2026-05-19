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
		&models.AIAgent{},
		&models.AIRuntimeProfile{},
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

func TestPlatformAIProviderMutationRequiresAdminWorkspace(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	owner := models.User{Username: "platform-ai-owner", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "normal-ai-space", Slug: "normal-ai-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace membership failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers", owner.ID, owner.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":          "blocked-provider",
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

func TestPlatformAIProviderMutationRevalidatesWorkspaceKindFromDB(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIProviderHandler{DB: db}

	admin := models.User{Username: "platform-ai-kind-check", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "spoofed-platform-ai-space", Slug: "spoofed-platform-ai-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/store/ai-providers", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"name":          "spoofed-provider",
		"provider_type": "openai",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateProvider(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with spoofed workspace kind, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestWorkspaceAgentMutationRequiresNormalWorkspaceGovernance(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIAgentHandler{DB: db}

	admin := models.User{Username: "workspace-agent-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "platform-governance-ai", Slug: "platform-governance-ai", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agents", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"name":     "blocked-agent",
		"scenario": "chat",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateAgent(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.AIAgent{}).Where("workspace_id = ?", workspace.ID).Count(&count).Error; err != nil {
		t.Fatalf("count agents failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("agent count=%d, want=0", count)
	}
}

func TestWorkspaceAgentMutationRevalidatesWorkspaceRoleFromDB(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIAgentHandler{DB: db}

	maintainer := models.User{Username: "workspace-agent-role-check", Role: "user", Status: "active"}
	if err := db.Create(&maintainer).Error; err != nil {
		t.Fatalf("create maintainer failed: %v", err)
	}
	workspace := models.Workspace{Name: "normal-governance-ai", Slug: "normal-governance-ai", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: maintainer.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: maintainer.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: maintainer.ID}).Error; err != nil {
		t.Fatalf("create workspace membership failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agents", maintainer.ID, maintainer.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindNormal, map[string]any{
		"name":     "spoofed-agent",
		"scenario": "chat",
	})
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateAgent(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with spoofed workspace role, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRuntimeProfileMutationRequiresNormalWorkspaceGovernance(t *testing.T) {
	db := openAIManagementTestDB(t)
	h := &AIAgentHandler{DB: db}

	admin := models.User{Username: "runtime-profile-admin", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "platform-runtime-space", Slug: "platform-runtime-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	model := models.AIModelCatalog{Name: "runtime-model", DisplayName: "Runtime Model", Source: "seed", SourceModelID: "runtime-model", ImportedBy: admin.ID}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create model failed: %v", err)
	}
	agent := models.AIAgent{WorkspaceID: workspace.ID, Name: "seed-agent", Scenario: "chat", ScopeType: models.AgentScopeWorkspace, Status: models.AIAgentStatusDraft, CreatedBy: admin.ID}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	c, w := newAIManagementTestContext(t, http.MethodPost, "/api/ai/agents/1/runtime-profiles", admin.ID, admin.Role, workspace.ID, models.WorkspaceRoleOwner, models.WorkspaceKindAdmin, map[string]any{
		"name":     "blocked-profile",
		"model_id": model.ID,
	})
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}
	c.Request.URL.RawQuery = "workspace_id=" + strconv.FormatUint(workspace.ID, 10)

	h.CreateRuntimeProfile(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.AIRuntimeProfile{}).Where("workspace_id = ?", workspace.ID).Count(&count).Error; err != nil {
		t.Fatalf("count runtime profiles failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("runtime profile count=%d, want=0", count)
	}
}
