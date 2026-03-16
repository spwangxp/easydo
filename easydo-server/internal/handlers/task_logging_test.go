package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSanitizeTaskLogText_MasksSensitiveValues(t *testing.T) {
	raw := "curl -H 'Authorization: Bearer top-secret' https://demo:superpass@example.com?token=abc --password hunter2 EASYDO_CRED_REPO_AUTH_TOKEN=ghp_123"
	masked := sanitizeTaskLogText(raw)
	for _, secret := range []string{"top-secret", "superpass", "hunter2", "ghp_123", "token=abc"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("expected secret %q to be masked, got %s", secret, masked)
		}
	}
	for _, expected := range []string{"Authorization: Bearer ***", "https://***:***@example.com", "--password ***", "EASYDO_CRED_REPO_AUTH_TOKEN=***"} {
		if !strings.Contains(masked, expected) {
			t.Fatalf("expected masked output to contain %q, got %s", expected, masked)
		}
	}
}

func TestExecuteServerTask_WritesStructuredLogsToExistingLogQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousLogs := agentFileLogs
	models.DB = db
	agentFileLogs = newTaskLogStore()
	t.Cleanup(func() {
		models.DB = previousDB
		agentFileLogs = previousLogs
	})

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "log-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "log-pipeline", WorkspaceID: workspace.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	agent := models.Agent{
		Name:               "server-task-agent",
		Host:               "server.internal",
		Token:              "server-task-token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ApprovedAt:         1,
		LastHeartAt:        1,
		HeartbeatInterval:  10,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning, TriggerUserID: user.ID, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	node := &PipelineNode{ID: "notify", Name: "站内信", Type: "in_app"}
	config := map[string]interface{}{
		"title":   "Deploy token=abc123 ready",
		"content": "Authorization: Bearer super-secret-message",
	}

	h := NewPipelineHandler()
	success, errMsg := h.executeServerTask(db, &run, node, "in_app", config, 30)
	if !success {
		t.Fatalf("expected server task success, got err=%s", errMsg)
	}

	var task models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task).Error; err != nil {
		t.Fatalf("load server task failed: %v", err)
	}

	taskResp := performCredentialRequest(t, NewTaskHandler().GetTaskLogs, user.ID, "user", workspace.ID, http.MethodGet, fmt.Sprintf("/tasks/%d/logs", task.ID), nil, pathID(task.ID))
	if taskResp.Code != http.StatusOK {
		t.Fatalf("task logs status=%d body=%s", taskResp.Code, taskResp.Body.String())
	}
	var taskPayload struct {
		Code int `json:"code"`
		Data struct {
			List []models.AgentLog `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(taskResp.Body.Bytes(), &taskPayload); err != nil {
		t.Fatalf("unmarshal task logs failed: %v", err)
	}
	if len(taskPayload.Data.List) == 0 {
		t.Fatalf("expected server task logs to be persisted")
	}

	runResp := performCredentialRequest(t, NewPipelineHandler().GetRunLogs, user.ID, "user", workspace.ID, http.MethodGet, fmt.Sprintf("/pipelines/%d/runs/%d/logs", pipeline.ID, run.ID), nil, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", pipeline.ID)}, {Key: "run_id", Value: fmt.Sprintf("%d", run.ID)}}
	})
	if runResp.Code != http.StatusOK {
		t.Fatalf("run logs status=%d body=%s", runResp.Code, runResp.Body.String())
	}
	var runPayload struct {
		Code int `json:"code"`
		Data struct {
			List []models.AgentLog `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(runResp.Body.Bytes(), &runPayload); err != nil {
		t.Fatalf("unmarshal run logs failed: %v", err)
	}
	if len(runPayload.Data.List) == 0 {
		t.Fatalf("expected run logs to include server task entries")
	}

	joined := ""
	for _, entry := range runPayload.Data.List {
		joined += entry.Message + "\n"
	}
	if !strings.Contains(joined, "[easydo][step]") || !strings.Contains(joined, "[easydo][cmd]") {
		t.Fatalf("expected structured server task logs, got %s", joined)
	}
	for _, secret := range []string{"abc123", "super-secret-message"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("expected persisted logs to mask secret %q, got %s", secret, joined)
		}
	}
}
