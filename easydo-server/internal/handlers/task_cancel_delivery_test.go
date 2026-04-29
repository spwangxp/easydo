package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func installConnectedAgentSocket(t *testing.T, agentID uint64) (chan WebSocketMessage, func()) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	messages := make(chan WebSocketMessage, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg WebSocketMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				return
			}
			messages <- msg
		}
	}))

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial websocket failed: %v", err)
	}

	handler := SharedWebSocketHandler()
	handler.agentsMu.Lock()
	previousAgents := handler.agents
	handler.agents = map[uint64]*wsClient{agentID: {agentID: agentID, sessionID: "session-1", serverID: "server-1", conn: conn}}
	handler.agentsMu.Unlock()

	cleanup := func() {
		handler.agentsMu.Lock()
		handler.agents = previousAgents
		handler.agentsMu.Unlock()
		_ = conn.Close()
		server.Close()
	}
	return messages, cleanup
}

func assertTaskCancelMessage(t *testing.T, messages <-chan WebSocketMessage, taskID uint64) {
	t.Helper()
	select {
	case msg := <-messages:
		if msg.Type != "task_cancel" {
			t.Fatalf("message type=%s, want task_cancel", msg.Type)
		}
		if got := uint64(msg.Payload["task_id"].(float64)); got != taskID {
			t.Fatalf("task_id=%d, want=%d", got, taskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task_cancel websocket message")
	}
}

func TestCancelTask_SendsTaskCancelToConnectedAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	workspace := models.Workspace{Name: "ws-cancel", Slug: "ws-cancel", Status: "active"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{
		Name:               "cancel-agent",
		Host:               "127.0.0.1",
		Port:               9001,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		WorkspaceID:        workspace.ID,
		ScopeType:          models.AgentScopeWorkspace,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, Status: models.PipelineRunStatusRunning, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, PipelineRunID: run.ID, AgentID: agent.ID, NodeID: "node-1", TaskType: "shell", Name: "cancel-me", Status: models.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	messages, cleanup := installConnectedAgentSocket(t, agent.ID)
	defer cleanup()

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
	assertTaskCancelMessage(t, messages, task.ID)
}

func TestCancelPipelineRun_SendsTaskCancelToConnectedAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "cancel-pipeline-user", models.WorkspaceRoleDeveloper)
	pipeline := models.Pipeline{Name: "cancel-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	agent := models.Agent{Name: "cancel-pipeline-agent", Host: "127.0.0.1", Port: 9002, Token: "token-2", Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved, WorkspaceID: workspace.ID, ScopeType: models.AgentScopeWorkspace}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, PipelineRunID: run.ID, AgentID: agent.ID, NodeID: "node-running", TaskType: "shell", Name: "cancel-from-pipeline", Status: models.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	messages, cleanup := installConnectedAgentSocket(t, agent.ID)
	defer cleanup()

	h := &PipelineHandler{DB: db}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/cancel", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")
	c.Set("workspace_id", workspace.ID)

	h.CancelPipelineRun(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertTaskCancelMessage(t, messages, task.ID)
}

func TestCancelRunningTasksForFailedPipeline_SendsTaskCancelToConnectedAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	oldDB := models.DB
	models.DB = db
	defer func() { models.DB = oldDB }()

	workspace := models.Workspace{Name: "ws-failed-cancel", Slug: "ws-failed-cancel", Status: "active"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{Name: "failed-cancel-agent", Host: "127.0.0.1", Port: 9003, Token: "token-3", Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved, WorkspaceID: workspace.ID, ScopeType: models.AgentScopeWorkspace}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, Status: models.PipelineRunStatusRunning, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, PipelineRunID: run.ID, AgentID: agent.ID, NodeID: "node-acked", TaskType: "shell", Name: "cancel-from-failure", Status: models.TaskStatusAcked}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	messages, cleanup := installConnectedAgentSocket(t, agent.ID)
	defer cleanup()

	h := SharedWebSocketHandler()
	h.cancelRunningTasksForFailedPipeline(run.ID, agent.ID)

	assertTaskCancelMessage(t, messages, task.ID)
}
