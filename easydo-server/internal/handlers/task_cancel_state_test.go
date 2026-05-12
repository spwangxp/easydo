package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestCancelTaskState_UsesIntermediateStatusForExecutionOwnedTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	workspace := models.Workspace{Name: "ws-cancel-state", Slug: "ws-cancel-state", Status: "active"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{Name: "cancel-state-agent", Host: "127.0.0.1", Port: 9101, Token: "token", Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved, WorkspaceID: workspace.ID, ScopeType: models.AgentScopeWorkspace}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, Status: models.PipelineRunStatusRunning, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	startTime := time.Now().Add(-30 * time.Second).Unix()
	task := models.AgentTask{WorkspaceID: workspace.ID, PipelineRunID: run.ID, AgentID: agent.ID, NodeID: "node-cancel-state", TaskType: "shell", Name: "cancel-state", Status: models.TaskStatusRunning, StartTime: startTime}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	taskHandler := &TaskHandler{DB: db}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/"+strconv.FormatUint(task.ID, 10)+"/cancel", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(task.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	taskHandler.CancelTask(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusCancelRequested {
		t.Fatalf("task status=%s, want %s", reloaded.Status, models.TaskStatusCancelRequested)
	}
	if reloaded.EndTime != 0 {
		t.Fatalf("end_time=%d, want 0 before terminal cancel", reloaded.EndTime)
	}
}

func TestCancelVsPull_RejectsPullAfterCancelRequested(t *testing.T) {
	setupAgentWSTestRuntime(t)
	gin.SetMode(gin.TestMode)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "cancel-pull-agent",
		Host:               "host-cancel-pull",
		Port:               1,
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-cancel-pull",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	handler := NewWebSocketHandler()
	server := newAgentWSTestServer(t, handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "?agent_id="+strconv.FormatUint(agent.ID, 10)+"&token="+agent.Token), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer conn.Close()

	heartbeat := WebSocketMessage{Type: "heartbeat", Payload: map[string]interface{}{"cpu_usage": 1.0, "memory_usage": 2.0, "disk_usage": 3.0, "tasks_running": float64(0)}}
	heartbeatData, _ := json.Marshal(heartbeat)
	if err := conn.WriteMessage(websocket.TextMessage, heartbeatData); err != nil {
		t.Fatalf("send heartbeat failed: %v", err)
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read heartbeat ack failed: %v", err)
		}
		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal heartbeat ack failed: %v", err)
		}
		if msg.Type == "heartbeat_ack" {
			break
		}
	}

	task := models.AgentTask{AgentID: agent.ID, WorkspaceID: 1, PipelineRunID: 1, NodeID: "node-cancel-pull", TaskType: "shell", Name: "cancel-vs-pull", Status: models.TaskStatusCancelRequested, DispatchToken: "dispatch-token", DispatchAttempt: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	pullTask := WebSocketMessage{Type: "pull_task", Payload: map[string]interface{}{"task_id": float64(task.ID), "dispatch_token": task.DispatchToken}}
	pullTaskData, _ := json.Marshal(pullTask)
	if err := conn.WriteMessage(websocket.TextMessage, pullTaskData); err != nil {
		t.Fatalf("send pull_task failed: %v", err)
	}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read pull response failed: %v", err)
		}
		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal pull response failed: %v", err)
		}
		if msg.Type != "ack_v2" {
			if msg.Type == "task_payload" {
				t.Fatalf("unexpected task_payload for cancel_requested task")
			}
			continue
		}
		if getString(msg.Payload, "event") != "pull_task" {
			continue
		}
		if ok, _ := msg.Payload["ok"].(bool); ok {
			t.Fatalf("expected pull_task ack failure for cancel_requested task")
		}
		break
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusCancelRequested {
		t.Fatalf("task status=%s, want %s", reloaded.Status, models.TaskStatusCancelRequested)
	}
}
