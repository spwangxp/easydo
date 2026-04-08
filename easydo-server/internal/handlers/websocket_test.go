package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
			"stdout": "EASYDO_BASE_INFO_BEGIN\nEASYDO_HOSTNAME=vm-prod-01\nEASYDO_CPU_LOGICAL_CORES=8\nEASYDO_MEMORY_TOTAL_BYTES=34359738368\nEASYDO_TOTAL_DISK_BYTES=536870912000\nEASYDO_GPU_COUNT=1\nEASYDO_DISK_ROWS_BEGIN\nNAME=\"sda\" SIZE=\"536870912000\" TYPE=\"disk\" FSTYPE=\"ext4\" MOUNTPOINT=\"/\"\nEASYDO_DISK_ROWS_END\nEASYDO_GPU_CSV_BEGIN\n0, NVIDIA L40, 46068\nEASYDO_GPU_CSV_END\nEASYDO_BASE_INFO_END\n",
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
	if !strings.Contains(stored.BaseInfo, `"logicalCores":8`) {
		t.Fatalf("expected logicalCores in stored base_info, got=%s", stored.BaseInfo)
	}
	if !strings.Contains(stored.BaseInfo, `"count":1`) {
		t.Fatalf("expected gpu count in stored base_info, got=%s", stored.BaseInfo)
	}
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

// Regression test: frontend WebSocket connections must stay open beyond 60 seconds.
// Before the fix, handleFrontendMessages set a 60-second read deadline on each
// ReadMessage call, causing the connection to be closed after 60 seconds of inactivity
// even though the frontend is a receive-only connection that doesn't send messages.
func TestFrontendWebSocket_ConnectionStaysOpenBeyond60Seconds(t *testing.T) {
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

	router := gin.New()
	router.GET("/ws/frontend/pipeline", wsHandler.HandleFrontendConnection)
	auth := router.Group("/api/auth")
	auth.POST("/login", userHandler.Login)

	server := httptest.NewServer(router)
	defer server.Close()

	user := models.User{
		Username: "ws-longevity-user",
		Status:   "active",
		Email:    "ws-longevity@example.com",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace := models.Workspace{
		Name:      "ws-longevity-workspace",
		Slug:      "ws-longevity-" + strconv.FormatUint(user.ID, 10),
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

	loginBody := map[string]string{"username": "ws-longevity-user", "password": "1qaz2WSX"}
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

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/frontend/pipeline?run_id=" + strconv.FormatUint(run.ID, 10) + "&token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	// Wait 65 seconds — past the old 60-second read deadline that caused disconnects.
	// The connection should remain open because handleFrontendMessages no longer sets
	// any read deadline on frontend WebSocket connections (they are receive-only).
	time.Sleep(65 * time.Second)

	// Verify connection is still open by setting a read deadline and attempting a read.
	// If the connection were closed by the server, this would fail immediately.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket connection was closed after 65 seconds (old 60s deadline bug not fixed): %v", err)
	}

	// Clean up the read deadline — connection is confirmed alive
	conn.SetReadDeadline(time.Time{})
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
