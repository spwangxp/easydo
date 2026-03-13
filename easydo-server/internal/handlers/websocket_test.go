package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
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

func TestHeartbeatAckPayload(t *testing.T) {
	payload := map[string]interface{}{
		"status":             "ok",
		"server_time":        float64(time.Now().Unix()),
		"pending_tasks":      float64(5),
		"heartbeat_interval": float64(10),
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
