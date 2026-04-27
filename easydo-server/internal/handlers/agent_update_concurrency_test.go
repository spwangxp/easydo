package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func performUpdateAgentRequest(t *testing.T, h *AgentHandler, agentID uint64, payload map[string]interface{}) *httptest.ResponseRecorder {
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
	c.Set("role", "admin")

	h.UpdateAgent(c)
	return w
}

func TestUpdateAgent_MaxConcurrencyRealtimeAdjustsStatus(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:                   "update-concurrency-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 3,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	if err := db.Create(&models.PipelineRun{
		PipelineID:  1,
		BuildNumber: 1,
		AgentID:     agent.ID,
		Status:      models.PipelineRunStatusRunning,
	}).Error; err != nil {
		t.Fatalf("create running run failed: %v", err)
	}

	w := performUpdateAgentRequest(t, h, agent.ID, map[string]interface{}{
		"max_concurrent_pipelines": 1,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.MaxConcurrentPipelines != 1 {
		t.Fatalf("max_concurrent_pipelines=%d, want=1", got.MaxConcurrentPipelines)
	}
	if got.Status != models.AgentStatusBusy {
		t.Fatalf("status=%s, want=%s after reducing max", got.Status, models.AgentStatusBusy)
	}

	w = performUpdateAgentRequest(t, h, agent.ID, map[string]interface{}{
		"max_concurrent_pipelines": 2,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.MaxConcurrentPipelines != 2 {
		t.Fatalf("max_concurrent_pipelines=%d, want=2", got.MaxConcurrentPipelines)
	}
	if got.Status != models.AgentStatusOnline {
		t.Fatalf("status=%s, want=%s after increasing max", got.Status, models.AgentStatusOnline)
	}
}

func TestUpdateAgent_NonPositiveMaxConcurrencyDoesNotOverwrite(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:                   "update-concurrency-agent-non-positive",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 4,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	cases := []int{0, -3}
	for _, max := range cases {
		w := performUpdateAgentRequest(t, h, agent.ID, map[string]interface{}{
			"max_concurrent_pipelines": max,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}

	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.MaxConcurrentPipelines != 4 {
		t.Fatalf("max_concurrent_pipelines=%d, want=4", got.MaxConcurrentPipelines)
	}
}

func TestUpdateAgent_PersistsTaskConcurrencyAndMirrorModes(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &AgentHandler{DB: db}

	agent := models.Agent{
		Name:                   "update-settings-agent",
		Host:                   "host",
		Port:                   1,
		Token:                  "token",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 4,
		TaskConcurrency:        5,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	w := performUpdateAgentRequest(t, h, agent.ID, map[string]interface{}{
		"task_concurrency":                7,
		"dockerhub_mirrors_configured":    true,
		"dockerhub_mirrors":               []string{"https://mirror-a.example", "https://mirror-b.example"},
		"max_concurrent_pipelines":        9,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var got models.Agent
	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if got.MaxConcurrentPipelines != 9 {
		t.Fatalf("max_concurrent_pipelines=%d, want=9", got.MaxConcurrentPipelines)
	}
	if got.TaskConcurrency != 7 {
		t.Fatalf("task_concurrency=%d, want=7", got.TaskConcurrency)
	}
	if !got.DockerHubMirrorsConfigured {
		t.Fatal("dockerhub_mirrors_configured=false, want true")
	}
	if got.DockerHubMirrors != "https://mirror-a.example,https://mirror-b.example" {
		t.Fatalf("dockerhub_mirrors=%q, want joined mirrors", got.DockerHubMirrors)
	}

	w = performUpdateAgentRequest(t, h, agent.ID, map[string]interface{}{
		"dockerhub_mirrors_configured": true,
		"dockerhub_mirrors":            []string{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	if err := db.First(&got, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if !got.DockerHubMirrorsConfigured {
		t.Fatal("dockerhub_mirrors_configured=false, want explicit empty list to stay configured")
	}
	if got.DockerHubMirrors != "" {
		t.Fatalf("dockerhub_mirrors=%q, want empty string for explicit empty list", got.DockerHubMirrors)
	}
}
