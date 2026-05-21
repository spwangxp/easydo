package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedWorkspace(t *testing.T, db *gorm.DB, slug, kind string) models.Workspace {
	t.Helper()
	workspace := models.Workspace{
		Name:       slug,
		Slug:       slug,
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       kind,
		CreatedBy:  1,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	return workspace
}

func seedAgentRecord(t *testing.T, db *gorm.DB, scopeType string, workspaceID uint64, name, registrationStatus string) models.Agent {
	t.Helper()
	agent := models.Agent{
		Name:               name,
		Host:               name + ".example.internal",
		Port:               8080,
		Token:              name + "-token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: registrationStatus,
		ScopeType:          scopeType,
		WorkspaceID:        workspaceID,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	return agent
}

func seedApprovedPlatformAgent(t *testing.T, db *gorm.DB, name string) models.Agent {
	t.Helper()
	return seedAgentRecord(t, db, models.AgentScopePlatform, 0, name, models.AgentRegistrationStatusApproved)
}

func seedApprovedWorkspaceAgent(t *testing.T, db *gorm.DB, workspaceID uint64, name string) models.Agent {
	t.Helper()
	return seedAgentRecord(t, db, models.AgentScopeWorkspace, workspaceID, name, models.AgentRegistrationStatusApproved)
}

func seedPendingPlatformAgent(t *testing.T, db *gorm.DB, name string) models.Agent {
	t.Helper()
	return seedAgentRecord(t, db, models.AgentScopePlatform, 0, name, models.AgentRegistrationStatusPending)
}

func seedPendingWorkspaceAgent(t *testing.T, db *gorm.DB, workspaceID uint64, name string) models.Agent {
	t.Helper()
	return seedAgentRecord(t, db, models.AgentScopeWorkspace, workspaceID, name, models.AgentRegistrationStatusPending)
}

func performAgentListRequest(t *testing.T, h *AgentHandler, role string, workspaceID uint64, workspaceKind string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agents?page=1&page_size=50", nil)
	c.Set("user_id", uint64(1))
	c.Set("role", role)
	c.Set("workspace_id", workspaceID)
	c.Set("workspace_kind", workspaceKind)
	c.Set("workspace_role", models.WorkspaceRoleOwner)
	c.Request.URL.RawQuery = "page=1&page_size=50"
	 h.GetAgentList(c)
	return w
}

func performPendingAgentListRequest(t *testing.T, h *AgentHandler, role string, workspaceID uint64, workspaceKind string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agents/pending?page=1&page_size=50", nil)
	c.Set("user_id", uint64(1))
	c.Set("role", role)
	c.Set("workspace_id", workspaceID)
	c.Set("workspace_kind", workspaceKind)
	c.Set("workspace_role", models.WorkspaceRoleOwner)
	c.Request.URL.RawQuery = "page=1&page_size=50"
	h.GetPendingAgents(c)
	return w
}

func performScopedUpdateAgentRequestWithKind(t *testing.T, h *AgentHandler, agentID uint64, role string, workspaceID uint64, workspaceKind string, payload map[string]any) *httptest.ResponseRecorder {
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
	c.Set("workspace_id", workspaceID)
	c.Set("workspace_kind", workspaceKind)
	c.Set("workspace_role", models.WorkspaceRoleOwner)
	h.UpdateAgent(c)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
	}
}

func requireAgentNames(t *testing.T, body string, want ...string) {
	t.Helper()
	var resp struct {
		Data struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, body)
	}
	got := make([]string, 0, len(resp.Data.List))
	for _, item := range resp.Data.List {
		got = append(got, item.Name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("agent names=%v want=%v body=%s", got, want, body)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("agent names=%v want=%v body=%s", got, want, body)
		}
	}
}

func TestGetAgentList_AdminWorkspaceSeesAllAgents(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	adminWorkspace := seedWorkspace(t, db, "platform-governance", models.WorkspaceKindAdmin)
	workspaceA := seedWorkspace(t, db, "team-a", models.WorkspaceKindNormal)
	workspaceB := seedWorkspace(t, db, "team-b", models.WorkspaceKindNormal)
	seedApprovedPlatformAgent(t, db, "platform-agent")
	seedApprovedWorkspaceAgent(t, db, workspaceA.ID, "workspace-a-agent")
	seedApprovedWorkspaceAgent(t, db, workspaceB.ID, "workspace-b-agent")

	w := performAgentListRequest(t, h, "admin", adminWorkspace.ID, models.WorkspaceKindAdmin)
	requireStatus(t, w, http.StatusOK)
	requireAgentNames(t, w.Body.String(), "platform-agent", "workspace-a-agent", "workspace-b-agent")
}

func TestGetAgentList_AdminWithoutAdminWorkspaceDoesNotGetGlobalFullScope(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	workspaceA := seedWorkspace(t, db, "team-a", models.WorkspaceKindNormal)
	workspaceB := seedWorkspace(t, db, "team-b", models.WorkspaceKindNormal)
	seedApprovedPlatformAgent(t, db, "platform-agent")
	seedApprovedWorkspaceAgent(t, db, workspaceA.ID, "workspace-a-agent")
	seedApprovedWorkspaceAgent(t, db, workspaceB.ID, "workspace-b-agent")

	w := performAgentListRequest(t, h, "admin", 0, "")
	requireStatus(t, w, http.StatusOK)
	requireAgentNames(t, w.Body.String(), "platform-agent")
}

func TestUpdateAgent_AdminWorkspaceCanManageWorkspacePrivateAgentAcrossWorkspaces(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	adminWorkspace := seedWorkspace(t, db, "platform-governance", models.WorkspaceKindAdmin)
	targetWorkspace := seedWorkspace(t, db, "target-space", models.WorkspaceKindNormal)
	agent := seedApprovedWorkspaceAgent(t, db, targetWorkspace.ID, "workspace-agent")

	w := performScopedUpdateAgentRequestWithKind(t, h, agent.ID, "admin", adminWorkspace.ID, models.WorkspaceKindAdmin, map[string]any{
		"name": "workspace-agent-updated",
	})
	requireStatus(t, w, http.StatusOK)

	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.Name != "workspace-agent-updated" {
		t.Fatalf("agent name=%s, want=workspace-agent-updated", got.Name)
	}
}

func TestGetPendingAgents_AdminWorkspaceSeesAllPendingAgents(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	adminWorkspace := seedWorkspace(t, db, "platform-governance", models.WorkspaceKindAdmin)
	workspaceA := seedWorkspace(t, db, "team-a", models.WorkspaceKindNormal)
	workspaceB := seedWorkspace(t, db, "team-b", models.WorkspaceKindNormal)
	seedPendingPlatformAgent(t, db, "platform-pending-agent")
	seedPendingWorkspaceAgent(t, db, workspaceA.ID, "workspace-a-pending-agent")
	seedPendingWorkspaceAgent(t, db, workspaceB.ID, "workspace-b-pending-agent")

	w := performPendingAgentListRequest(t, h, "admin", adminWorkspace.ID, models.WorkspaceKindAdmin)
	requireStatus(t, w, http.StatusOK)
	requireAgentNames(t, w.Body.String(), "platform-pending-agent", "workspace-a-pending-agent", "workspace-b-pending-agent")
}

func TestGetPendingAgents_NormalWorkspaceStillFiltered(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	workspaceA := seedWorkspace(t, db, "team-a", models.WorkspaceKindNormal)
	workspaceB := seedWorkspace(t, db, "team-b", models.WorkspaceKindNormal)
	seedPendingPlatformAgent(t, db, "platform-pending-agent")
	seedPendingWorkspaceAgent(t, db, workspaceA.ID, "workspace-a-pending-agent")
	seedPendingWorkspaceAgent(t, db, workspaceB.ID, "workspace-b-pending-agent")

	w := performPendingAgentListRequest(t, h, "admin", workspaceA.ID, models.WorkspaceKindNormal)
	requireStatus(t, w, http.StatusOK)
	requireAgentNames(t, w.Body.String(), "workspace-a-pending-agent")
}
