package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestNewWebSocketHandler(t *testing.T) {
	handler := NewWebSocketHandler()

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.agents)
	assert.NotNil(t, handler.frontends)
	assert.Equal(t, uint64(0), handler.clientIDCounter)
}

func TestWebSocketMessage_Marshal(t *testing.T) {
	msg := WebSocketMessage{
		Type: "heartbeat",
		Payload: map[string]interface{}{
			"timestamp":    float64(1234567890),
			"agent_id":     float64(1),
			"cpu_usage":    45.5,
			"memory_usage": 60.0,
		},
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, msg.Type, decoded.Type)
	assert.Equal(t, msg.Payload["timestamp"], decoded.Payload["timestamp"])
	assert.Equal(t, msg.Payload["agent_id"], decoded.Payload["agent_id"])
}

func TestWebSocketMessage_Unmarshal(t *testing.T) {
	jsonData := `{
		"type": "task_status",
		"payload": {
			"task_id": 123,
			"run_id": 456,
			"status": "execute_success",
			"exit_code": 0,
			"error_msg": ""
		}
	}`

	var msg WebSocketMessage
	err := json.Unmarshal([]byte(jsonData), &msg)
	assert.NoError(t, err)
	assert.Equal(t, "task_status", msg.Type)
	assert.Equal(t, float64(123), msg.Payload["task_id"])
	assert.Equal(t, float64(456), msg.Payload["run_id"])
	assert.Equal(t, "execute_success", msg.Payload["status"])
}

func TestWebSocketMessage_Unmarshal_EmptyPayload(t *testing.T) {
	jsonData := `{"type": "heartbeat_ack", "payload": {}}`

	var msg WebSocketMessage
	err := json.Unmarshal([]byte(jsonData), &msg)
	assert.NoError(t, err)
	assert.Equal(t, "heartbeat_ack", msg.Type)
	assert.NotNil(t, msg.Payload)
}

func TestGetInt64(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected int64
	}{
		{
			name:     "float64 value",
			m:        map[string]interface{}{"key": float64(123)},
			key:      "key",
			expected: 123,
		},
		{
			name:     "int value",
			m:        map[string]interface{}{"key": 456},
			key:      "key",
			expected: 456,
		},
		{
			name:     "int64 value",
			m:        map[string]interface{}{"key": int64(789)},
			key:      "key",
			expected: 789,
		},
		{
			name:     "string value",
			m:        map[string]interface{}{"key": "999"},
			key:      "key",
			expected: 999,
		},
		{
			name:     "missing key",
			m:        map[string]interface{}{},
			key:      "missing",
			expected: 0,
		},
		{
			name:     "invalid string",
			m:        map[string]interface{}{"key": "invalid"},
			key:      "key",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getInt64(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetFloat64(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected float64
	}{
		{
			name:     "float64 value",
			m:        map[string]interface{}{"key": float64(45.5)},
			key:      "key",
			expected: 45.5,
		},
		{
			name:     "int value",
			m:        map[string]interface{}{"key": 100},
			key:      "key",
			expected: 100.0,
		},
		{
			name:     "string value",
			m:        map[string]interface{}{"key": "55.5"},
			key:      "key",
			expected: 55.5,
		},
		{
			name:     "missing key",
			m:        map[string]interface{}{},
			key:      "missing",
			expected: 0,
		},
		{
			name:     "invalid string",
			m:        map[string]interface{}{"key": "not a number"},
			key:      "key",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFloat64(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected string
	}{
		{
			name:     "string value",
			m:        map[string]interface{}{"key": "hello"},
			key:      "key",
			expected: "hello",
		},
		{
			name:     "missing key",
			m:        map[string]interface{}{},
			key:      "missing",
			expected: "",
		},
		{
			name:     "non-string value",
			m:        map[string]interface{}{"key": 123},
			key:      "key",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getString(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAgentConnectionMap(t *testing.T) {
	handler := NewWebSocketHandler()

	assert.NotNil(t, handler.agents)
	assert.Equal(t, 0, len(handler.agents))

	handler.agentsMu.Lock()
	handler.agents[1] = nil
	handler.agents[2] = nil
	handler.agentsMu.Unlock()

	assert.Equal(t, 2, len(handler.agents))

	handler.agentsMu.Lock()
	delete(handler.agents, 1)
	handler.agentsMu.Unlock()

	assert.Equal(t, 1, len(handler.agents))
}

func TestFrontendConnectionMap(t *testing.T) {
	handler := NewWebSocketHandler()

	assert.NotNil(t, handler.frontends)
	assert.Equal(t, 0, len(handler.frontends))

	handler.frontendsMu.Lock()
	handler.frontends["run_1"] = make(map[string]*frontendClient)
	handler.frontends["run_2"] = make(map[string]*frontendClient)
	handler.frontendsMu.Unlock()

	assert.Equal(t, 2, len(handler.frontends))

	handler.frontendsMu.Lock()
	delete(handler.frontends, "run_1")
	handler.frontendsMu.Unlock()

	assert.Equal(t, 1, len(handler.frontends))
}

func TestBroadcastMessageStructure(t *testing.T) {
	payload := map[string]interface{}{
		"task_id":    float64(1),
		"run_id":     float64(100),
		"status":     models.TaskStatusExecuteSuccess,
		"exit_code":  float64(0),
		"error_msg":  "",
		"duration":   float64(120),
		"agent_id":   float64(5),
		"agent_name": "test-agent",
		"timestamp":  float64(time.Now().Unix()),
	}

	msg := WebSocketMessage{
		Type:    "task_status",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "task_status", decoded.Type)
	assert.Equal(t, payload["task_id"], decoded.Payload["task_id"])
	assert.Equal(t, payload["run_id"], decoded.Payload["run_id"])
	assert.Equal(t, payload["status"], decoded.Payload["status"])
}

func TestSendTaskAssign_RollsBackTaskWhenStreamPublishFails(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	task := models.AgentTask{
		AgentID:       9,
		PipelineRunID: 12,
		WorkspaceID:   1,
		NodeID:        "build",
		Name:          "build",
		TaskType:      "shell",
		Status:        models.TaskStatusAssigned,
		Timeout:       60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	ok := handler.sendTaskAssign(task)
	assert.False(t, ok)

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	assert.Equal(t, models.TaskStatusAssigned, reloaded.Status)
	assert.Empty(t, reloaded.DispatchToken)
	assert.Equal(t, 0, reloaded.DispatchAttempt)
	assert.EqualValues(t, 0, reloaded.LeaseExpireAt)
	assert.Empty(t, reloaded.ErrorMsg)
}

func TestSendTaskAssign_PublishesAgentStreamEvent(t *testing.T) {
	db := openHandlerTestDB(t)
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	task := models.AgentTask{
		AgentID:       7,
		PipelineRunID: 21,
		WorkspaceID:   1,
		NodeID:        "deploy",
		Name:          "deploy",
		TaskType:      "shell",
		Status:        models.TaskStatusAssigned,
		Timeout:       60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	ok := handler.sendTaskAssign(task)
	assert.True(t, ok)

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	assert.Equal(t, models.TaskStatusDispatching, reloaded.Status)
	assert.NotEmpty(t, reloaded.DispatchToken)
	assert.Equal(t, 1, reloaded.DispatchAttempt)
	assert.Greater(t, reloaded.LeaseExpireAt, time.Now().Unix())

	entries, err := utils.RedisClient.XRange(context.Background(), utils.AgentStreamKey(task.AgentID), "-", "+").Result()
	if err != nil {
		t.Fatalf("read agent stream failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 stream entry, got %d", len(entries))
	}
	assert.Equal(t, reloaded.DispatchToken, entries[0].Values["dispatch_token"])
	assert.Equal(t, "1", entries[0].Values["dispatch_attempt"])
	assert.Equal(t, fmt.Sprintf("%d", reloaded.ID), fmt.Sprintf("%v", entries[0].Values["task_id"]))
}

func TestHandleAgentPullTaskRejectsStaleStateTransition(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "pull-stale-agent",
		Host:               "host-stale",
		Port:               1,
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-pull-stale",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	handler := NewWebSocketHandler()
	server := newAgentWSTestServer(t, handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer conn.Close()

	heartbeat := WebSocketMessage{
		Type: "heartbeat",
		Payload: map[string]interface{}{
			"agent_id":  agent.ID,
			"timestamp": time.Now().Unix(),
		},
	}
	heartbeatData, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("marshal heartbeat failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, heartbeatData); err != nil {
		t.Fatalf("write heartbeat failed: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set heartbeat read deadline failed: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read heartbeat ack failed: %v", err)
	}
	conn.SetReadDeadline(time.Time{})
	var heartbeatAck WebSocketMessage
	if err := json.Unmarshal(raw, &heartbeatAck); err != nil {
		t.Fatalf("unmarshal heartbeat ack failed: %v", err)
	}
	if heartbeatAck.Type != "heartbeat_ack" {
		t.Fatalf("expected heartbeat_ack, got %s", heartbeatAck.Type)
	}

	task := models.AgentTask{
		WorkspaceID:     1,
		AgentID:         agent.ID,
		PipelineRunID:   1,
		NodeID:          "node-stale",
		TaskType:        "shell",
		Name:            "Stale Pull",
		Status:          models.TaskStatusDispatching,
		DispatchToken:   "dispatch-token-stale",
		DispatchAttempt: 1,
		Timeout:         60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	staleOverwriteInjected := false
	callbackName := "test:pull-task-stale-state-transition"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if staleOverwriteInjected || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "agent_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		status, _ := updates["status"].(string)
		if status != models.TaskStatusAcked {
			return
		}
		staleOverwriteInjected = true
		if err := db.Model(&models.AgentTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
			"status":           models.TaskStatusRunning,
			"agent_session_id": "newer-session",
			"owner_server_id":  "other-server",
		}).Error; err != nil {
			t.Fatalf("inject newer task state failed: %v", err)
		}
	}); err != nil {
		t.Fatalf("register update callback failed: %v", err)
	}
	defer db.Callback().Update().Remove(callbackName)

	pullTask := WebSocketMessage{
		Type: "pull_task",
		Payload: map[string]interface{}{
			"task_id":        task.ID,
			"dispatch_token": task.DispatchToken,
			"timestamp":      time.Now().Unix(),
		},
	}
	pullTaskData, err := json.Marshal(pullTask)
	if err != nil {
		t.Fatalf("marshal pull_task failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, pullTaskData); err != nil {
		t.Fatalf("write pull_task failed: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set pull_task read deadline failed: %v", err)
	}
	defer conn.SetReadDeadline(time.Time{})

	sawTaskPayload := false
	var ack WebSocketMessage
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read pull_task response failed: %v", err)
		}
		if err := json.Unmarshal(raw, &ack); err != nil {
			t.Fatalf("unmarshal pull_task response failed: %v", err)
		}
		if ack.Type == "task_payload" {
			sawTaskPayload = true
			continue
		}
		if ack.Type == "ack_v2" && ack.Payload["event"] == "pull_task" {
			break
		}
	}

	if !staleOverwriteInjected {
		t.Fatal("expected stale state injection to run")
	}
	if sawTaskPayload {
		t.Fatal("expected stale pull to be rejected before task_payload delivery")
	}
	if ok, _ := ack.Payload["ok"].(bool); ok {
		t.Fatalf("expected pull_task ack failure for stale state transition, got %#v", ack.Payload)
	}
	if getString(ack.Payload, "error_msg") == "" {
		t.Fatalf("expected stale pull rejection to include error message, got %#v", ack.Payload)
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusRunning {
		t.Fatalf("task status=%s, want %s", reloaded.Status, models.TaskStatusRunning)
	}
	if reloaded.AgentSessionID != "newer-session" {
		t.Fatalf("task agent_session_id=%q, want newer-session", reloaded.AgentSessionID)
	}
	if reloaded.OwnerServerID != "other-server" {
		t.Fatalf("task owner_server_id=%q, want other-server", reloaded.OwnerServerID)
	}
}

func TestHandleAgentPullTaskAcksAndBindsOwnership(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "pull-success-agent",
		Host:               "host-success",
		Port:               1,
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-pull-success",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	handler := NewWebSocketHandler()
	server := newAgentWSTestServer(t, handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer conn.Close()

	heartbeat := WebSocketMessage{
		Type: "heartbeat",
		Payload: map[string]interface{}{
			"agent_id":  agent.ID,
			"timestamp": time.Now().Unix(),
		},
	}
	heartbeatData, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("marshal heartbeat failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, heartbeatData); err != nil {
		t.Fatalf("write heartbeat failed: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set heartbeat read deadline failed: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read heartbeat ack failed: %v", err)
	}
	conn.SetReadDeadline(time.Time{})
	var heartbeatAck WebSocketMessage
	if err := json.Unmarshal(raw, &heartbeatAck); err != nil {
		t.Fatalf("unmarshal heartbeat ack failed: %v", err)
	}
	if heartbeatAck.Type != "heartbeat_ack" {
		t.Fatalf("expected heartbeat_ack, got %s", heartbeatAck.Type)
	}
	sessionID := getString(heartbeatAck.Payload, "agent_session_id")
	if sessionID == "" {
		t.Fatal("expected heartbeat ack to carry agent_session_id")
	}

	task := models.AgentTask{
		WorkspaceID:     1,
		AgentID:         agent.ID,
		PipelineRunID:   1,
		NodeID:          "node-success",
		TaskType:        "shell",
		Name:            "Successful Pull",
		Status:          models.TaskStatusDispatching,
		DispatchToken:   "dispatch-token-success",
		DispatchAttempt: 1,
		Timeout:         60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	pullTask := WebSocketMessage{
		Type: "pull_task",
		Payload: map[string]interface{}{
			"task_id":          task.ID,
			"dispatch_token":   task.DispatchToken,
			"agent_session_id": sessionID,
			"timestamp":        time.Now().Unix(),
		},
	}
	pullTaskData, err := json.Marshal(pullTask)
	if err != nil {
		t.Fatalf("marshal pull_task failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, pullTaskData); err != nil {
		t.Fatalf("write pull_task failed: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set pull_task read deadline failed: %v", err)
	}
	defer conn.SetReadDeadline(time.Time{})

	sawTaskPayload := false
	sawAck := false
	for !(sawTaskPayload && sawAck) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read pull_task response failed: %v", err)
		}
		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal pull_task response failed: %v", err)
		}
		switch msg.Type {
		case "task_payload":
			taskPayload, ok := msg.Payload["task"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected task payload map, got %#v", msg.Payload["task"])
			}
			if uint64(getFloat64(taskPayload, "id")) != task.ID {
				t.Fatalf("task payload id=%d, want %d", uint64(getFloat64(taskPayload, "id")), task.ID)
			}
			if getString(taskPayload, "dispatch_token") != task.DispatchToken {
				t.Fatalf("task payload dispatch_token=%q, want %q", getString(taskPayload, "dispatch_token"), task.DispatchToken)
			}
			if getString(taskPayload, "status") != models.TaskStatusAcked {
				t.Fatalf("task payload status=%q, want %q", getString(taskPayload, "status"), models.TaskStatusAcked)
			}
			sawTaskPayload = true
		case "ack_v2":
			if msg.Payload["event"] != "pull_task" {
				continue
			}
			if ok, _ := msg.Payload["ok"].(bool); !ok {
				t.Fatalf("expected successful pull_task ack, got %#v", msg.Payload)
			}
			sawAck = true
		}
	}

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusAcked {
		t.Fatalf("task status=%s, want %s", reloaded.Status, models.TaskStatusAcked)
	}
	if reloaded.AgentSessionID != sessionID {
		t.Fatalf("task agent_session_id=%q, want %q", reloaded.AgentSessionID, sessionID)
	}
	if reloaded.OwnerServerID != handler.serverID {
		t.Fatalf("task owner_server_id=%q, want %q", reloaded.OwnerServerID, handler.serverID)
	}
}

func TestRetryTaskRejectsOldDispatchIdentity(t *testing.T) {
	setupAgentWSTestRuntime(t)
	gin.SetMode(gin.TestMode)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	workspace := models.Workspace{Name: "retry-dispatch-workspace", Slug: "retry-dispatch-workspace", Status: "active"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{
		Name:               "retry-dispatch-agent",
		Host:               "host-retry",
		Port:               1,
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-retry-dispatch",
		WorkspaceID:        workspace.ID,
		ScopeType:          models.AgentScopeWorkspace,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	oldDispatchToken := "dispatch-token-old"
	task := models.AgentTask{
		WorkspaceID:     workspace.ID,
		AgentID:         agent.ID,
		PipelineRunID:   1,
		NodeID:          "node-retry",
		TaskType:        "shell",
		Name:            "Retry Task",
		Status:          models.TaskStatusExecuteFailed,
		RetryCount:      0,
		MaxRetries:      2,
		DispatchToken:   oldDispatchToken,
		DispatchAttempt: 3,
		LeaseExpireAt:   time.Now().Add(-time.Minute).Unix(),
		AgentSessionID:  "old-session",
		OwnerServerID:   "old-server",
		ErrorMsg:        "previous dispatch failed",
		Timeout:         60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	taskHandler := &TaskHandler{DB: db}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/"+strconv.FormatUint(task.ID, 10)+"/retry", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(task.ID, 10)}}
	c.Set("workspace_id", workspace.ID)

	taskHandler.RetryTask(c)
	if w.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", w.Code, w.Body.String())
	}

	var retried models.AgentTask
	if err := db.First(&retried, task.ID).Error; err != nil {
		t.Fatalf("reload retried task failed: %v", err)
	}
	if retried.Status != models.TaskStatusDispatching {
		t.Fatalf("retried task status=%s, want %s", retried.Status, models.TaskStatusDispatching)
	}
	if retried.DispatchToken == "" {
		t.Fatal("expected retry to issue a new dispatch token")
	}
	if retried.DispatchToken == oldDispatchToken {
		t.Fatalf("retry kept old dispatch token=%q", retried.DispatchToken)
	}
	if retried.DispatchAttempt != 1 {
		t.Fatalf("retried dispatch_attempt=%d, want 1", retried.DispatchAttempt)
	}
	if retried.AgentSessionID != "" {
		t.Fatalf("retried agent_session_id=%q, want empty", retried.AgentSessionID)
	}
	if retried.OwnerServerID != "" {
		t.Fatalf("retried owner_server_id=%q, want empty", retried.OwnerServerID)
	}

	oldEvent := utils.AgentStreamEvent{
		TaskID:          retried.ID,
		DispatchToken:   oldDispatchToken,
		DispatchAttempt: task.DispatchAttempt,
		CreatedAt:       time.Now().Unix(),
	}
	if shouldDispatchAgentStreamEvent(&retried, oldEvent) {
		t.Fatal("expected old dispatch stream event to be rejected after retry")
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: agent.ID, sessionID: "session-retry-new", serverID: "server-retry-new"}
	handler.handleAgentPullTask(client, &agent, map[string]interface{}{
		"task_id":        float64(retried.ID),
		"dispatch_token": oldDispatchToken,
		"timestamp":      float64(time.Now().Unix()),
	})

	var afterStalePull models.AgentTask
	if err := db.First(&afterStalePull, task.ID).Error; err != nil {
		t.Fatalf("reload task after stale pull failed: %v", err)
	}
	if afterStalePull.Status != models.TaskStatusDispatching {
		t.Fatalf("task status after stale pull=%s, want %s", afterStalePull.Status, models.TaskStatusDispatching)
	}
	if afterStalePull.DispatchToken != retried.DispatchToken {
		t.Fatalf("task dispatch_token after stale pull=%q, want %q", afterStalePull.DispatchToken, retried.DispatchToken)
	}
	if afterStalePull.DispatchAttempt != retried.DispatchAttempt {
		t.Fatalf("task dispatch_attempt after stale pull=%d, want %d", afterStalePull.DispatchAttempt, retried.DispatchAttempt)
	}
	if afterStalePull.AgentSessionID != "" {
		t.Fatalf("task agent_session_id after stale pull=%q, want empty", afterStalePull.AgentSessionID)
	}
	if afterStalePull.OwnerServerID != "" {
		t.Fatalf("task owner_server_id after stale pull=%q, want empty", afterStalePull.OwnerServerID)
	}
}

func TestTriggerDownstreamTasks_InjectsCredentialEnvForGitClone(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "downstream-user", models.WorkspaceRoleDeveloper)
	encrypted, err := NewCredentialHandler().encryptionService.EncryptCredentialData(map[string]interface{}{
		"access_token": "gho_downstream_only",
		"username":     "oauth2",
	})
	if err != nil {
		t.Fatalf("encrypt payload failed: %v", err)
	}
	credential := models.Credential{
		Name:             "downstream-repo-auth",
		Type:             models.TypeToken,
		Category:         models.CategoryGitHub,
		Scope:            models.ScopeWorkspace,
		WorkspaceID:      workspace.ID,
		OwnerID:          user.ID,
		EncryptedPayload: encrypted,
		Status:           models.CredentialStatusActive,
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create credential failed: %v", err)
	}

	runConfig, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "1", Type: "shell", Name: "Build", Config: map[string]interface{}{"script": "echo ok"}},
			{ID: "2", Type: "git_clone", Name: "Clone", Config: map[string]interface{}{
				"repository": map[string]interface{}{"url": "https://example.com/repo.git"},
				"credentials": map[string]interface{}{
					"repo_auth": map[string]interface{}{"credential_id": credential.ID},
				},
			}},
		},
		Edges: []PipelineEdge{{From: "1", To: "2"}},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      workspace.ID,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		Config:           "{invalid-json",
		PipelineSnapshot: string(runConfig),
		AgentID:          1,
		TriggerUserID:    user.ID,
		TriggerUserRole:  "user",
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{{NodeID: "1", Status: models.TaskStatusExecuteSuccess}})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "2").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}
	if downstream.EnvVars == "" {
		t.Fatalf("expected downstream task env vars to be injected")
	}

	var envMap map[string]interface{}
	if err := json.Unmarshal([]byte(downstream.EnvVars), &envMap); err != nil {
		t.Fatalf("unmarshal downstream env vars failed: %v", err)
	}
	if envMap["EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN"] != "gho_downstream_only" {
		t.Fatalf("expected downstream access_token env, got %#v", envMap["EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN"])
	}
	if envMap["EASYDO_CRED_REPO_AUTH_TYPE"] != string(models.TypeToken) {
		t.Fatalf("expected downstream type env, got %#v", envMap["EASYDO_CRED_REPO_AUTH_TYPE"])
	}
}

func TestTriggerDownstreamTasks_PreservesDockerTaskType(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	runConfig, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "prep", Type: "shell", Name: "Prepare", Config: map[string]interface{}{"script": "echo ok"}},
			{ID: "build", Type: "docker", Name: "Build Image", Config: map[string]interface{}{
				"image_name": "demo/app",
				"image_tag":  "latest",
			}},
		},
		Edges: []PipelineEdge{{From: "prep", To: "build"}},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(runConfig),
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{{NodeID: "prep", Status: models.TaskStatusExecuteSuccess}})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "build").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}
	if downstream.TaskType != "docker" {
		t.Fatalf("downstream task type=%s, want docker", downstream.TaskType)
	}
	if !strings.Contains(downstream.Script, "执行 Docker 构建任务") {
		t.Fatalf("expected downstream docker task to keep rendered docker build script")
	}
}

func TestTriggerDownstreamTasks_UsesLatestPersistedTaskStateForFanIn(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Build A", Config: map[string]interface{}{"script": "echo a"}},
			{ID: "node_2", Type: "shell", Name: "Build B", Config: map[string]interface{}{"script": "echo b"}},
			{ID: "node_3", Type: "shell", Name: "Deploy", Config: map[string]interface{}{"script": "echo deploy"}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_3"}, {From: "node_2", To: "node_3"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(pipelineSnapshot),
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	node1Task := models.AgentTask{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_1", TaskType: "shell", Status: models.TaskStatusExecuteSuccess}
	node2Task := models.AgentTask{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_2", TaskType: "shell", Status: models.TaskStatusExecuteSuccess}
	if err := db.Create(&node1Task).Error; err != nil {
		t.Fatalf("create node_1 task failed: %v", err)
	}
	if err := db.Create(&node2Task).Error; err != nil {
		t.Fatalf("create node_2 task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{node1Task})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_3").First(&downstream).Error; err != nil {
		t.Fatalf("expected fan-in downstream task to be created from latest persisted state: %v", err)
	}
}

func TestReconcileRunningPipelineTasks_MaterializesReadyFanInNode(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Build A", Config: map[string]interface{}{"script": "echo a"}},
			{ID: "node_2", Type: "shell", Name: "Build B", Config: map[string]interface{}{"script": "echo b"}},
			{ID: "node_3", Type: "shell", Name: "Deploy", Config: map[string]interface{}{"script": "echo deploy"}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_3"}, {From: "node_2", To: "node_3"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(pipelineSnapshot),
		ResolvedNodes:    `[{"node_id":"node_1","status":"execute_success"},{"node_id":"node_2","status":"execute_success"},{"node_id":"node_3","status":"queued","attempts":[]}]`,
		AgentID:          1,
		StartTime:        time.Now().Add(-5 * time.Second).Unix(),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	for _, task := range []models.AgentTask{
		{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_1", TaskType: "shell", Status: models.TaskStatusExecuteSuccess},
		{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_2", TaskType: "shell", Status: models.TaskStatusExecuteSuccess},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create completed task failed: %v", err)
		}
	}

	reconciled := reconcileRunningPipelineTasks(db, 64)
	if reconciled != 1 {
		t.Fatalf("reconciled=%d, want 1", reconciled)
	}

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_3").First(&downstream).Error; err != nil {
		t.Fatalf("expected reconciliation to create ready fan-in node: %v", err)
	}
}

func TestCheckAndUpdatePipelineStatus_WaitsForUnmaterializedPipelineNode(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Build A", Config: map[string]interface{}{"script": "echo a"}},
			{ID: "node_2", Type: "shell", Name: "Build B", Config: map[string]interface{}{"script": "echo b"}},
			{ID: "node_3", Type: "shell", Name: "Deploy", Config: map[string]interface{}{"script": "echo deploy"}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_3"}, {From: "node_2", To: "node_3"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(pipelineSnapshot),
		ResolvedNodes:    `[{"node_id":"node_1","status":"execute_success"},{"node_id":"node_2","status":"execute_success"},{"node_id":"node_3","status":"queued","attempts":[]}]`,
		AgentID:          1,
		StartTime:        time.Now().Add(-5 * time.Second).Unix(),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	for _, task := range []models.AgentTask{
		{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_1", TaskType: "shell", Status: models.TaskStatusExecuteSuccess},
		{WorkspaceID: 1, PipelineRunID: run.ID, NodeID: "node_2", TaskType: "shell", Status: models.TaskStatusExecuteSuccess},
	} {
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create completed task failed: %v", err)
		}
	}

	handler := NewWebSocketHandler()
	handler.checkAndUpdatePipelineStatus(run.ID)

	var reloaded models.PipelineRun
	if err := db.First(&reloaded, run.ID).Error; err != nil {
		t.Fatalf("reload pipeline run failed: %v", err)
	}
	if reloaded.Status != models.PipelineRunStatusRunning {
		t.Fatalf("pipeline status=%s, want %s while node_3 is still queued", reloaded.Status, models.PipelineRunStatusRunning)
	}
}

func TestHeartbeatPayload(t *testing.T) {
	payload := map[string]interface{}{
		"timestamp":     float64(time.Now().Unix()),
		"cpu_usage":     45.5,
		"memory_usage":  60.0,
		"disk_usage":    75.5,
		"load_avg":      "1.5, 1.2, 1.0",
		"tasks_running": float64(2),
		"os":            "linux",
		"arch":          "amd64",
		"version":       "1.0.0",
	}

	msg := WebSocketMessage{
		Type:    "heartbeat",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "heartbeat", decoded.Type)
	assert.Equal(t, payload["cpu_usage"], decoded.Payload["cpu_usage"])
	assert.Equal(t, payload["memory_usage"], decoded.Payload["memory_usage"])
	assert.Equal(t, payload["load_avg"], decoded.Payload["load_avg"])
}

func TestHandleTaskUpdateV2_PersistsResourceBaseInfoFromCollectionTask(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	resource := models.Resource{
		WorkspaceID: 1,
		Name:        "inventory-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.88:22",
		CreatedBy:   1,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceRuntimeLabel{
		WorkspaceID: resource.WorkspaceID,
		ResourceID:  resource.ID,
		TargetType:  "resource",
		TargetKey:   buildResourceBaseInfoResourcePrefix(resource.ID),
		LabelsJSON:  `{"team":"ops","owner":"runtime"}`,
		CreatedBy:   1,
	}).Error; err != nil {
		t.Fatalf("create runtime labels failed: %v", err)
	}
	if err := db.Create(&models.ResourceRuntimeLabel{
		WorkspaceID: resource.WorkspaceID,
		ResourceID:  resource.ID,
		TargetType:  "gpu",
		TargetKey:   buildResourceBaseInfoGPUID(resource.ID, "0"),
		LabelsJSON:  `{"gpuOnly":"true"}`,
		CreatedBy:   1,
	}).Error; err != nil {
		t.Fatalf("create gpu runtime labels failed: %v", err)
	}
	payload := resourceBaseInfoTaskPayload{
		Collection: resourceBaseInfoCollectionSnapshot{
			Kind:         "resource_base_info_refresh",
			ResourceID:   resource.ID,
			ResourceType: models.ResourceTypeVM,
		},
	}
	rawParams, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal params failed: %v", err)
	}
	task := models.AgentTask{
		AgentID:     9,
		Status:      models.TaskStatusRunning,
		TaskType:    "ssh",
		Name:        "采集资源基础信息",
		NodeID:      "resource-base-info",
		WorkspaceID: 1,
		Params:      string(rawParams),
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: 9, sessionID: "session-1"}
	handler.handleTaskUpdateV2(client, &models.Agent{BaseModel: models.BaseModel{ID: 9}, Name: "collector-1"}, map[string]interface{}{
		"task_id":         float64(task.ID),
		"attempt":         float64(1),
		"status":          models.TaskStatusExecuteSuccess,
		"exit_code":       float64(0),
		"duration_ms":     float64(1200),
		"idempotency_key": "resource-base-info-success",
		"result": map[string]interface{}{
			"stdout": "EASYDO_BASE_INFO_BEGIN\nEASYDO_HOSTNAME=vm-prod-01\nEASYDO_CPU_LOGICAL_CORES=8\nEASYDO_MEMORY_TOTAL_BYTES=34359738368\nEASYDO_TOTAL_DISK_BYTES=536870912000\nEASYDO_GPU_COUNT=1\nEASYDO_RESOURCE_LABELS_JSON={\"team\":\"platform\",\"tier\":\"infra\"}\nEASYDO_DISK_ROWS_BEGIN\nNAME=\"sda\" SIZE=\"536870912000\" TYPE=\"disk\" FSTYPE=\"ext4\" MOUNTPOINT=\"/\"\nEASYDO_DISK_ROWS_END\nEASYDO_GPU_CSV_BEGIN\n0, NVIDIA L40, 46068, GPU-UUID-0, 0000:00:00.0, NVIDIA\nEASYDO_GPU_CSV_END\nEASYDO_BASE_INFO_END\n",
			"stderr": "",
		},
	})

	var stored models.Resource
	if err := db.First(&stored, resource.ID).Error; err != nil {
		t.Fatalf("reload resource failed: %v", err)
	}
	if stored.BaseInfoStatus != "success" {
		t.Fatalf("base_info_status=%s, want success", stored.BaseInfoStatus)
	}
	if stored.BaseInfoCollectedAt == 0 {
		t.Fatal("expected base_info_collected_at to be set")
	}
	var storedBaseInfo ResourceBaseInfoV3
	if err := json.Unmarshal([]byte(stored.BaseInfo), &storedBaseInfo); err != nil {
		t.Fatalf("unmarshal stored base_info failed: %v raw=%s", err, stored.BaseInfo)
	}
	if storedBaseInfo.SchemaVersion != 3 {
		t.Fatalf("expected canonical v3 base_info, got=%s", stored.BaseInfo)
	}
	if storedBaseInfo.ResourceID != buildResourceBaseInfoResourcePrefix(resource.ID) {
		t.Fatalf("resourceId=%q, want %q", storedBaseInfo.ResourceID, buildResourceBaseInfoResourcePrefix(resource.ID))
	}
	if storedBaseInfo.Labels["team"] != "platform" || storedBaseInfo.Labels["owner"] != "runtime" {
		t.Fatalf("expected merged top-level labels, got=%#v", storedBaseInfo.Labels)
	}
	if _, exists := storedBaseInfo.Labels["gpuOnly"]; exists {
		t.Fatalf("expected non-resource scoped labels to stay out of persisted base_info, got=%#v", storedBaseInfo.Labels)
	}
	gpuFound := false
	for _, instance := range storedBaseInfo.ResourceInstances {
		if instance.ResourceTypeID != "gpu" {
			continue
		}
		gpuFound = true
		if instance.EntityID == "" {
			t.Fatalf("expected gpu instance to keep canonical entity linkage, got %+v", instance)
		}
	}
	if !gpuFound {
		t.Fatalf("expected at least one gpu resource instance, got %+v", storedBaseInfo.ResourceInstances)
	}
}

func TestHeartbeatAckPayload(t *testing.T) {
	payload := map[string]interface{}{
		"status":                   "ok",
		"server_time":              float64(time.Now().Unix()),
		"pending_tasks":            float64(5),
		"heartbeat_interval":       float64(10),
		"max_concurrent_pipelines": float64(4),
		"task_concurrency":         float64(12),
	}

	msg := WebSocketMessage{
		Type:    "heartbeat_ack",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "heartbeat_ack", decoded.Type)
	assert.Equal(t, "ok", decoded.Payload["status"])
	assert.Equal(t, float64(5), decoded.Payload["pending_tasks"])
	assert.Equal(t, float64(4), decoded.Payload["max_concurrent_pipelines"])
	assert.Equal(t, float64(12), decoded.Payload["task_concurrency"])
}

func TestBuildHeartbeatAckPayloadIncludesConcurrency(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	h := NewWebSocketHandler()
	client := &wsClient{sessionID: "session-1", serverID: "server-1"}
	agent := &models.Agent{HeartbeatInterval: 10, RegistrationStatus: models.AgentRegistrationStatusApproved, MaxConcurrentPipelines: 4, TaskConcurrency: 7, DockerHubMirrorsConfigured: true, DockerHubMirrors: "https://mirror-a.example"}
	if err := db.Create(&models.SystemSetting{Key: models.SystemSettingKeyDockerHubMirrors, Value: `[
		"https://system-mirror.example"
	]`}).Error; err != nil {
		t.Fatalf("seed system setting failed: %v", err)
	}

	payload := h.buildHeartbeatAckPayload(client, agent, 2)
	assert.Equal(t, 4, payload["max_concurrent_pipelines"])
	assert.Equal(t, 7, payload["task_concurrency"])
	assert.Equal(t, []string{"https://mirror-a.example"}, payload["dockerhub_mirrors"])
}

func TestTaskLogPayload(t *testing.T) {
	payload := map[string]interface{}{
		"log_id":      float64(1),
		"task_id":     float64(100),
		"run_id":      float64(1000),
		"level":       "info",
		"message":     "Step 1: Building project...",
		"source":      "stdout",
		"line_number": float64(42),
		"timestamp":   float64(time.Now().Unix()),
	}

	msg := WebSocketMessage{
		Type:    "task_log",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "task_log", decoded.Type)
	assert.Equal(t, payload["log_id"], decoded.Payload["log_id"])
	assert.Equal(t, payload["task_id"], decoded.Payload["task_id"])
	assert.Equal(t, payload["level"], decoded.Payload["level"])
	assert.Equal(t, payload["message"], decoded.Payload["message"])
	assert.Equal(t, payload["source"], decoded.Payload["source"])
}

func TestTaskLogStreamPayload(t *testing.T) {
	payload := map[string]interface{}{
		"task_id":   float64(100),
		"run_id":    float64(1000),
		"chunk":     "building...",
		"timestamp": float64(time.Now().Unix()),
	}

	msg := WebSocketMessage{
		Type:    "task_log_stream",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "task_log_stream", decoded.Type)
	assert.Equal(t, payload["task_id"], decoded.Payload["task_id"])
	assert.Equal(t, payload["chunk"], decoded.Payload["chunk"])
}

func TestTaskStatusPayload(t *testing.T) {
	payload := map[string]interface{}{
		"task_id":    float64(100),
		"run_id":     float64(1000),
		"status":     models.TaskStatusRunning,
		"exit_code":  float64(0),
		"error_msg":  "",
		"duration":   float64(60),
		"agent_id":   float64(5),
		"agent_name": "test-agent",
		"timestamp":  float64(time.Now().Unix()),
	}

	msg := WebSocketMessage{
		Type:    "task_status",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "task_status", decoded.Type)
	assert.Equal(t, payload["task_id"], decoded.Payload["task_id"])
	assert.Equal(t, payload["status"], decoded.Payload["status"])
	assert.Equal(t, payload["agent_name"], decoded.Payload["agent_name"])
}

func TestFrontendSubscriptionPayload(t *testing.T) {
	payload := map[string]interface{}{
		"run_id":  float64(1000),
		"user_id": float64(1),
		"action":  "subscribe",
	}

	msg := WebSocketMessage{
		Type:    "subscribe",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "subscribe", decoded.Type)
	assert.Equal(t, float64(1000), decoded.Payload["run_id"])
	assert.Equal(t, float64(1), decoded.Payload["user_id"])
}

func TestRunProgressPayload(t *testing.T) {
	payload := map[string]interface{}{
		"run_id":            float64(1000),
		"status":            "running",
		"progress":          float64(45.5),
		"current_node_id":   "node-5",
		"current_node_name": "Deploy",
		"total_nodes":       float64(10),
		"completed_nodes":   float64(4),
		"start_time":        float64(time.Now().Unix() - 300),
		"elapsed_seconds":   float64(300),
	}

	msg := WebSocketMessage{
		Type:    "run_progress",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "run_progress", decoded.Type)
	assert.Equal(t, float64(1000), decoded.Payload["run_id"])
	assert.Equal(t, float64(45.5), decoded.Payload["progress"])
	assert.Equal(t, float64(10), decoded.Payload["total_nodes"])
}

func TestAgentStatusPayload(t *testing.T) {
	payload := map[string]interface{}{
		"agent_id":           float64(5),
		"agent_name":         "test-agent",
		"status":             models.AgentStatusOnline,
		"version":            "1.0.0",
		"os":                 "linux",
		"arch":               "amd64",
		"cpu_cores":          float64(8),
		"memory_total":       float64(16000000000),
		"disk_total":         float64(500000000000),
		"last_heart_at":      float64(time.Now().Unix()),
		"heartbeat_interval": float64(10),
		"labels":             "[\"linux\", \"docker\"]",
		"tags":               "{\"env\": \"prod\"}",
	}

	msg := WebSocketMessage{
		Type:    "agent_status",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "agent_status", decoded.Type)
	assert.Equal(t, payload["agent_id"], decoded.Payload["agent_id"])
	assert.Equal(t, payload["agent_name"], decoded.Payload["agent_name"])
	assert.Equal(t, payload["status"], decoded.Payload["status"])
	assert.Equal(t, payload["version"], decoded.Payload["version"])
}

func TestIsAgentOnlineSimulation(t *testing.T) {
	handler := NewWebSocketHandler()

	result := handler.IsAgentOnline(1)
	assert.False(t, result)

	handler.agentsMu.Lock()
	handler.agents[1] = nil
	handler.agentsMu.Unlock()

	result = handler.IsAgentOnline(1)
	assert.True(t, result)

	result = handler.IsAgentOnline(999)
	assert.False(t, result)

	handler.agentsMu.Lock()
	delete(handler.agents, 1)
	handler.agentsMu.Unlock()

	result = handler.IsAgentOnline(1)
	assert.False(t, result)
}

func TestMessageTypeConstants(t *testing.T) {
	msgTypes := []string{
		"heartbeat",
		"heartbeat_ack",
		"task_status",
		"task_log",
		"task_log_stream",
		"subscribe",
		"unsubscribe",
		"run_progress",
		"agent_status",
	}

	for _, msgType := range msgTypes {
		assert.NotEmpty(t, msgType)
	}

	msg := WebSocketMessage{Type: "heartbeat"}
	assert.Equal(t, "heartbeat", msg.Type)

	msg = WebSocketMessage{Type: "heartbeat_ack"}
	assert.Equal(t, "heartbeat_ack", msg.Type)

	msg = WebSocketMessage{Type: "task_status"}
	assert.Equal(t, "task_status", msg.Type)

	msg = WebSocketMessage{Type: "task_log"}
	assert.Equal(t, "task_log", msg.Type)

	msg = WebSocketMessage{Type: "task_log_stream"}
	assert.Equal(t, "task_log_stream", msg.Type)
}

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, "online", models.AgentStatusOnline)
	assert.Equal(t, "offline", models.AgentStatusOffline)
	assert.Equal(t, "busy", models.AgentStatusBusy)
	assert.Equal(t, "error", models.AgentStatusError)

	assert.Equal(t, "assigned", models.TaskStatusAssigned)
	assert.Equal(t, "dispatching", models.TaskStatusDispatching)
	assert.Equal(t, "pulling", models.TaskStatusPulling)
	assert.Equal(t, "acked", models.TaskStatusAcked)
	assert.Equal(t, "running", models.TaskStatusRunning)
	assert.Equal(t, "execute_success", models.TaskStatusExecuteSuccess)
	assert.Equal(t, "execute_failed", models.TaskStatusExecuteFailed)
	assert.Equal(t, "schedule_failed", models.TaskStatusScheduleFailed)
	assert.Equal(t, "dispatch_timeout", models.TaskStatusDispatchTimeout)
	assert.Equal(t, "lease_expired", models.TaskStatusLeaseExpired)
	assert.Equal(t, "cancel_requested", models.TaskStatusCancelRequested)
	assert.Equal(t, "cancel_requested", models.PipelineRunStatusCancelRequested)
	assert.Equal(t, "cancelled", models.TaskStatusCancelled)

	assert.Equal(t, "pending", models.AgentRegistrationStatusPending)
	assert.Equal(t, "approved", models.AgentRegistrationStatusApproved)
	assert.Equal(t, "rejected", models.AgentRegistrationStatusRejected)
}

func TestHeartbeatHistory(t *testing.T) {
	heartbeatHistory = make(map[uint64][]models.AgentHeartbeat)
	handler := NewWebSocketHandler()

	heartbeats, total := handler.GetHeartbeats(1, 1, 10)
	assert.Equal(t, 0, len(heartbeats))
	assert.Equal(t, int64(0), total)

	heartbeat := models.AgentHeartbeat{
		AgentID:      1,
		Timestamp:    time.Now().Unix(),
		CPUUsage:     50.0,
		MemoryUsage:  60.0,
		DiskUsage:    70.0,
		LoadAvg:      "1.0, 1.0, 1.0",
		TasksRunning: 2,
	}

	handler.storeHeartbeat(1, heartbeat)

	heartbeats, total = handler.GetHeartbeats(1, 1, 10)
	assert.Equal(t, 1, len(heartbeats))
	assert.Equal(t, int64(1), total)
	assert.Equal(t, uint64(1), heartbeats[0].AgentID)
	assert.Equal(t, float64(50.0), heartbeats[0].CPUUsage)

	handler.storeHeartbeat(1, models.AgentHeartbeat{
		AgentID:     1,
		Timestamp:   time.Now().Unix(),
		CPUUsage:    55.0,
		MemoryUsage: 65.0,
		DiskUsage:   75.0,
	})

	handler.storeHeartbeat(1, models.AgentHeartbeat{
		AgentID:     1,
		Timestamp:   time.Now().Unix(),
		CPUUsage:    60.0,
		MemoryUsage: 70.0,
		DiskUsage:   80.0,
	})

	heartbeats, total = handler.GetHeartbeats(1, 1, 10)
	assert.Equal(t, 3, len(heartbeats))
	assert.Equal(t, int64(3), total)
}

func TestHeartbeatHistoryPagination(t *testing.T) {
	heartbeatHistory = make(map[uint64][]models.AgentHeartbeat)
	handler := NewWebSocketHandler()

	for i := 0; i < 5; i++ {
		handler.storeHeartbeat(1, models.AgentHeartbeat{
			AgentID:   1,
			Timestamp: time.Now().Unix() + int64(i),
			CPUUsage:  float64(50 + i),
		})
	}

	heartbeats, total := handler.GetHeartbeats(1, 1, 10)
	assert.Equal(t, 5, len(heartbeats))
	assert.Equal(t, int64(5), total)

	heartbeats, total = handler.GetHeartbeats(1, 1, 2)
	assert.Equal(t, 2, len(heartbeats))
	assert.Equal(t, int64(5), total)

	heartbeats, total = handler.GetHeartbeats(1, 2, 2)
	assert.Equal(t, 2, len(heartbeats))
	assert.Equal(t, int64(5), total)

	heartbeats, total = handler.GetHeartbeats(1, 3, 2)
	assert.Equal(t, 2, len(heartbeats))
	assert.Equal(t, int64(5), total)
}

func TestHeartbeatHistoryMultipleAgents(t *testing.T) {
	heartbeatHistory = make(map[uint64][]models.AgentHeartbeat)
	handler := NewWebSocketHandler()

	handler.storeHeartbeat(1, models.AgentHeartbeat{
		AgentID:   1,
		Timestamp: time.Now().Unix(),
		CPUUsage:  50.0,
	})

	handler.storeHeartbeat(2, models.AgentHeartbeat{
		AgentID:   2,
		Timestamp: time.Now().Unix(),
		CPUUsage:  60.0,
	})

	heartbeats1, total1 := handler.GetHeartbeats(1, 1, 10)
	assert.Equal(t, 1, len(heartbeats1))
	assert.Equal(t, int64(1), total1)
	assert.Equal(t, float64(50.0), heartbeats1[0].CPUUsage)

	heartbeats2, total2 := handler.GetHeartbeats(2, 1, 10)
	assert.Equal(t, 1, len(heartbeats2))
	assert.Equal(t, int64(1), total2)
	assert.Equal(t, float64(60.0), heartbeats2[0].CPUUsage)

	heartbeats3, total3 := handler.GetHeartbeats(999, 1, 10)
	assert.Equal(t, 0, len(heartbeats3))
	assert.Equal(t, int64(0), total3)
}

func TestWsClientStructure(t *testing.T) {
	client := &wsClient{
		agentID:     1,
		lastHeartAt: time.Now().Unix(),
	}

	assert.Equal(t, uint64(1), client.agentID)
	assert.NotZero(t, client.lastHeartAt)
	assert.Nil(t, client.conn)
	assert.NotNil(t, client.mu)
}

func TestFrontendClientStructure(t *testing.T) {
	client := &frontendClient{
		runID:  "run_123",
		userID: 1,
	}

	assert.Equal(t, "run_123", client.runID)
	assert.Equal(t, uint64(1), client.userID)
	assert.Nil(t, client.conn)
	assert.NotNil(t, client.mu)
}

func TestClientIDCounter(t *testing.T) {
	handler := NewWebSocketHandler()

	assert.Equal(t, uint64(0), handler.clientIDCounter)

	handler.clientIDMu.Lock()
	handler.clientIDCounter++
	handler.clientIDMu.Unlock()
	assert.Equal(t, uint64(1), handler.clientIDCounter)

	handler.clientIDMu.Lock()
	handler.clientIDCounter++
	handler.clientIDMu.Unlock()
	assert.Equal(t, uint64(2), handler.clientIDCounter)

	handler.clientIDMu.Lock()
	handler.clientIDCounter++
	handler.clientIDMu.Unlock()
	assert.Equal(t, uint64(3), handler.clientIDCounter)
}

func TestWebSocketMessageComplexPayload(t *testing.T) {
	payload := map[string]interface{}{
		"task_id":    float64(123),
		"run_id":     float64(456),
		"status":     "execute_success",
		"exit_code":  float64(0),
		"error_msg":  "",
		"duration":   float64(300),
		"agent_id":   float64(7),
		"agent_name": "prod-agent-01",
		"timestamp":  float64(time.Now().Unix()),
		"outputs": map[string]interface{}{
			"artifact_path": "/tmp/artifact",
			"build_id":      "build_12345",
		},
	}

	msg := WebSocketMessage{
		Type:    "task_status",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var decoded WebSocketMessage
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "task_status", decoded.Type)
	assert.NotNil(t, decoded.Payload["outputs"])
	outputs := decoded.Payload["outputs"].(map[string]interface{})
	assert.Equal(t, "/tmp/artifact", outputs["artifact_path"])
	assert.Equal(t, "build_12345", outputs["build_id"])
}

func TestLogLevelConstants(t *testing.T) {
	logLevels := []string{"debug", "info", "warn", "error"}

	for _, level := range logLevels {
		payload := map[string]interface{}{
			"level":   level,
			"message": "test message",
			"task_id": float64(1),
			"run_id":  float64(1),
		}

		msg := WebSocketMessage{
			Type:    "task_log",
			Payload: payload,
		}

		data, err := json.Marshal(msg)
		assert.NoError(t, err)

		var decoded WebSocketMessage
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)
		assert.Equal(t, level, decoded.Payload["level"])
	}
}

func TestTriggerDownstreamTasks_VariableSubstitution(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	runConfig, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Upstream", Config: map[string]interface{}{"script": "echo upstream"}},
			{ID: "node_2", Type: "shell", Name: "Downstream", Config: map[string]interface{}{
				"script": "echo Commit: ${outputs.node_1.git_commit}",
			}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_2"}},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(runConfig),
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	upstreamTask := &models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteSuccess,
		ExitCode:      0,
		Duration:      2,
		ResultData:    `{"git_commit": "abc123def"}`,
	}
	if err := db.Create(&upstreamTask).Error; err != nil {
		t.Fatalf("create upstream task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{*upstreamTask})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_2").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}

	if !strings.Contains(downstream.Script, "abc123def") {
		t.Fatalf("expected downstream script to contain substituted git_commit 'abc123def', got: %s", downstream.Script)
	}
	if strings.Contains(downstream.Script, "${outputs.node_1.git_commit}") {
		t.Fatalf("expected downstream script to NOT contain unresolved variable, got: %s", downstream.Script)
	}
}

func TestTriggerDownstreamTasks_VariableSubstitutionNoResultData(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	runConfig, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Upstream", Config: map[string]interface{}{"script": "echo upstream"}},
			{ID: "node_2", Type: "shell", Name: "Downstream", Config: map[string]interface{}{
				"script": "echo Commit: ${outputs.node_1.git_commit}",
			}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_2"}},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(runConfig),
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	upstreamTask := &models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteSuccess,
		ExitCode:      0,
		Duration:      2,
		ResultData:    "",
	}
	if err := db.Create(&upstreamTask).Error; err != nil {
		t.Fatalf("create upstream task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{*upstreamTask})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_2").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}

	if !strings.Contains(downstream.Script, "${outputs.node_1.git_commit}") {
		t.Fatalf("expected downstream script to contain unresolved variable when no result data, got: %s", downstream.Script)
	}
}

func TestCheckAndUpdatePipelineStatus_IgnoresNodeConfiguredFailure(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "build", Type: "shell", Name: "Build", IgnoreFailure: true, Config: map[string]interface{}{"script": "exit 1"}},
			{ID: "deploy", Type: "shell", Name: "Deploy", Config: map[string]interface{}{"script": "echo deploy"}},
		},
		Edges: []PipelineEdge{{From: "build", To: "deploy"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(pipelineSnapshot),
		AgentID:          1,
		StartTime:        time.Now().Add(-5 * time.Second).Unix(),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	failedTask := models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "build",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteFailed,
		ErrorMsg:      "build failed",
	}
	if err := db.Create(&failedTask).Error; err != nil {
		t.Fatalf("create failed task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{failedTask})
	handler.checkAndUpdatePipelineStatus(run.ID)

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "deploy").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created before status evaluation: %v", err)
	}

	var reloaded models.PipelineRun
	if err := db.First(&reloaded, run.ID).Error; err != nil {
		t.Fatalf("reload pipeline run failed: %v", err)
	}
	if reloaded.Status != models.PipelineRunStatusRunning {
		t.Fatalf("pipeline status=%s, want %s", reloaded.Status, models.PipelineRunStatusRunning)
	}
}

func TestTriggerDownstreamTasks_AllowsNodeConfiguredFailureToUnblockDownstream(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "build", Type: "shell", Name: "Build", IgnoreFailure: true, Config: map[string]interface{}{"script": "exit 1"}},
			{ID: "deploy", Type: "shell", Name: "Deploy", Config: map[string]interface{}{"script": "echo deploy"}},
		},
		Edges: []PipelineEdge{{From: "build", To: "deploy"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		PipelineSnapshot: string(pipelineSnapshot),
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	failedTask := models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "build",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteFailed,
		ErrorMsg:      "build failed",
	}
	if err := db.Create(&failedTask).Error; err != nil {
		t.Fatalf("create failed task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{failedTask})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "deploy").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created despite upstream failure on ignored node: %v", err)
	}
}

func TestTriggerDownstreamTasks_UsesRunRecordOutputsAsPrimaryTruth(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	runConfig, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "shell", Name: "Upstream", Config: map[string]interface{}{"script": "echo upstream"}},
			{ID: "node_2", Type: "shell", Name: "Downstream", Config: map[string]interface{}{
				"script": "echo Commit: ${outputs.node_1.git_commit}",
			}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_2"}},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		Config:           "{invalid-json",
		PipelineSnapshot: string(runConfig),
		Outputs:          `{"node_1":{"git_commit":"run-record-commit","exit_code":0,"status":"execute_success"}}`,
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	upstreamTask := &models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteSuccess,
		ExitCode:      0,
		Duration:      2,
		ResultData:    `{"git_commit": "task-row-commit"}`,
	}
	if err := db.Create(&upstreamTask).Error; err != nil {
		t.Fatalf("create upstream task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{*upstreamTask})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_2").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}

	if !strings.Contains(downstream.Script, "run-record-commit") {
		t.Fatalf("expected downstream script to contain run-record commit, got: %s", downstream.Script)
	}
	if strings.Contains(downstream.Script, "task-row-commit") {
		t.Fatalf("expected downstream script to ignore task-row commit, got: %s", downstream.Script)
	}
}

func TestTriggerDownstreamTasks_ReappliesManualRuntimeInputsFromRunConfig(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	pipelineSnapshot, err := json.Marshal(PipelineConfig{
		Version: "2.0",
		Nodes: []PipelineNode{
			{ID: "node_1", Type: "git_clone", Name: "Clone", Config: map[string]interface{}{"git_repo_url": "https://example.com/repo.git", "git_ref": "main", "git_checkout_path": "./app"}},
			{ID: "node_2", Type: "docker", Name: "Build", Config: map[string]interface{}{"image_name": "demo/app", "image_tag": "latest", "dockerfile": "Dockerfile", "context": ".", "push": false, "pre_build_script": "cd ${outputs.node_1.git_checkout_path};"}},
		},
		Edges: []PipelineEdge{{From: "node_1", To: "node_2"}},
	})
	if err != nil {
		t.Fatalf("marshal pipeline snapshot failed: %v", err)
	}

	runConfigJSON, err := json.Marshal(models.PipelineRunConfigSnapshot{
		Inputs: map[string]map[string]interface{}{
			"node_2": {
				"image_tag": "hk_latest",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal run config failed: %v", err)
	}

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		RunConfig:        string(runConfigJSON),
		PipelineSnapshot: string(pipelineSnapshot),
		Outputs:          `{"node_1":{"git_checkout_path":"./app","exit_code":0,"status":"execute_success"}}`,
		AgentID:          1,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	upstreamTask := &models.AgentTask{
		WorkspaceID:   1,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "git_clone",
		Status:        models.TaskStatusExecuteSuccess,
		ExitCode:      0,
		Duration:      2,
		ResultData:    `{"git_checkout_path":"./app"}`,
	}
	if err := db.Create(&upstreamTask).Error; err != nil {
		t.Fatalf("create upstream task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.triggerDownstreamTasks(run.ID, []models.AgentTask{*upstreamTask})

	var downstream models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, "node_2").First(&downstream).Error; err != nil {
		t.Fatalf("expected downstream task to be created: %v", err)
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(downstream.Params), &params); err != nil {
		t.Fatalf("unmarshal downstream params failed: %v", err)
	}
	if got := fmt.Sprint(params["image_tag"]); got != "hk_latest" {
		t.Fatalf("downstream image_tag=%q, want hk_latest; params=%s", got, downstream.Params)
	}
}

func TestHandleTaskUpdateV2_PersistsRunRecordOutputsForCompletedTask(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		Config:           `{"version":"2.0","nodes":[{"id":"node_1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
		PipelineSnapshot: `{"version":"2.0","nodes":[{"id":"node_1","type":"shell","name":"Build","config":{"script":"echo hi"}}],"edges":[]}`,
		ResolvedNodes:    `[]`,
		Outputs:          `{}`,
		AgentID:          9,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{
		WorkspaceID:   1,
		AgentID:       9,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		TaskType:      "shell",
		Name:          "Build",
		Params:        `{"script":"echo hi"}`,
		Status:        models.TaskStatusRunning,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: 9, sessionID: "session-1"}
	handler.handleTaskUpdateV2(client, &models.Agent{BaseModel: models.BaseModel{ID: 9}, Name: "worker-1"}, map[string]interface{}{
		"task_id":         float64(task.ID),
		"attempt":         float64(1),
		"status":          models.TaskStatusExecuteSuccess,
		"exit_code":       float64(0),
		"duration_ms":     float64(1200),
		"idempotency_key": "task-success-outputs",
		"result": map[string]interface{}{
			"git_commit": "abc123def",
			"artifact":   "demo.tar",
		},
	})

	var updatedRun models.PipelineRun
	if err := db.First(&updatedRun, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if !strings.Contains(updatedRun.Outputs, `"node_1"`) || !strings.Contains(updatedRun.Outputs, `"git_commit":"abc123def"`) {
		t.Fatalf("expected run outputs_json updated from task result, got=%s", updatedRun.Outputs)
	}
}

func TestTaskUpdateV2_CancelRequestedConvergesToCancelled(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusCancelRequested,
		Config:           `{"version":"2.0","nodes":[{"id":"node_cancel","type":"shell","name":"Cancelable","config":{"script":"sleep 30"}}],"edges":[]}`,
		PipelineSnapshot: `{"version":"2.0","nodes":[{"id":"node_cancel","type":"shell","name":"Cancelable","config":{"script":"sleep 30"}}],"edges":[]}`,
		ResolvedNodes:    `[]`,
		Outputs:          `{}`,
		AgentID:          9,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	startTime := time.Now().Add(-5 * time.Second).Unix()
	task := models.AgentTask{
		WorkspaceID:    1,
		AgentID:        9,
		PipelineRunID:  run.ID,
		NodeID:         "node_cancel",
		TaskType:       "shell",
		Name:           "Cancelable",
		Params:         `{"script":"sleep 30"}`,
		Status:         models.TaskStatusCancelRequested,
		AgentSessionID: "session-1",
		StartTime:      startTime,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: 9, sessionID: "session-1"}
	handler.handleTaskUpdateV2(client, &models.Agent{BaseModel: models.BaseModel{ID: 9}, Name: "worker-1"}, map[string]interface{}{
		"task_id":         float64(task.ID),
		"attempt":         float64(1),
		"status":          models.TaskStatusCancelled,
		"exit_code":       float64(130),
		"error_msg":       "task cancelled",
		"duration_ms":     float64(4200),
		"idempotency_key": "task-cancelled-terminal",
		"result": map[string]interface{}{
			"stdout": "partial output",
		},
	})

	var reloaded models.AgentTask
	if err := db.First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloaded.Status != models.TaskStatusCancelled {
		t.Fatalf("task status=%s, want %s", reloaded.Status, models.TaskStatusCancelled)
	}
	if reloaded.EndTime == 0 {
		t.Fatal("expected cancelled task end_time to be set")
	}
	if reloaded.Duration != 4 {
		t.Fatalf("duration=%d, want 4", reloaded.Duration)
	}
	if reloaded.ExitCode != 130 {
		t.Fatalf("exit_code=%d, want 130", reloaded.ExitCode)
	}
	if reloaded.ErrorMsg != "task cancelled" {
		t.Fatalf("error_msg=%q, want task cancelled", reloaded.ErrorMsg)
	}
	if reloaded.EndTime < reloaded.StartTime {
		t.Fatalf("end_time=%d before start_time=%d", reloaded.EndTime, reloaded.StartTime)
	}

	var execution models.TaskExecution
	if err := db.Where("task_id = ? AND attempt = ?", task.ID, 1).First(&execution).Error; err != nil {
		t.Fatalf("load task execution failed: %v", err)
	}
	if execution.Status != models.TaskStatusCancelled {
		t.Fatalf("execution status=%s, want %s", execution.Status, models.TaskStatusCancelled)
	}
	if execution.Duration != 4 {
		t.Fatalf("execution duration=%d, want 4", execution.Duration)
	}
	if execution.ExitCode != 130 {
		t.Fatalf("execution exit_code=%d, want 130", execution.ExitCode)
	}

	var event models.AgentTaskEvent
	if err := db.Where("task_id = ? AND attempt = ? AND idempotency_key = ?", task.ID, 1, "task-cancelled-terminal").First(&event).Error; err != nil {
		t.Fatalf("load task event failed: %v", err)
	}
	if event.Status != models.TaskStatusCancelled {
		t.Fatalf("event status=%s, want %s", event.Status, models.TaskStatusCancelled)
	}
	if event.ExitCode != 130 {
		t.Fatalf("event exit_code=%d, want 130", event.ExitCode)
	}

	var updatedRun models.PipelineRun
	if err := db.First(&updatedRun, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if updatedRun.Status != models.PipelineRunStatusCancelled {
		t.Fatalf("run status=%s, want %s", updatedRun.Status, models.PipelineRunStatusCancelled)
	}
	if updatedRun.EndTime == 0 {
		t.Fatal("expected cancelled run end_time to be set")
	}

	type runEventRecord struct {
		EventType string                 `json:"event_type"`
		Payload   map[string]interface{} `json:"payload"`
	}
	var events []runEventRecord
	if err := json.Unmarshal([]byte(updatedRun.Events), &events); err != nil {
		t.Fatalf("unmarshal run events failed: %v raw=%s", err, updatedRun.Events)
	}
	foundCancelledEvent := false
	for _, event := range events {
		if event.EventType != "node_cancelled" {
			continue
		}
		if got := uint64(event.Payload["task_id"].(float64)); got != task.ID {
			t.Fatalf("node_cancelled task_id=%d, want %d", got, task.ID)
		}
		foundCancelledEvent = true
	}
	if !foundCancelledEvent {
		t.Fatalf("expected node_cancelled event in run events, got=%s", updatedRun.Events)
	}
}

func TestTaskUpdateV2_BlockingFailureWaitsForCancelRequestedSibling(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	run := models.PipelineRun{
		WorkspaceID:      1,
		PipelineID:       1,
		BuildNumber:      1,
		Status:           models.PipelineRunStatusRunning,
		Config:           `{"version":"2.0","nodes":[{"id":"node_fail","type":"shell","name":"Failing","config":{"script":"exit 1"}},{"id":"node_cancel","type":"shell","name":"Cancelable","config":{"script":"sleep 30"}}],"edges":[]}`,
		PipelineSnapshot: `{"version":"2.0","nodes":[{"id":"node_fail","type":"shell","name":"Failing","config":{"script":"exit 1"}},{"id":"node_cancel","type":"shell","name":"Cancelable","config":{"script":"sleep 30"}}],"edges":[]}`,
		ResolvedNodes:    `[]`,
		Outputs:          `{}`,
		AgentID:          9,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	failedTask := models.AgentTask{
		WorkspaceID:   1,
		AgentID:       9,
		PipelineRunID: run.ID,
		NodeID:        "node_fail",
		TaskType:      "shell",
		Name:          "Failing",
		Status:        models.TaskStatusRunning,
		RetryCount:    3,
		MaxRetries:    3,
		StartTime:     time.Now().Add(-5 * time.Second).Unix(),
	}
	cancelledSibling := models.AgentTask{
		WorkspaceID:   1,
		AgentID:       9,
		PipelineRunID: run.ID,
		NodeID:        "node_cancel",
		TaskType:      "shell",
		Name:          "Cancelable",
		Status:        models.TaskStatusRunning,
		RetryCount:    3,
		MaxRetries:    3,
		StartTime:     time.Now().Add(-5 * time.Second).Unix(),
	}
	if err := db.Create(&failedTask).Error; err != nil {
		t.Fatalf("create failed task failed: %v", err)
	}
	if err := db.Create(&cancelledSibling).Error; err != nil {
		t.Fatalf("create sibling task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	client := &wsClient{agentID: 9, sessionID: "session-1"}
	handler.handleTaskUpdateV2(client, &models.Agent{BaseModel: models.BaseModel{ID: 9}, Name: "worker-1"}, map[string]interface{}{
		"task_id":         float64(failedTask.ID),
		"attempt":         float64(1),
		"status":          models.TaskStatusExecuteFailed,
		"exit_code":       float64(1),
		"error_msg":       "boom",
		"duration_ms":     float64(1200),
		"idempotency_key": "task-failed-terminal",
	})

	var updatedRun models.PipelineRun
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := db.First(&updatedRun, run.ID).Error; err != nil {
			t.Fatalf("reload run failed: %v", err)
		}
		if updatedRun.Status == models.PipelineRunStatusCancelRequested {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run status=%s, want %s", updatedRun.Status, models.PipelineRunStatusCancelRequested)
		}
		time.Sleep(10 * time.Millisecond)
	}

	var siblingReloaded models.AgentTask
	if err := db.First(&siblingReloaded, cancelledSibling.ID).Error; err != nil {
		t.Fatalf("reload sibling task failed: %v", err)
	}
	if siblingReloaded.Status != models.TaskStatusCancelRequested {
		t.Fatalf("sibling status=%s, want %s", siblingReloaded.Status, models.TaskStatusCancelRequested)
	}

	handler.handleTaskUpdateV2(client, &models.Agent{BaseModel: models.BaseModel{ID: 9}, Name: "worker-1"}, map[string]interface{}{
		"task_id":         float64(cancelledSibling.ID),
		"attempt":         float64(1),
		"status":          models.TaskStatusCancelled,
		"exit_code":       float64(130),
		"error_msg":       "task cancelled",
		"duration_ms":     float64(3200),
		"idempotency_key": "task-cancelled-after-failure",
	})

	deadline = time.Now().Add(2 * time.Second)
	for {
		if err := db.First(&updatedRun, run.ID).Error; err != nil {
			t.Fatalf("reload final run failed: %v", err)
		}
		if updatedRun.Status == models.PipelineRunStatusFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("final run status=%s, want %s", updatedRun.Status, models.PipelineRunStatusFailed)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if updatedRun.EndTime == 0 {
		t.Fatal("expected failed run end_time to be set")
	}
}

func TestTaskUpdateV2_AcksBeforeSlowPostPersistWork(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{
		Name:               "worker-1",
		Host:               "host-a",
		Port:               1,
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-ok",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	task := models.AgentTask{
		WorkspaceID:     1,
		AgentID:         agent.ID,
		AgentSessionID:  "",
		PipelineRunID:   1,
		NodeID:          "node_1",
		TaskType:        "shell",
		Name:            "Build",
		Params:          `{"script":"echo hi"}`,
		Status:          models.TaskStatusAcked,
		DispatchAttempt: 1,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	slowUpdateStarted := make(chan struct{}, 1)
	releaseSlowUpdate := make(chan struct{})
	callbackName := "test:slow-agent-task-update"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "agent_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		status, _ := updates["status"].(string)
		if status != models.TaskStatusRunning {
			return
		}
		select {
		case slowUpdateStarted <- struct{}{}:
		default:
		}
		<-releaseSlowUpdate
	}); err != nil {
		t.Fatalf("register update callback failed: %v", err)
	}
	defer db.Callback().Update().Remove(callbackName)

	handler := NewWebSocketHandler()
	server := newAgentWSTestServer(t, handler)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, fmt.Sprintf("?agent_id=%d&token=%s", agent.ID, agent.Token)), nil)
	if err != nil {
		t.Fatalf("dial agent websocket failed: %v", err)
	}
	defer conn.Close()

	heartbeat := WebSocketMessage{
		Type: "heartbeat",
		Payload: map[string]interface{}{
			"agent_id":  agent.ID,
			"timestamp": time.Now().Unix(),
		},
	}
	heartbeatData, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("marshal heartbeat failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, heartbeatData); err != nil {
		t.Fatalf("write heartbeat failed: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set heartbeat read deadline failed: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read heartbeat ack failed: %v", err)
	}
	conn.SetReadDeadline(time.Time{})
	var heartbeatAck WebSocketMessage
	if err := json.Unmarshal(raw, &heartbeatAck); err != nil {
		t.Fatalf("unmarshal heartbeat ack failed: %v", err)
	}
	if heartbeatAck.Type != "heartbeat_ack" {
		t.Fatalf("expected heartbeat_ack, got %s", heartbeatAck.Type)
	}

	taskUpdate := WebSocketMessage{
		Type: "task_update_v2",
		Payload: map[string]interface{}{
			"task_id":         task.ID,
			"attempt":         1,
			"status":          models.TaskStatusRunning,
			"exit_code":       0,
			"duration_ms":     0,
			"idempotency_key": fmt.Sprintf("%d:1:running:0", task.ID),
		},
	}
	data, err := json.Marshal(taskUpdate)
	if err != nil {
		t.Fatalf("marshal task update failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write task update failed: %v", err)
	}

	select {
	case <-slowUpdateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected slow agent_tasks update to start")
	}

	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, raw, err = conn.ReadMessage()
	if err != nil {
		close(releaseSlowUpdate)
		t.Fatalf("expected ack_v2 before slow post-persist work finished: %v", err)
	}
	conn.SetReadDeadline(time.Time{})

	var ack WebSocketMessage
	if err := json.Unmarshal(raw, &ack); err != nil {
		close(releaseSlowUpdate)
		t.Fatalf("unmarshal ack failed: %v", err)
	}
	if ack.Type != "ack_v2" {
		close(releaseSlowUpdate)
		t.Fatalf("expected ack_v2, got %s", ack.Type)
	}
	if ack.Payload["event"] != "task_update_v2" {
		close(releaseSlowUpdate)
		t.Fatalf("expected ack for task_update_v2, got %#v", ack.Payload)
	}
	if ok, _ := ack.Payload["ok"].(bool); !ok {
		close(releaseSlowUpdate)
		t.Fatalf("expected successful ack payload, got %#v", ack.Payload)
	}

	close(releaseSlowUpdate)
}

// Regression test: frontend WebSocket connections must stay open beyond 60 seconds.
// Before the fix, handleFrontendMessages set a 60-second read deadline on each
// ReadMessage call, causing the connection to be closed after 60 seconds of inactivity
// even though the frontend is a receive-only connection that doesn't send messages.
type recordingConn struct {
	net.Conn
	mu                  sync.Mutex
	readDeadlineRecords []readDeadlineRecord
}

type readDeadlineRecord struct {
	time  time.Time
	stack []string
}

func (c *recordingConn) SetReadDeadline(t time.Time) error {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	stack := make([]string, 0, n)
	for {
		frame, more := frames.Next()
		stack = append(stack, frame.Function)
		if !more {
			break
		}
	}

	c.mu.Lock()
	c.readDeadlineRecords = append(c.readDeadlineRecords, readDeadlineRecord{time: t, stack: stack})
	c.mu.Unlock()
	return c.Conn.SetReadDeadline(t)
}

func (c *recordingConn) readDeadlineCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.readDeadlineRecords)
}

func (c *recordingConn) frontendSetReadDeadlineCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, record := range c.readDeadlineRecords {
		for _, fn := range record.stack {
			if strings.Contains(fn, "handleFrontendMessages") {
				count++
				break
			}
		}
	}
	return count
}

type recordingListener struct {
	net.Listener
	accepted chan *recordingConn
}

func (l *recordingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	recorded := &recordingConn{Conn: conn}
	select {
	case l.accepted <- recorded:
	default:
	}
	return recorded, nil
}

func TestFrontendWebSocket_DoesNotSetReadDeadlineForReceiveOnlyClients(t *testing.T) {
	t.Setenv("JWT_SECRET", "ws-longevity-test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	})

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	config.Init()
	config.Config.Set("server.id", "ws-longevity-test-server")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	config.Config.Set("server.internal_token", "ws-longevity-internal-token")

	wsHandler := NewWebSocketHandler()
	userHandler := &UserHandler{DB: db}

	loginRouter := gin.New()
	loginRouter.POST("/api/auth/login", userHandler.Login)
	loginServer := httptest.NewServer(loginRouter)
	defer loginServer.Close()

	wsRouter := gin.New()
	wsRouter.GET("/ws/frontend/pipeline", wsHandler.HandleFrontendConnection)
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	recording := &recordingListener{Listener: baseListener, accepted: make(chan *recordingConn, 1)}
	wsServer := &http.Server{Handler: wsRouter}
	go func() { _ = wsServer.Serve(recording) }()
	defer func() {
		_ = wsServer.Close()
		_ = recording.Close()
	}()

	user := models.User{Username: "ws-longevity-user", Status: "active", Email: "ws-longevity@example.com"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "ws-longevity-workspace", Slug: "ws-longevity-" + strconv.FormatUint(user.ID, 10), CreatedBy: user.ID, Status: "active"}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: "owner", Status: "active"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	loginBody := map[string]string{"username": "ws-longevity-user", "password": "1qaz2WSX"}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginW.Body.String())
	}
	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	if loginResp.Data.Token == "" {
		t.Fatalf("no token received")
	}

	run := models.PipelineRun{WorkspaceID: workspace.ID, Status: models.PipelineRunStatusRunning, Config: `{"version":"2.0","nodes":[],"edges":[]}`, TriggerType: "manual", TriggerUserID: user.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	wsURL := "ws://" + recording.Addr().String() + "/ws/frontend/pipeline?run_id=" + strconv.FormatUint(run.ID, 10) + "&token=" + loginResp.Data.Token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	var accepted *recordingConn
	select {
	case accepted = <-recording.accepted:
	case <-time.After(1 * time.Second):
		t.Fatal("expected websocket server connection")
	}

	deadline := time.After(500 * time.Millisecond)
	for {
		if accepted.frontendSetReadDeadlineCalls() > 0 {
			break
		}
		select {
		case <-deadline:
			goto assertFrontendDeadlines
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

assertFrontendDeadlines:
	if got := accepted.frontendSetReadDeadlineCalls(); got != 0 {
		t.Fatalf("expected frontend websocket handler to avoid SetReadDeadline, got %d calls", got)
	}
}

// Regression test: when a frontend WebSocket client disconnects, the server must
// properly clean up: remove the client from the frontends map, close the connection,
// and stop the run watcher when no more clients are subscribed to the run.
func TestFrontendWebSocket_ClientDisconnect_CleansUpResources(t *testing.T) {
	t.Setenv("JWT_SECRET", "ws-disconnect-test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	})

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	config.Init()
	config.Config.Set("server.id", "ws-disconnect-test-server")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	config.Config.Set("server.internal_token", "ws-disconnect-internal-token")

	wsHandler := NewWebSocketHandler()
	userHandler := &UserHandler{DB: db}

	router := gin.New()
	router.GET("/ws/frontend/pipeline", wsHandler.HandleFrontendConnection)
	auth := router.Group("/api/auth")
	auth.POST("/login", userHandler.Login)

	server := httptest.NewServer(router)
	defer server.Close()

	user := models.User{
		Username: "ws-disconnect-user",
		Status:   "active",
		Email:    "ws-disconnect@example.com",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace := models.Workspace{
		Name:      "ws-disconnect-workspace",
		Slug:      "ws-disconnect-" + strconv.FormatUint(user.ID, 10),
		CreatedBy: user.ID,
		Status:    "active",
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        "owner",
		Status:      "active",
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	loginBody := map[string]string{"username": "ws-disconnect-user", "password": "1qaz2WSX"}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginW.Body.String())
	}

	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	token := loginResp.Data.Token
	if token == "" {
		t.Fatalf("no token received")
	}

	run := models.PipelineRun{
		WorkspaceID:   workspace.ID,
		Status:        models.PipelineRunStatusRunning,
		Config:        `{"version":"2.0","nodes":[],"edges":[]}`,
		TriggerType:   "manual",
		TriggerUserID: user.ID,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	runIDStr := strconv.FormatUint(run.ID, 10)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/frontend/pipeline?run_id=" + runIDStr + "&token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (resp=%v)", err, resp)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	// Wait for server goroutine to register the client
	time.Sleep(100 * time.Millisecond)

	wsHandler.frontendsMu.RLock()
	runClients, runExists := wsHandler.frontends[runIDStr]
	wsHandler.frontendsMu.RUnlock()
	if !runExists {
		t.Fatalf("expected frontends[%s] to exist after client connected", runIDStr)
	}
	if len(runClients) != 1 {
		t.Fatalf("expected exactly 1 client in frontends[%s], got %d", runIDStr, len(runClients))
	}

	wsHandler.runWatchersMu.Lock()
	_, watcherExists := wsHandler.runWatchers[run.ID]
	wsHandler.runWatchersMu.Unlock()
	if !watcherExists {
		t.Fatalf("expected runWatcher for run %d to exist after client connected", run.ID)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("conn.Close() failed: %v", err)
	}

	// Wait for deferred cleanup in handleFrontendMessages to process the disconnect
	time.Sleep(500 * time.Millisecond)

	wsHandler.frontendsMu.RLock()
	_, runExists = wsHandler.frontends[runIDStr]
	wsHandler.frontendsMu.RUnlock()
	if runExists {
		wsHandler.frontendsMu.RLock()
		remaining := wsHandler.frontends[runIDStr]
		wsHandler.frontendsMu.RUnlock()
		t.Fatalf("expected frontends[%s] removed after client disconnected, but still has %d clients", runIDStr, len(remaining))
	}

	wsHandler.runWatchersMu.Lock()
	_, watcherExists = wsHandler.runWatchers[run.ID]
	wsHandler.runWatchersMu.Unlock()
	if watcherExists {
		t.Fatalf("expected runWatcher for run %d stopped after last client disconnected", run.ID)
	}
}

func TestFrontendWebSocket_ReceivesRunTaskAndLogEvents(t *testing.T) {
	t.Setenv("JWT_SECRET", "ws-event-stream-test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	})

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	config.Init()
	config.Config.Set("server.id", "ws-event-stream-test-server")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	config.Config.Set("server.internal_token", "ws-event-stream-internal-token")

	wsHandler := NewWebSocketHandler()
	userHandler := &UserHandler{DB: db}

	router := gin.New()
	router.GET("/ws/frontend/pipeline", wsHandler.HandleFrontendConnection)
	auth := router.Group("/api/auth")
	auth.POST("/login", userHandler.Login)

	server := httptest.NewServer(router)
	defer server.Close()

	user := models.User{
		Username: "ws-event-user",
		Status:   "active",
		Email:    "ws-event@example.com",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace := models.Workspace{
		Name:      "ws-event-workspace",
		Slug:      "ws-event-" + strconv.FormatUint(user.ID, 10),
		CreatedBy: user.ID,
		Status:    "active",
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        "owner",
		Status:      "active",
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	loginBody := map[string]string{"username": "ws-event-user", "password": "1qaz2WSX"}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginW.Body.String())
	}

	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	token := loginResp.Data.Token
	if token == "" {
		t.Fatalf("no token received")
	}

	run := models.PipelineRun{
		WorkspaceID:   workspace.ID,
		Status:        models.PipelineRunStatusRunning,
		Config:        `{"version":"2.0","nodes":[],"edges":[]}`,
		TriggerType:   "manual",
		TriggerUserID: user.ID,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	task := models.AgentTask{
		WorkspaceID:   workspace.ID,
		PipelineRunID: run.ID,
		NodeID:        "node_1",
		Name:          "Realtime Build",
		TaskType:      "shell",
		Status:        models.TaskStatusQueued,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create agent task failed: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/frontend/pipeline?run_id=" + strconv.FormatUint(run.ID, 10) + "&token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	// Give the frontend client registration path time to add the subscription.
	time.Sleep(150 * time.Millisecond)

	wsHandler.BroadcastTaskStatus(run.ID, task.ID, task.NodeID, models.TaskStatusRunning, 0, "", "Agent #1")
	_, err = appendTaskLogChunk(wsHandler, task, taskLogChunkPayloadV2{
		TaskID:    task.ID,
		Attempt:   1,
		Seq:       1,
		Level:     "info",
		Stream:    "stdout",
		Chunk:     "first-line",
		Timestamp: time.Now().Unix(),
	}, 1, "session-1")
	if err != nil {
		t.Fatalf("append task log chunk failed: %v", err)
	}
	wsHandler.BroadcastRunStatus(run.ID, models.PipelineRunStatusSuccess, "", 3)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	seenTypes := make(map[string]WebSocketMessage)
	for len(seenTypes) < 3 {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket message failed: %v", err)
		}

		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal websocket message failed: %v", err)
		}

		switch msg.Type {
		case "task_status":
			if getInt64(msg.Payload, "task_id") == int64(task.ID) && getString(msg.Payload, "status") == models.TaskStatusRunning {
				seenTypes[msg.Type] = msg
			}
		case "task_log":
			if getInt64(msg.Payload, "task_id") == int64(task.ID) && getString(msg.Payload, "message") == "first-line" {
				seenTypes[msg.Type] = msg
			}
		case "run_status":
			if getInt64(msg.Payload, "run_id") == int64(run.ID) && getString(msg.Payload, "status") == models.PipelineRunStatusSuccess {
				seenTypes[msg.Type] = msg
			}
		}
	}

	if _, ok := seenTypes["task_status"]; !ok {
		t.Fatalf("expected task_status websocket event")
	}
	if _, ok := seenTypes["task_log"]; !ok {
		t.Fatalf("expected task_log websocket event")
	}
	if _, ok := seenTypes["run_status"]; !ok {
		t.Fatalf("expected run_status websocket event")
	}
}

func TestFrontendWebSocket_InitialTaskSnapshotIncludesOutputs(t *testing.T) {
	t.Setenv("JWT_SECRET", "ws-initial-snapshot-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	})

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	config.Init()
	config.Config.Set("server.id", "ws-initial-snapshot-server")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	config.Config.Set("server.internal_token", "ws-initial-snapshot-token")

	wsHandler := NewWebSocketHandler()
	userHandler := &UserHandler{DB: db}

	router := gin.New()
	router.GET("/ws/frontend/pipeline", wsHandler.HandleFrontendConnection)
	auth := router.Group("/api/auth")
	auth.POST("/login", userHandler.Login)

	server := httptest.NewServer(router)
	defer server.Close()

	user := models.User{
		Username: "ws-initial-snapshot-user",
		Status:   "active",
		Email:    "ws-initial-snapshot@example.com",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace := models.Workspace{
		Name:      "ws-initial-snapshot-workspace",
		Slug:      "ws-initial-snapshot-" + strconv.FormatUint(user.ID, 10),
		CreatedBy: user.ID,
		Status:    "active",
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        "owner",
		Status:      "active",
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	loginBody := map[string]string{"username": "ws-initial-snapshot-user", "password": "1qaz2WSX"}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginW.Body.String())
	}

	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	token := loginResp.Data.Token
	if token == "" {
		t.Fatalf("no token received")
	}

	run := models.PipelineRun{
		WorkspaceID:   workspace.ID,
		Status:        models.PipelineRunStatusRunning,
		Config:        `{"version":"2.0","nodes":[],"edges":[]}`,
		TriggerType:   "manual",
		TriggerUserID: user.ID,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	task := models.AgentTask{
		WorkspaceID:   workspace.ID,
		PipelineRunID: run.ID,
		NodeID:        "node_output",
		Name:          "Output Task",
		TaskType:      "shell",
		Status:        models.TaskStatusExecuteSuccess,
		ExitCode:      0,
		Duration:      7,
		ResultData:    `{"artifact":"bundle.tgz","commit_sha":"abc123"}`,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create agent task failed: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/frontend/pipeline?run_id=" + strconv.FormatUint(run.ID, 10) + "&token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket message failed: %v", err)
		}

		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal websocket message failed: %v", err)
		}

		if msg.Type != "task_status" {
			continue
		}
		if getInt64(msg.Payload, "task_id") != int64(task.ID) {
			continue
		}

		outputs, ok := msg.Payload["outputs"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected task_status payload to include outputs map, got %#v", msg.Payload["outputs"])
		}
		if outputs["artifact"] != "bundle.tgz" {
			t.Fatalf("expected artifact output in task_status payload, got %#v", outputs)
		}
		if outputs["commit_sha"] != "abc123" {
			t.Fatalf("expected commit_sha output in task_status payload, got %#v", outputs)
		}
		break
	}
}

func TestSendControlMessageToAgent_FallsBackToCrossReplicaRelay(t *testing.T) {
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	})

	handler := NewWebSocketHandler()
	agentID := uint64(901)
	targetServerID := handler.serverID + "-remote"
	if err := utils.PutAgentPresence(context.Background(), utils.AgentPresence{
		AgentID:           agentID,
		AgentSessionID:    "session-cross-replica",
		ServerID:          targetServerID,
		ServerURL:         "http://remote-server:8080",
		HeartbeatInterval: 10,
	}); err != nil {
		t.Fatalf("put agent presence failed: %v", err)
	}

	pubsub := utils.RedisClient.Subscribe(context.Background(), utils.ControlRelayTopic(targetServerID))
	defer func() { _ = pubsub.Close() }()
	_, err = pubsub.Receive(context.Background())
	if err != nil {
		t.Fatalf("subscribe relay topic failed: %v", err)
	}

	payload := map[string]interface{}{
		"task_id": float64(88),
		"run_id":  float64(19),
	}
	if ok := handler.sendControlMessageToAgent(agentID, "task_cancel", payload); !ok {
		t.Fatalf("expected cross-replica relay delivery to succeed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	relayMsg, err := pubsub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("receive relay message failed: %v", err)
	}

	envelope := controlRelayEnvelope{}
	if err := json.Unmarshal([]byte(relayMsg.Payload), &envelope); err != nil {
		t.Fatalf("unmarshal relay envelope failed: %v", err)
	}
	if envelope.AgentID != agentID {
		t.Fatalf("relay agent_id=%d, want %d", envelope.AgentID, agentID)
	}
	if envelope.CommandType != "task_cancel" {
		t.Fatalf("relay command_type=%s, want task_cancel", envelope.CommandType)
	}
	if got := uint64(getFloat64(envelope.Payload, "task_id")); got != 88 {
		t.Fatalf("relay payload task_id=%d, want 88", got)
	}
	if envelope.CommandID != "" {
		t.Fatalf("expected empty command_id in generic relay test, got %q", envelope.CommandID)
	}
}

func TestHandleControlRelayEnvelope_TaskCancelRejectsStaleSession(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	defer func() { models.DB = previousDB }()

	handler := NewWebSocketHandler()
	handler.serverID = "server-owner"
	agentID := uint64(77)
	task := models.AgentTask{
		BaseModel:      models.BaseModel{ID: 88},
		AgentID:        agentID,
		Status:         models.TaskStatusCancelRequested,
		AgentSessionID: "session-live",
		OwnerServerID:  "server-owner",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	captured := 0
	previousWrite := writeAgentTextMessage
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		captured++
		return nil
	}
	defer func() { writeAgentTextMessage = previousWrite }()

	handler.agentsMu.Lock()
	handler.agents[agentID] = &wsClient{agentID: agentID, sessionID: "session-live", serverID: "server-owner", conn: &websocket.Conn{}}
	handler.agentsMu.Unlock()

	handler.handleControlRelayEnvelope(controlRelayEnvelope{
		CommandType:    "task_cancel",
		CommandID:      "cmd-1",
		TaskID:         task.ID,
		AgentID:        agentID,
		TargetServerID: "server-owner",
		AgentSessionID: "session-stale",
		Payload: map[string]interface{}{
			"task_id":    float64(task.ID),
			"command_id": "cmd-1",
		},
	})

	if captured != 0 {
		t.Fatalf("expected stale session relay to be rejected")
	}
}

func TestHandleControlRelayEnvelope_TaskCancelRejectsNonCancelableState(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	defer func() { models.DB = previousDB }()

	handler := NewWebSocketHandler()
	handler.serverID = "server-owner"
	agentID := uint64(78)
	task := models.AgentTask{
		BaseModel:      models.BaseModel{ID: 89},
		AgentID:        agentID,
		Status:         models.TaskStatusCancelled,
		AgentSessionID: "session-live",
		OwnerServerID:  "server-owner",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	captured := 0
	previousWrite := writeAgentTextMessage
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		captured++
		return nil
	}
	defer func() { writeAgentTextMessage = previousWrite }()

	handler.agentsMu.Lock()
	handler.agents[agentID] = &wsClient{agentID: agentID, sessionID: "session-live", serverID: "server-owner", conn: &websocket.Conn{}}
	handler.agentsMu.Unlock()

	handler.handleControlRelayEnvelope(controlRelayEnvelope{
		CommandType:    "task_cancel",
		CommandID:      "cmd-2",
		TaskID:         task.ID,
		AgentID:        agentID,
		TargetServerID: "server-owner",
		AgentSessionID: "session-live",
		Payload: map[string]interface{}{
			"task_id":    float64(task.ID),
			"command_id": "cmd-2",
		},
	})

	if captured != 0 {
		t.Fatalf("expected non-cancelable relay to be rejected")
	}
}

func TestConsumeControlRelay_DeliversTaskCancelToLocalAgent(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	defer func() { models.DB = previousDB }()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	}()

	handler := NewWebSocketHandler()
	handler.serverID = "server-owner"
	agentID := uint64(1201)
	task := models.AgentTask{
		AgentID:        agentID,
		Status:         models.TaskStatusCancelRequested,
		AgentSessionID: "session-live",
		OwnerServerID:  "server-owner",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	captured := make(chan WebSocketMessage, 1)
	previousWrite := writeAgentTextMessage
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		var msg WebSocketMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		captured <- msg
		return nil
	}
	defer func() { writeAgentTextMessage = previousWrite }()

	handler.agentsMu.Lock()
	handler.agents[agentID] = &wsClient{agentID: agentID, sessionID: "session-live", serverID: "server-owner", conn: &websocket.Conn{}}
	handler.agentsMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handler.consumeControlRelay(ctx)
	time.Sleep(100 * time.Millisecond)

	payload := map[string]interface{}{
		"task_id":    float64(task.ID),
		"command_id": "cmd-consumer-ok",
	}
	envelope := controlRelayEnvelope{
		CommandType:    "task_cancel",
		CommandID:      "cmd-consumer-ok",
		TaskID:         task.ID,
		AgentID:        agentID,
		TargetServerID: "server-owner",
		AgentSessionID: "session-live",
		Payload:        payload,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal relay envelope failed: %v", err)
	}
	if err := utils.RedisClient.Publish(context.Background(), utils.ControlRelayTopic("server-owner"), raw).Err(); err != nil {
		t.Fatalf("publish relay message failed: %v", err)
	}

	select {
	case msg := <-captured:
		if msg.Type != "task_cancel" {
			t.Fatalf("message type=%s, want task_cancel", msg.Type)
		}
		if got := uint64(getFloat64(msg.Payload, "task_id")); got != task.ID {
			t.Fatalf("payload task_id=%d, want %d", got, task.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relayed task_cancel delivery")
	}
}

func TestConsumeControlRelay_RejectsStaleSession(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	defer func() { models.DB = previousDB }()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	defer mini.Close()

	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
	}()

	handler := NewWebSocketHandler()
	handler.serverID = "server-owner"
	agentID := uint64(1202)
	task := models.AgentTask{
		AgentID:        agentID,
		Status:         models.TaskStatusCancelRequested,
		AgentSessionID: "session-live",
		OwnerServerID:  "server-owner",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	captured := make(chan WebSocketMessage, 1)
	previousWrite := writeAgentTextMessage
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		var msg WebSocketMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		captured <- msg
		return nil
	}
	defer func() { writeAgentTextMessage = previousWrite }()

	handler.agentsMu.Lock()
	handler.agents[agentID] = &wsClient{agentID: agentID, sessionID: "session-live", serverID: "server-owner", conn: &websocket.Conn{}}
	handler.agentsMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handler.consumeControlRelay(ctx)
	time.Sleep(100 * time.Millisecond)

	envelope := controlRelayEnvelope{
		CommandType:    "task_cancel",
		CommandID:      "cmd-consumer-stale",
		TaskID:         task.ID,
		AgentID:        agentID,
		TargetServerID: "server-owner",
		AgentSessionID: "session-stale",
		Payload: map[string]interface{}{
			"task_id":    float64(task.ID),
			"command_id": "cmd-consumer-stale",
		},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal relay envelope failed: %v", err)
	}
	if err := utils.RedisClient.Publish(context.Background(), utils.ControlRelayTopic("server-owner"), raw).Err(); err != nil {
		t.Fatalf("publish relay message failed: %v", err)
	}

	select {
	case msg := <-captured:
		t.Fatalf("unexpected relayed message type=%s for stale session", msg.Type)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestAgentControlWriteTimesOut(t *testing.T) {
	handler := NewWebSocketHandler()
	conn, cleanup := newBlockingAgentTestWebSocketConn(t)
	defer cleanup()
	client := &wsClient{conn: conn}

	previousTimeout := agentControlWriteTimeout
	agentControlWriteTimeout = 50 * time.Millisecond
	defer func() {
		agentControlWriteTimeout = previousTimeout
	}()

	largePayload := strings.Repeat("x", 1<<20)
	start := time.Now()
	err := handler.writeAgentWebSocketMessage(client, WebSocketMessage{Type: "task_cancel", Payload: map[string]interface{}{"task_id": float64(1), "blob": largePayload}})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected control write to fail on timeout")
	}
	if !isTimeoutError(err) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed < 40*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("expected bounded control write near timeout, elapsed=%s", elapsed)
	}
}

func TestAgentControlWriteSucceeds(t *testing.T) {
	handler := NewWebSocketHandler()
	client := &wsClient{conn: &websocket.Conn{}}

	previousWrite := writeAgentTextMessage
	captured := WebSocketMessage{}
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		if timeout != agentControlWriteTimeout {
			t.Fatalf("write timeout=%s, want %s", timeout, agentControlWriteTimeout)
		}
		if err := json.Unmarshal(data, &captured); err != nil {
			t.Fatalf("unmarshal outbound control message failed: %v", err)
		}
		return nil
	}
	defer func() {
		writeAgentTextMessage = previousWrite
	}()

	ok := handler.sendMessageToLocalAgent(99, "task_cancel", map[string]interface{}{"task_id": float64(7)})
	if ok {
		t.Fatalf("expected local delivery to fail without registered agent")
	}

	handler.agentsMu.Lock()
	handler.agents[99] = client
	handler.agentsMu.Unlock()

	if ok := handler.sendMessageToLocalAgent(99, "task_cancel", map[string]interface{}{"task_id": float64(7)}); !ok {
		t.Fatalf("expected bounded control write to succeed")
	}
	if captured.Type != "task_cancel" {
		t.Fatalf("message type=%s, want task_cancel", captured.Type)
	}
	if got := uint64(getFloat64(captured.Payload, "task_id")); got != 7 {
		t.Fatalf("payload task_id=%d, want 7", got)
	}
}

func newBlockingAgentTestWebSocketConn(t *testing.T) (*websocket.Conn, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(serverConn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		accept := computeTestWebSocketAccept(req.Header.Get("Sec-WebSocket-Key"))
		_, _ = io.WriteString(serverConn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+"\r\n\r\n")
		<-stop
	}()
	wsConn, _, err := websocket.NewClient(clientConn, &url.URL{Scheme: "ws", Host: "example.test", Path: "/ws"}, nil, 1024, 1024)
	if err != nil {
		close(stop)
		_ = serverConn.Close()
		t.Fatalf("create websocket client failed: %v", err)
	}
	cleanup := func() {
		close(stop)
		_ = wsConn.Close()
		_ = serverConn.Close()
		<-done
	}
	return wsConn, cleanup
}

func computeTestWebSocketAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func isTimeoutError(err error) bool {
	timeoutErr, ok := err.(interface{ Timeout() bool })
	return ok && timeoutErr.Timeout()
}

func TestHandleTaskCancelAck_CompletesTracker(t *testing.T) {
	handler := NewWebSocketHandler()
	commandID := "cancel-command-1"
	tracker := handler.registerTaskCancelTracker(commandID, models.AgentTask{BaseModel: models.BaseModel{ID: 88}})
	if tracker == nil {
		t.Fatalf("expected tracker to be created")
	}

	client := &wsClient{agentID: 1, sessionID: "session-a"}
	handler.handleTaskCancelAck(client, map[string]interface{}{
		"task_id":          float64(88),
		"command_id":       commandID,
		"ok":               true,
		"agent_session_id": "session-a",
	})

	select {
	case ok := <-tracker.done:
		if !ok {
			t.Fatalf("expected ack result to be true")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for tracker completion")
	}

	if stillTracked := handler.getTaskCancelTracker(commandID); stillTracked != nil {
		t.Fatalf("expected tracker to be removed after ack")
	}
}

func TestHandleTaskCancelAck_IgnoresMismatchedTaskID(t *testing.T) {
	handler := NewWebSocketHandler()
	commandID := "cancel-command-2"
	tracker := handler.registerTaskCancelTracker(commandID, models.AgentTask{BaseModel: models.BaseModel{ID: 88}})
	if tracker == nil {
		t.Fatalf("expected tracker to be created")
	}

	client := &wsClient{agentID: 1, sessionID: "session-b"}
	handler.handleTaskCancelAck(client, map[string]interface{}{
		"task_id":          float64(99),
		"command_id":       commandID,
		"ok":               true,
		"agent_session_id": "session-b",
	})

	select {
	case <-tracker.done:
		t.Fatalf("unexpected tracker completion for mismatched task id")
	case <-time.After(200 * time.Millisecond):
	}

	if stillTracked := handler.getTaskCancelTracker(commandID); stillTracked == nil {
		t.Fatalf("expected tracker to remain registered")
	}
}

func TestRedrivePendingTasksForConnectedAgent_ResendsCancelRequestedTask(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = previousDB
	})

	agent := models.Agent{Name: "cancel-redrive-agent", Host: "host", Port: 1, Token: "tok", Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, Status: models.PipelineRunStatusCancelRequested, AgentID: agent.ID}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 1, AgentID: agent.ID, PipelineRunID: run.ID, NodeID: "node-cancel-redrive", TaskType: "shell", Name: "cancel-redrive", Status: models.TaskStatusCancelRequested, AgentSessionID: "session-1", OwnerServerID: "server-1", StartTime: time.Now().Add(-3 * time.Second).Unix()}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.serverID = "server-1"
	client := &wsClient{agentID: agent.ID, sessionID: "session-1", serverID: "server-1"}

	previousWrite := writeAgentTextMessage
	writes := 0
	writeAgentTextMessage = func(conn *websocket.Conn, data []byte, timeout time.Duration) error {
		writes++
		return nil
	}
	defer func() {
		writeAgentTextMessage = previousWrite
	}()

	handler.agentsMu.Lock()
	handler.agents[agent.ID] = &wsClient{agentID: agent.ID, sessionID: "session-1", serverID: "server-1", conn: &websocket.Conn{}}
	handler.agentsMu.Unlock()

	handler.redrivePendingTasksForConnectedAgent(client)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if writes > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected cancel_requested task to be re-sent")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMarkTaskCancelDeliveryTimeout_PersistsObservableMessage(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	defer func() { models.DB = previousDB }()

	run := models.PipelineRun{WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, Status: models.PipelineRunStatusCancelRequested}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 1, AgentID: 1, PipelineRunID: run.ID, NodeID: "node-timeout", TaskType: "shell", Name: "timeout-task", Status: models.TaskStatusCancelRequested}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	handler := NewWebSocketHandler()
	handler.markTaskCancelDeliveryTimeout(task, 2*time.Second, 3)

	var reloadedTask models.AgentTask
	if err := db.First(&reloadedTask, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if !strings.Contains(reloadedTask.ErrorMsg, "等待 agent 确认") {
		t.Fatalf("task error_msg=%q, want waiting-for-agent hint", reloadedTask.ErrorMsg)
	}
	if reloadedTask.Status != models.TaskStatusCancelRequested {
		t.Fatalf("task status=%s, want %s", reloadedTask.Status, models.TaskStatusCancelRequested)
	}

	var reloadedRun models.PipelineRun
	if err := db.First(&reloadedRun, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if reloadedRun.Status != models.PipelineRunStatusCancelRequested {
		t.Fatalf("run status=%s, want %s", reloadedRun.Status, models.PipelineRunStatusCancelRequested)
	}
}

func TestReconcileCancelRequestedTasks_FinalizesOfflineTaskAndRun(t *testing.T) {
	db := openHandlerTestDB(t)
	previousDB := models.DB
	previousRedis := utils.RedisClient
	models.DB = db
	utils.RedisClient = nil
	t.Cleanup(func() {
		models.DB = previousDB
		utils.RedisClient = previousRedis
	})

	agent := models.Agent{Name: "offline-cancel-agent", Host: "host", Port: 1, Token: "tok", Status: models.AgentStatusOffline, RegistrationStatus: models.AgentRegistrationStatusApproved, HeartbeatInterval: 10, LastHeartAt: time.Now().Add(-time.Minute).Unix()}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: 1, PipelineID: 1, BuildNumber: 1, Status: models.PipelineRunStatusCancelRequested, AgentID: agent.ID, StartTime: time.Now().Add(-10 * time.Second).Unix()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 1, AgentID: agent.ID, PipelineRunID: run.ID, NodeID: "node-offline-cancel", TaskType: "shell", Name: "offline-cancel", Status: models.TaskStatusCancelRequested, AgentSessionID: "gone-session", OwnerServerID: "gone-server", StartTime: time.Now().Add(-5 * time.Second).Unix()}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	updated, err := reconcileCancelRequestedTasks(db, time.Now().Unix())
	if err != nil {
		t.Fatalf("reconcileCancelRequestedTasks returned error: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d, want 1", updated)
	}

	var reloadedTask models.AgentTask
	if err := db.First(&reloadedTask, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if reloadedTask.Status != models.TaskStatusCancelled {
		t.Fatalf("task status=%s, want %s", reloadedTask.Status, models.TaskStatusCancelled)
	}
	if reloadedTask.EndTime == 0 {
		t.Fatal("expected cancelled task end_time to be set")
	}

	var reloadedRun models.PipelineRun
	if err := db.First(&reloadedRun, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if reloadedRun.Status != models.PipelineRunStatusCancelled {
		t.Fatalf("run status=%s, want %s", reloadedRun.Status, models.PipelineRunStatusCancelled)
	}
	if reloadedRun.EndTime == 0 {
		t.Fatal("expected cancelled run end_time to be set")
	}
}
