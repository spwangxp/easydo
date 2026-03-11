package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSelectAgent_RejectsForgedWorkspaceScopeForNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	platformAgent := models.Agent{
		Name:                   "platform-agent",
		Host:                   "platform-host",
		Port:                   9100,
		Token:                  "platform-token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 2,
		ScopeType:              models.AgentScopePlatform,
	}
	workspaceAgent := models.Agent{
		Name:                   "workspace-b-agent",
		Host:                   "workspace-host",
		Port:                   9101,
		Token:                  "workspace-token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 2,
		ScopeType:              models.AgentScopeWorkspace,
		WorkspaceID:            22,
	}
	if err := db.Create(&platformAgent).Error; err != nil {
		t.Fatalf("create platform agent failed: %v", err)
	}
	if err := db.Create(&workspaceAgent).Error; err != nil {
		t.Fatalf("create workspace agent failed: %v", err)
	}

	body, err := json.Marshal(map[string]interface{}{
		"workspace_id": 22,
	})
	if err != nil {
		t.Fatalf("marshal request failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/agents/select", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", uint64(1001))
	c.Set("role", "user")
	c.Set("workspace_id", uint64(11))

	h.SelectAgent(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for forged workspace scope, got %d body=%s", w.Code, w.Body.String())
	}
}
