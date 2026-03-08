package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestGetTaskList_IncludeScheduleFieldsAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &TaskHandler{DB: db}

	agent1 := models.Agent{
		Name:                   "agent-alpha",
		Host:                   "a1",
		Port:                   9001,
		Token:                  "t1",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 2,
	}
	agent2 := models.Agent{
		Name:                   "agent-beta",
		Host:                   "a2",
		Port:                   9002,
		Token:                  "t2",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 2,
	}
	if err := db.Create(&agent1).Error; err != nil {
		t.Fatalf("create agent1 failed: %v", err)
	}
	if err := db.Create(&agent2).Error; err != nil {
		t.Fatalf("create agent2 failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "deploy-web",
		Description: "task schedule list test",
		OwnerID:     1,
		Environment: "testing",
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	runRunning := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 12,
		Status:      models.PipelineRunStatusRunning,
		TriggerUser: "demo",
		AgentID:     agent1.ID,
	}
	runQueued := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 13,
		Status:      models.PipelineRunStatusQueued,
		TriggerUser: "demo",
		AgentID:     agent2.ID,
	}
	if err := db.Create(&runRunning).Error; err != nil {
		t.Fatalf("create running run failed: %v", err)
	}
	if err := db.Create(&runQueued).Error; err != nil {
		t.Fatalf("create queued run failed: %v", err)
	}

	targetTask := models.AgentTask{
		AgentID:       agent1.ID,
		PipelineRunID: runRunning.ID,
		NodeID:        "task-3",
		TaskType:      "shell",
		Name:          "compile-task",
		Status:        models.TaskStatusPending,
		Timeout:       60,
	}
	otherTask := models.AgentTask{
		AgentID:       agent2.ID,
		PipelineRunID: runQueued.ID,
		NodeID:        "task-4",
		TaskType:      "shell",
		Name:          "other-task",
		Status:        models.TaskStatusPending,
		Timeout:       60,
	}
	if err := db.Create(&targetTask).Error; err != nil {
		t.Fatalf("create target task failed: %v", err)
	}
	if err := db.Create(&otherTask).Error; err != nil {
		t.Fatalf("create other task failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	c.Request.URL.RawQuery = "include_schedule=1&agent_id=" + strconv.FormatUint(targetTask.AgentID, 10) + "&status=pending&run_status=running&page=1&page_size=20"

	h.GetTaskList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID           uint64 `json:"id"`
				Name         string `json:"name"`
				AgentID      uint64 `json:"agent_id"`
				Status       string `json:"status"`
				PipelineID   uint64 `json:"pipeline_id"`
				PipelineName string `json:"pipeline_name"`
				BuildNumber  int    `json:"build_number"`
				RunStatus    string `json:"run_status"`
				TriggerUser  string `json:"trigger_user"`
				AgentName    string `json:"agent_name"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("response code=%d body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.Total != 1 {
		t.Fatalf("total=%d, want=1", resp.Data.Total)
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("list size=%d, want=1", len(resp.Data.List))
	}

	item := resp.Data.List[0]
	if item.ID != targetTask.ID {
		t.Fatalf("task id=%d, want=%d", item.ID, targetTask.ID)
	}
	if item.PipelineName != pipeline.Name {
		t.Fatalf("pipeline_name=%s, want=%s", item.PipelineName, pipeline.Name)
	}
	if item.BuildNumber != runRunning.BuildNumber {
		t.Fatalf("build_number=%d, want=%d", item.BuildNumber, runRunning.BuildNumber)
	}
	if item.RunStatus != models.PipelineRunStatusRunning {
		t.Fatalf("run_status=%s, want=%s", item.RunStatus, models.PipelineRunStatusRunning)
	}
	if item.AgentName != agent1.Name {
		t.Fatalf("agent_name=%s, want=%s", item.AgentName, agent1.Name)
	}
}

func TestGetTaskList_IncludeScheduleKeywordSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &TaskHandler{DB: db}

	agent := models.Agent{
		Name:                   "agent-gamma",
		Host:                   "a3",
		Port:                   9003,
		Token:                  "t3",
		Status:                 models.AgentStatusOnline,
		RegistrationStatus:     models.AgentRegistrationStatusApproved,
		MaxConcurrentPipelines: 2,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	pipeline := models.Pipeline{
		Name:        "release-service",
		Description: "keyword search",
		OwnerID:     1,
		Environment: "testing",
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{
		PipelineID:  pipeline.ID,
		BuildNumber: 1,
		Status:      models.PipelineRunStatusRunning,
		AgentID:     agent.ID,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	task := models.AgentTask{
		AgentID:       agent.ID,
		PipelineRunID: run.ID,
		NodeID:        "deploy-node",
		TaskType:      "shell",
		Name:          "deploy-job",
		Status:        models.TaskStatusRunning,
		Timeout:       60,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/tasks?include_schedule=1&keyword=release&page=1&page_size=20", nil)

	h.GetTaskList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID uint64 `json:"id"`
			} `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("response code=%d body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 || resp.Data.List[0].ID != task.ID {
		t.Fatalf("unexpected keyword search result: total=%d list=%v", resp.Data.Total, resp.Data.List)
	}
}
