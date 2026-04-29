package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"github.com/gin-gonic/gin"
)

func resetHeartbeatHistoryForTest() {
	heartbeatMu.Lock()
	heartbeatHistory = make(map[uint64][]models.AgentHeartbeat)
	heartbeatMu.Unlock()
}

func TestGetAgentHeartbeats_ReadsFromDatabaseFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetHeartbeatHistoryForTest()

	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:               "heartbeat-agent-db",
		Host:               "host",
		Port:               9001,
		Token:              "token-db",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	record := models.AgentHeartbeat{
		AgentID:      agent.ID,
		Timestamp:    time.Now().Unix(),
		CPUUsage:     33.3,
		MemoryUsage:  44.4,
		DiskUsage:    55.5,
		LoadAvg:      "0.2,0.3,0.4",
		TasksRunning: 2,
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("create heartbeat record failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agents/"+strconv.FormatUint(agent.ID, 10)+"/heartbeats?page=1&page_size=10", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}

	h.GetAgentHeartbeats(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []models.AgentHeartbeat `json:"list"`
			Total int64                   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Code != 200 {
		t.Fatalf("code=%d body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("unexpected total/list: total=%d len=%d", resp.Data.Total, len(resp.Data.List))
	}
	if resp.Data.List[0].AgentID != agent.ID {
		t.Fatalf("agent_id=%d, want=%d", resp.Data.List[0].AgentID, agent.ID)
	}
}

func TestGetAgentHeartbeats_FallbacksToMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetHeartbeatHistoryForTest()

	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:               "heartbeat-agent-memory",
		Host:               "host",
		Port:               9002,
		Token:              "token-memory",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	SharedWebSocketHandler().storeHeartbeat(agent.ID, models.AgentHeartbeat{
		AgentID:      agent.ID,
		Timestamp:    time.Now().Unix(),
		CPUUsage:     66.6,
		MemoryUsage:  77.7,
		DiskUsage:    88.8,
		TasksRunning: 1,
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agents/"+strconv.FormatUint(agent.ID, 10)+"/heartbeats?page=1&page_size=10", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(agent.ID, 10)}}

	h.GetAgentHeartbeats(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []models.AgentHeartbeat `json:"list"`
			Total int64                   `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Code != 200 {
		t.Fatalf("code=%d body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("unexpected total/list: total=%d len=%d", resp.Data.Total, len(resp.Data.List))
	}
	if resp.Data.List[0].AgentID != agent.ID {
		t.Fatalf("agent_id=%d, want=%d", resp.Data.List[0].AgentID, agent.ID)
	}
}

func TestHeartbeat_PersistsHeartbeatRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetHeartbeatHistoryForTest()
	resetHeartbeatSamplingForTest()

	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:               "heartbeat-agent-persist",
		Host:               "host",
		Port:               9003,
		Token:              "token-persist",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	reqBody := map[string]any{
		"agent_id":      agent.ID,
		"token":         agent.Token,
		"timestamp":     time.Now().Unix(),
		"cpu_usage":     21.5,
		"memory_usage":  35.7,
		"disk_usage":    49.2,
		"load_avg":      "0.1,0.2,0.3",
		"tasks_running": 3,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/agents/heartbeat", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Heartbeat(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var count int64
	if err := db.Model(&models.AgentHeartbeat{}).Where("agent_id = ?", agent.ID).Count(&count).Error; err != nil {
		t.Fatalf("count heartbeat records failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("heartbeat record count=%d, want=1", count)
	}
}

func TestHeartbeat_SamplesDatabaseWritesButKeepsFullMemoryHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetHeartbeatHistoryForTest()
	resetHeartbeatSamplingForTest()

	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:               "heartbeat-agent-sampled",
		Host:               "host",
		Port:               9004,
		Token:              "token-sampled",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	sendHeartbeat := func(ts int64) {
		reqBody := map[string]any{
			"agent_id":      agent.ID,
			"token":         agent.Token,
			"timestamp":     ts,
			"cpu_usage":     21.5,
			"memory_usage":  35.7,
			"disk_usage":    49.2,
			"load_avg":      "0.1,0.2,0.3",
			"tasks_running": 3,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request failed: %v", err)
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/agents/heartbeat", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.Heartbeat(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}

	baseTs := time.Now().Unix()
	sendHeartbeat(baseTs)
	sendHeartbeat(baseTs + 10)
	sendHeartbeat(baseTs + 20)

	var count int64
	if err := db.Model(&models.AgentHeartbeat{}).Where("agent_id = ?", agent.ID).Count(&count).Error; err != nil {
		t.Fatalf("count heartbeat records failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("heartbeat record count=%d, want=1", count)
	}

	memoryHeartbeats, total := SharedWebSocketHandler().GetHeartbeats(agent.ID, 1, 10)
	if total != 3 || len(memoryHeartbeats) != 3 {
		t.Fatalf("memory heartbeat total=%d len=%d, want=3", total, len(memoryHeartbeats))
	}

	sendHeartbeat(baseTs + 61)
	if err := db.Model(&models.AgentHeartbeat{}).Where("agent_id = ?", agent.ID).Count(&count).Error; err != nil {
		t.Fatalf("count heartbeat records after sample window failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("heartbeat record count after sample window=%d, want=2", count)
	}
}

func TestHeartbeat_DebouncesQueuedRunScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetHeartbeatHistoryForTest()
	resetHeartbeatSamplingForTest()
	resetHeartbeatSchedulerDebounceForTest()

	originalRedis := utils.RedisClient
	utils.RedisClient = nil
	defer func() { utils.RedisClient = originalRedis }()

	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:               "heartbeat-agent-debounce",
		Host:               "host",
		Port:               9005,
		Token:              "token-debounce",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	sendHeartbeat := func(ts int64) {
		reqBody := map[string]any{
			"agent_id":      agent.ID,
			"token":         agent.Token,
			"timestamp":     ts,
			"cpu_usage":     21.5,
			"memory_usage":  35.7,
			"disk_usage":    49.2,
			"load_avg":      "0.1,0.2,0.3",
			"tasks_running": 0,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("marshal request failed: %v", err)
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/agents/heartbeat", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.Heartbeat(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}

	baseTs := time.Now().Unix()
	sendHeartbeat(baseTs)
	sendHeartbeat(baseTs + 10)
	sendHeartbeat(baseTs + 20)

	if got := heartbeatSchedulerDebounceCount(); got != 1 {
		t.Fatalf("heartbeat scheduler debounce count=%d, want=1", got)
	}

	sendHeartbeat(baseTs + 61)
	if got := heartbeatSchedulerDebounceCount(); got != 2 {
		t.Fatalf("heartbeat scheduler debounce count after window=%d, want=2", got)
	}
}
