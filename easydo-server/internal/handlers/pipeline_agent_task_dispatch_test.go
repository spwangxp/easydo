package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestExecuteNodeWithAgent_PreservesCanonicalTaskTypeForAgentTasks(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })

	agent := models.Agent{
		Name:               "agent-online",
		Host:               "127.0.0.1",
		Port:               0,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	run := &models.PipelineRun{WorkspaceID: 11}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	node := &PipelineNode{
		ID:   "docker-build",
		Type: "docker",
		Name: "Build Image",
		Config: map[string]interface{}{
			"image_name": "demo/app",
			"image_tag":  "latest",
		},
	}

	success, _ := handler.executeNodeWithAgent(db, models.Pipeline{}, run, node, nil, nil, agent.ID, 0, "")
	if !success {
		t.Fatalf("expected executeNodeWithAgent success")
	}

	var task models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task).Error; err != nil {
		t.Fatalf("load task failed: %v", err)
	}
	if task.TaskType != "docker" {
		t.Fatalf("task type=%s, want docker", task.TaskType)
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
		t.Fatalf("unmarshal params failed: %v", err)
	}
	if params["image_name"] != "demo/app" {
		t.Fatalf("image_name=%v, want demo/app", params["image_name"])
	}
}

func TestExecuteNodeWithAgent_DoesNotStoreAISessionMetadataInResultData(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIAgent{}, &models.AIRuntimeProfile{}, &models.AIModelBinding{}, &models.AISession{}); err != nil {
		t.Fatalf("migrate ai tables failed: %v", err)
	}

	agent := models.Agent{
		Name:               "agent-online",
		Host:               "127.0.0.1",
		Port:               0,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	provider := models.AIProvider{WorkspaceID: 11, Name: "provider", ProviderType: "openai", Status: models.AIProviderStatusActive, CreatedBy: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create ai provider failed: %v", err)
	}
	model := models.AIModelCatalog{Name: "demo-model", Source: "seed", SourceModelID: "demo-model", ImportedBy: 1}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create ai model failed: %v", err)
	}
	aia := models.AIAgent{WorkspaceID: 11, Name: "mr-agent", Scenario: "mr_quality_check", Status: models.AIAgentStatusActive, CreatedBy: 1}
	if err := db.Create(&aia).Error; err != nil {
		t.Fatalf("create ai agent failed: %v", err)
	}
	profile := models.AIRuntimeProfile{WorkspaceID: 11, AgentID: aia.ID, Name: "default", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: 1}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create runtime profile failed: %v", err)
	}
	binding := models.AIModelBinding{WorkspaceID: 11, ModelID: model.ID, ProviderID: provider.ID, ProviderModelKey: "demo-model", Status: models.AIModelBindingStatusActive, CreatedBy: 1}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatalf("create ai binding failed: %v", err)
	}
	profile.BindingPriorityJSON = `[{"binding_id":` + fmt.Sprint(binding.ID) + `,"priority":0,"enabled":true}]`
	if err := db.Save(&profile).Error; err != nil {
		t.Fatalf("update runtime profile bindings failed: %v", err)
	}

	run := &models.PipelineRun{WorkspaceID: 11, TriggerUserID: 1}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	node := &PipelineNode{
		ID:   "mr-review",
		Type: "mr_quality_check",
		Name: "MR Review",
		Config: map[string]interface{}{
			"runtime_profile_id": profile.ID,
			"input_text":         "review this MR",
		},
	}

	success, _ := handler.executeNodeWithAgent(db, models.Pipeline{}, run, node, nil, nil, agent.ID, 1, "owner")
	if !success {
		t.Fatalf("expected executeNodeWithAgent success")
	}

	var task models.AgentTask
	if err := db.Where("pipeline_run_id = ? AND node_id = ?", run.ID, node.ID).First(&task).Error; err != nil {
		t.Fatalf("load task failed: %v", err)
	}
	if strings.TrimSpace(task.ResultData) != "" {
		t.Fatalf("result_data=%q, want empty", task.ResultData)
	}
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
		t.Fatalf("unmarshal params failed: %v", err)
	}
	if toUint64Value(params["ai_session_id"]) == 0 {
		t.Fatalf("expected ai_session_id in params, got %#v", params)
	}
}
