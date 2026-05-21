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
		Config: map[string]any{
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

	var params map[string]any
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
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIAgent{}, &models.AIRuntimeProfile{}, &models.AIModelBinding{}, &models.AIScene{}, &models.AISession{}); err != nil {
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
	profile := models.AIRuntimeProfile{WorkspaceID: 11, Name: "default", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: 1}
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
		Config: map[string]any{
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
	var params map[string]any
	if err := json.Unmarshal([]byte(task.Params), &params); err != nil {
		t.Fatalf("unmarshal params failed: %v", err)
	}
	if toUint64Value(params["ai_session_id"]) == 0 {
		t.Fatalf("expected ai_session_id in params, got %#v", params)
	}
	if params["task_type"] != "mr_quality_check" {
		t.Fatalf("task_type=%v, want mr_quality_check", params["task_type"])
	}
	if strings.TrimSpace(fmt.Sprint(params["scene_code"])) == "" {
		t.Fatalf("expected scene_code in params, got %#v", params)
	}
	if params["scene_type"] != pipelineTaskSceneType {
		t.Fatalf("scene_type=%v, want %s", params["scene_type"], pipelineTaskSceneType)
	}
	if _, exists := params["scenario"]; exists {
		t.Fatalf("expected agent params to stop exposing scenario, got %#v", params)
	}
}

func TestCreateAISessionForNode_UsesAgentRuntimeProfileWhenNodeConfigOmitsIt(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AIProvider{}, &models.AIAgent{}, &models.AIRuntimeProfile{}, &models.AIModelBinding{}, &models.AIScene{}, &models.AISession{}); err != nil {
		t.Fatalf("migrate ai tables failed: %v", err)
	}

	provider := models.AIProvider{WorkspaceID: 11, Name: "provider", ProviderType: "openai", Status: models.AIProviderStatusActive, CreatedBy: 1}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create ai provider failed: %v", err)
	}
	model := models.AIModelCatalog{Name: "agent-runtime-model", Source: "seed", SourceModelID: "agent-runtime-model", ImportedBy: 1}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("create ai model failed: %v", err)
	}
	profile := models.AIRuntimeProfile{WorkspaceID: 11, Name: "agent-runtime-profile", ModelID: model.ID, Status: models.AIRuntimeProfileStatusActive, CreatedBy: 1}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create runtime profile failed: %v", err)
	}
	binding := models.AIModelBinding{WorkspaceID: 11, ModelID: model.ID, ProviderID: provider.ID, ProviderModelKey: "agent-runtime-model", Status: models.AIModelBindingStatusActive, CreatedBy: 1}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatalf("create ai binding failed: %v", err)
	}
	profile.BindingPriorityJSON = `[{"binding_id":` + fmt.Sprint(binding.ID) + `,"priority":0,"enabled":true}]`
	if err := db.Save(&profile).Error; err != nil {
		t.Fatalf("update runtime profile bindings failed: %v", err)
	}
	runtimeProfileID := profile.ID
	aiAgent := models.AIAgent{WorkspaceID: 11, Name: "runtime-agent", RuntimeProfileID: &runtimeProfileID, Status: models.AIAgentStatusActive, CreatedBy: 1}
	if err := db.Create(&aiAgent).Error; err != nil {
		t.Fatalf("create ai agent failed: %v", err)
	}
	run := &models.PipelineRun{WorkspaceID: 11, TriggerUserID: 1}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	session, scene, err := handler.createAISessionForNode(db, run, &PipelineNode{ID: "mr-review", Type: "mr_quality_check", Name: "MR Review"}, "mr_quality_check", map[string]any{
		"agent_id":     aiAgent.ID,
		"scene_code":   "user-supplied-scene",
		"input_text":   "review this MR",
		"workspace_id": run.WorkspaceID,
	}, 1)
	if err != nil {
		t.Fatalf("createAISessionForNode returned error: %v", err)
	}
	if scene == nil {
		t.Fatal("expected scene to be created")
	}
	if session.RuntimeProfileID != profile.ID {
		t.Fatalf("runtime_profile_id=%d, want %d", session.RuntimeProfileID, profile.ID)
	}
	if session.BindingID != binding.ID {
		t.Fatalf("binding_id=%d, want %d", session.BindingID, binding.ID)
	}
	if session.ProviderID != provider.ID {
		t.Fatalf("provider_id=%d, want %d", session.ProviderID, provider.ID)
	}
	if session.ModelID != model.ID {
		t.Fatalf("model_id=%d, want %d", session.ModelID, model.ID)
	}
	if session.SceneID == nil || *session.SceneID != scene.ID {
		t.Fatalf("scene_id=%v, want %d", session.SceneID, scene.ID)
	}
	if scene.Code != "pipeline_task:mr_quality_check:agent:"+fmt.Sprint(aiAgent.ID) {
		t.Fatalf("scene_code=%s, want pipeline_task:mr_quality_check:agent:%d", scene.Code, aiAgent.ID)
	}
	var requestPayload map[string]any
	if err := json.Unmarshal([]byte(session.RequestJSON), &requestPayload); err != nil {
		t.Fatalf("unmarshal request payload failed: %v", err)
	}
	if requestPayload["task_type"] != "mr_quality_check" {
		t.Fatalf("task_type=%v, want mr_quality_check", requestPayload["task_type"])
	}
	if toUint64Value(requestPayload["scene_id"]) != scene.ID {
		t.Fatalf("scene_id=%v, want %d", requestPayload["scene_id"], scene.ID)
	}
	if requestPayload["scene_code"] != scene.Code {
		t.Fatalf("scene_code=%v, want %s", requestPayload["scene_code"], scene.Code)
	}
	if requestPayload["scene_type"] != pipelineTaskSceneType {
		t.Fatalf("scene_type=%v, want %s", requestPayload["scene_type"], pipelineTaskSceneType)
	}
	if _, exists := requestPayload["scenario"]; exists {
		t.Fatalf("expected ai session request payload to stop exposing scenario, got %#v", requestPayload)
	}
}

func TestBuildTaskAttemptSnapshotsIncludesAIRuntimeIdentifiers(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AgentTask{}, &models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate runtime snapshot tables failed: %v", err)
	}

	sceneID := uint64(51)
	startedAt := int64(171)
	completedAt := int64(181)
	session := models.AISession{WorkspaceID: 11, SceneID: &sceneID, RuntimeProfileID: 21, ProviderID: 31, ModelID: 41, TaskType: "mr_quality_check", Status: models.AISessionStatusQueued, CreatedBy: 1}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}
	turn := models.AISessionTurn{SessionID: session.ID, TurnSeq: 1, TurnType: "mr_quality_check", Role: "assistant", Status: string(models.AISessionStatusCompleted), InputJSON: `{"input_text":"review this MR"}`, OutputJSON: `{"summary":"ok"}`, StartedAt: &startedAt, CompletedAt: &completedAt}
	if err := db.Create(&turn).Error; err != nil {
		t.Fatalf("create ai session turn failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 11, AgentID: 7, PipelineRunID: 19, NodeID: "mr-review", Params: `{"ai_session_id":` + fmt.Sprint(session.ID) + `}`, Status: models.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create agent task failed: %v", err)
	}

	attempts := buildTaskAttemptSnapshots(db, task)
	if len(attempts) != 1 {
		t.Fatalf("attempt count=%d, want 1", len(attempts))
	}
	if toUint64Value(attempts[0]["ai_session_id"]) != session.ID {
		t.Fatalf("ai_session_id=%v, want %d", attempts[0]["ai_session_id"], session.ID)
	}
	if toUint64Value(attempts[0]["scene_id"]) != sceneID {
		t.Fatalf("scene_id=%v, want %d", attempts[0]["scene_id"], sceneID)
	}
	if toUint64Value(attempts[0]["runtime_profile_id"]) != session.RuntimeProfileID {
		t.Fatalf("runtime_profile_id=%v, want %d", attempts[0]["runtime_profile_id"], session.RuntimeProfileID)
	}
	if toUint64Value(attempts[0]["provider_id"]) != session.ProviderID {
		t.Fatalf("provider_id=%v, want %d", attempts[0]["provider_id"], session.ProviderID)
	}
	if toUint64Value(attempts[0]["model_id"]) != session.ModelID {
		t.Fatalf("model_id=%v, want %d", attempts[0]["model_id"], session.ModelID)
	}
	turnSnapshot, ok := attempts[0]["ai_session_turn"].(map[string]any)
	if !ok {
		t.Fatalf("expected ai_session_turn snapshot, got %#v", attempts[0]["ai_session_turn"])
	}
	if toUint64Value(turnSnapshot["id"]) != turn.ID {
		t.Fatalf("turn id=%v, want %d", turnSnapshot["id"], turn.ID)
	}
	if toInt(turnSnapshot["turn_seq"]) != 1 {
		t.Fatalf("turn_seq=%v, want 1", turnSnapshot["turn_seq"])
	}
	if turnSnapshot["turn_type"] != "mr_quality_check" {
		t.Fatalf("turn_type=%v, want mr_quality_check", turnSnapshot["turn_type"])
	}
	if turnSnapshot["status"] != string(models.AISessionStatusCompleted) {
		t.Fatalf("turn status=%v, want %s", turnSnapshot["status"], models.AISessionStatusCompleted)
	}
	inputJSON, ok := turnSnapshot["input_json"].(map[string]any)
	if !ok || inputJSON["input_text"] != "review this MR" {
		t.Fatalf("input_json=%#v, want parsed input_text", turnSnapshot["input_json"])
	}
	outputJSON, ok := turnSnapshot["output_json"].(map[string]any)
	if !ok || outputJSON["summary"] != "ok" {
		t.Fatalf("output_json=%#v, want parsed summary", turnSnapshot["output_json"])
	}
}

func TestBuildTaskAttemptSnapshotsOmitsTurnWhenAttemptDoesNotMatch(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AgentTask{}, &models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate runtime snapshot tables failed: %v", err)
	}

	session := models.AISession{WorkspaceID: 11, TaskType: "mr_quality_check", Status: models.AISessionStatusQueued, CreatedBy: 1}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}
	turn := models.AISessionTurn{SessionID: session.ID, TurnSeq: 2, TurnType: "mr_quality_check", Role: "assistant", Status: string(models.AISessionStatusCompleted)}
	if err := db.Create(&turn).Error; err != nil {
		t.Fatalf("create ai session turn failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 11, AgentID: 7, PipelineRunID: 20, NodeID: "mr-review", Params: `{"ai_session_id":` + fmt.Sprint(session.ID) + `}`, Status: models.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create agent task failed: %v", err)
	}

	attempts := buildTaskAttemptSnapshots(db, task)
	if len(attempts) != 1 {
		t.Fatalf("attempt count=%d, want 1", len(attempts))
	}
	if _, exists := attempts[0]["ai_session_turn"]; exists {
		t.Fatalf("expected unmatched turn to be omitted, got %#v", attempts[0]["ai_session_turn"])
	}
}

func TestBuildTaskAttemptSnapshotsDoesNotLeakCrossWorkspaceTurnData(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AgentTask{}, &models.AISession{}, &models.AISessionTurn{}); err != nil {
		t.Fatalf("migrate runtime snapshot tables failed: %v", err)
	}

	session := models.AISession{WorkspaceID: 22, TaskType: "mr_quality_check", Status: models.AISessionStatusQueued, CreatedBy: 1}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create ai session failed: %v", err)
	}
	turn := models.AISessionTurn{SessionID: session.ID, TurnSeq: 1, TurnType: "mr_quality_check", Role: "assistant", Status: string(models.AISessionStatusCompleted)}
	if err := db.Create(&turn).Error; err != nil {
		t.Fatalf("create ai session turn failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: 11, AgentID: 7, PipelineRunID: 21, NodeID: "mr-review", Params: `{"ai_session_id":` + fmt.Sprint(session.ID) + `}`, Status: models.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create agent task failed: %v", err)
	}

	attempts := buildTaskAttemptSnapshots(db, task)
	if len(attempts) != 1 {
		t.Fatalf("attempt count=%d, want 1", len(attempts))
	}
	if _, exists := attempts[0]["ai_session_turn"]; exists {
		t.Fatalf("expected cross-workspace turn to be omitted, got %#v", attempts[0]["ai_session_turn"])
	}
	if _, exists := attempts[0]["ai_session_id"]; exists {
		t.Fatalf("expected cross-workspace session snapshot to be omitted, got %#v", attempts[0]["ai_session_id"])
	}
}

func TestCreateAISessionForNode_RejectsUnresolvedRuntimeSelection(t *testing.T) {
	db := openHandlerTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = originalDB })
	if err := db.AutoMigrate(&models.AIAgent{}, &models.AISession{}); err != nil {
		t.Fatalf("migrate ai tables failed: %v", err)
	}

	aiAgent := models.AIAgent{WorkspaceID: 11, Name: "runtime-agent-without-runtime", Status: models.AIAgentStatusActive, CreatedBy: 1}
	if err := db.Create(&aiAgent).Error; err != nil {
		t.Fatalf("create ai agent failed: %v", err)
	}
	run := &models.PipelineRun{WorkspaceID: 11, TriggerUserID: 1}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	handler := &PipelineHandler{DB: db}
	session, scene, err := handler.createAISessionForNode(db, run, &PipelineNode{ID: "mr-review", Type: "mr_quality_check", Name: "MR Review"}, "mr_quality_check", map[string]any{
		"agent_id":   aiAgent.ID,
		"input_text": "review this MR",
	}, 1)
	if err == nil {
		t.Fatalf("expected unresolved ai runtime error, got session %#v scene %#v", session, scene)
	}
	if !strings.Contains(err.Error(), "ai runtime not resolved") {
		t.Fatalf("unexpected error: %v", err)
	}
	var sessionCount int64
	if err := db.Model(&models.AISession{}).Count(&sessionCount).Error; err != nil {
		t.Fatalf("count ai sessions failed: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("ai session count=%d, want 0", sessionCount)
	}
}
