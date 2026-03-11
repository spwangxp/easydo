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
)

func performScopedUpdateAgentRequest(t *testing.T, h *AgentHandler, agentID uint64, role string, workspaceID uint64, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/agents/"+strconv.FormatUint(agentID, 10), bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agentID, 10)}}
	c.Set("user_id", uint64(1))
	c.Set("role", role)
	if workspaceID > 0 {
		c.Set("workspace_id", workspaceID)
	}
	h.UpdateAgent(c)
	return w
}

func TestRegisterAgent_ReregisterDoesNotMutateScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	targetWorkspace := models.Workspace{Name: "target-workspace", Slug: "target-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 88}
	if err := db.Create(&targetWorkspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	existing := models.Agent{
		Name:               "platform-agent",
		Host:               "host-a",
		Port:               8080,
		Token:              "approved-token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopePlatform,
		WorkspaceID:        0,
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	body, err := json.Marshal(map[string]interface{}{
		"name":         "platform-agent-updated",
		"host":         "host-b",
		"port":         9090,
		"token":        existing.Token,
		"workspace_id": targetWorkspace.ID,
	})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/agents/register", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.RegisterAgent(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Agent
	if err := db.First(&got, existing.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.ScopeType != models.AgentScopePlatform {
		t.Fatalf("scope_type=%s, want=%s", got.ScopeType, models.AgentScopePlatform)
	}
	if got.WorkspaceID != 0 {
		t.Fatalf("workspace_id=%d, want=0", got.WorkspaceID)
	}
	if got.Host != "host-b" || got.Port != 9090 || got.Name != "platform-agent-updated" {
		t.Fatalf("expected mutable runtime fields to update, got=%+v", got)
	}
}

func TestUpdateAgent_AdminCanMoveAgentAcrossScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	workspace := models.Workspace{Name: "workspace-scope", Slug: "workspace-scope", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 77}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{
		Name:               "platform-agent",
		Host:               "host",
		Port:               8080,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopePlatform,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	w := performScopedUpdateAgentRequest(t, h, agent.ID, "admin", 0, map[string]interface{}{
		"scope_type":   models.AgentScopeWorkspace,
		"workspace_id": workspace.ID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.ScopeType != models.AgentScopeWorkspace || got.WorkspaceID != workspace.ID {
		t.Fatalf("agent not moved to workspace scope: %+v", got)
	}

	w = performScopedUpdateAgentRequest(t, h, agent.ID, "admin", 0, map[string]interface{}{
		"scope_type": models.AgentScopePlatform,
		"name":       "platform-again",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.ScopeType != models.AgentScopePlatform || got.WorkspaceID != 0 {
		t.Fatalf("agent not moved back to platform scope: %+v", got)
	}
}

func TestUpdateAgent_NonAdminCannotChangeScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	workspaceA := models.Workspace{Name: "workspace-a", Slug: "workspace-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 90}
	workspaceB := models.Workspace{Name: "workspace-b", Slug: "workspace-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 91}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspace A failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspace B failed: %v", err)
	}
	agent := models.Agent{
		Name:               "workspace-agent",
		Host:               "host",
		Port:               8080,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopeWorkspace,
		WorkspaceID:        workspaceA.ID,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	w := performScopedUpdateAgentRequest(t, h, agent.ID, "user", workspaceA.ID, map[string]interface{}{
		"scope_type":   models.AgentScopeWorkspace,
		"workspace_id": workspaceB.ID,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got status=%d body=%s", w.Code, w.Body.String())
	}
	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.WorkspaceID != workspaceA.ID || got.ScopeType != models.AgentScopeWorkspace {
		t.Fatalf("non-admin unexpectedly changed agent scope: %+v", got)
	}
}
