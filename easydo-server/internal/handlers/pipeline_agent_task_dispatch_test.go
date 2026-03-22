package handlers

import (
	"encoding/json"
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
