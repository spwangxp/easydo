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

func performCreateTaskRequest(t *testing.T, h *TaskHandler, workspaceID uint64, role string, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", uint64(1))
	c.Set("role", role)
	c.Set("workspace_id", workspaceID)
	h.CreateTask(c)
	return w
}

func TestCreateTask_RejectsCrossWorkspacePrivateAgent(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &TaskHandler{DB: db}

	agent := models.Agent{
		Name:               "ws-b-agent",
		Host:               "host-b",
		Port:               9001,
		Token:              "agent-token-b",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopeWorkspace,
		WorkspaceID:        22,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	w := performCreateTaskRequest(t, h, 11, "user", map[string]interface{}{
		"agent_id":  agent.ID,
		"task_type": "shell",
		"name":      "cross-workspace-task",
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-workspace agent, got %d body=%s", w.Code, w.Body.String())
	}
	var taskCount int64
	if err := db.Model(&models.AgentTask{}).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks failed: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("taskCount=%d, want=0", taskCount)
	}
}

func TestCreateTask_AllowsPlatformAgentInWorkspace(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &TaskHandler{DB: db}

	agent := models.Agent{
		Name:               "platform-agent",
		Host:               "host-platform",
		Port:               9002,
		Token:              "agent-token-platform",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopePlatform,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	w := performCreateTaskRequest(t, h, 11, "user", map[string]interface{}{
		"agent_id":  agent.ID,
		"task_type": "shell",
		"name":      "platform-task",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for platform agent, got %d body=%s", w.Code, w.Body.String())
	}

	var task models.AgentTask
	if err := db.First(&task).Error; err != nil {
		t.Fatalf("load created task failed: %v", err)
	}
	if task.WorkspaceID != 11 {
		t.Fatalf("task workspace_id=%d, want=11", task.WorkspaceID)
	}
	if task.AgentID != agent.ID {
		t.Fatalf("task agent_id=%d, want=%d", task.AgentID, agent.ID)
	}
}
