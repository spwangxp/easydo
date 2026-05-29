package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestPipelineOperationActorFromContextDoesNotInventAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	actor := pipelineOperationActorFromContext(c)
	if actor.UserID != 0 || actor.SystemRole != "" || actor.Username != "" {
		t.Fatalf("actor=%+v, want empty actor when auth context is missing", actor)
	}
}

func createPipelineQueueTestWorkspace(t *testing.T, db *gorm.DB, slug string) models.Workspace {
	t.Helper()
	workspace := models.Workspace{Name: slug, Slug: slug, Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	return workspace
}

func setPipelineOperationTestActor(c *gin.Context, user models.User, workspace models.Workspace) {
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("username", user.Username)
	c.Set("workspace_id", workspace.ID)
}

func TestRunPipelineRejectsMissingWorkspaceContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "missing-workspace-run-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{Name: "missing-workspace-run", OwnerID: user.ID, WorkspaceID: workspace.ID, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("username", user.Username)

	h.RunPipeline(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 when workspace context is missing", w.Code, w.Body.String())
	}
	var runCount int64
	if err := db.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipeline.ID).Count(&runCount).Error; err != nil {
		t.Fatalf("count runs failed: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("expected no run without explicit workspace context, got=%d", runCount)
	}
}

func TestCancelPipelineRunRejectsMissingWorkspaceContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "missing-workspace-cancel-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{Name: "missing-workspace-cancel", OwnerID: user.ID, WorkspaceID: workspace.ID, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/runs/%d/cancel", pipeline.ID, run.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}, {Key: "run_id", Value: strconv.FormatUint(run.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("username", user.Username)

	h.CancelPipelineRun(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 when workspace context is missing", w.Code, w.Body.String())
	}
	var updated models.PipelineRun
	if err := db.First(&updated, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if updated.Status != models.PipelineRunStatusRunning {
		t.Fatalf("run status=%s, want unchanged running", updated.Status)
	}
}

func TestRunPipelineDoesNotInventTriggerUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "missing-username-run-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{Name: "missing-username-run", OwnerID: user.ID, WorkspaceID: workspace.ID, Environment: "testing", Config: `{"version":"2.0","nodes":[],"edges":[]}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("workspace_id", workspace.ID)

	h.RunPipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			RunID uint64 `json:"run_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}
	var run models.PipelineRun
	if err := db.First(&run, resp.Data.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.TriggerUser != "" {
		t.Fatalf("trigger_user=%q, want empty when username context is missing", run.TriggerUser)
	}
}

func TestRunPipeline_AgentNodeReturnsQueuedWithoutPrecreatedTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "queued-regression-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "queued-regression",
		Description: "regression test for queued run",
		OwnerID:     user.ID,
		WorkspaceID: workspace.ID,
		Environment: "testing",
		Config: `{
			"version":"2.0",
			"nodes":[
				{"id":"n1","type":"sleep","name":"Sleep","config":{"seconds":2}}
			],
			"edges":[]
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	setPipelineOperationTestActor(c, user, workspace)

	h.RunPipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			RunID       uint64 `json:"run_id"`
			BuildNumber int    `json:"build_number"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("response code=%d body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.RunID == 0 {
		t.Fatalf("run_id should be set")
	}
	if resp.Data.Status != models.PipelineRunStatusQueued {
		t.Fatalf("response status=%s, want=%s", resp.Data.Status, models.PipelineRunStatusQueued)
	}

	var run models.PipelineRun
	if err := db.First(&run, resp.Data.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.Status != models.PipelineRunStatusQueued {
		t.Fatalf("run status=%s, want=%s", run.Status, models.PipelineRunStatusQueued)
	}
	if run.StartTime != 0 {
		t.Fatalf("run start_time=%d, want=0 for queued run", run.StartTime)
	}
	if run.TriggerUserID != user.ID {
		t.Fatalf("run trigger_user_id=%d, want=%d", run.TriggerUserID, user.ID)
	}
	if run.TriggerUserRole != user.Role {
		t.Fatalf("run trigger_user_role=%s, want=%s", run.TriggerUserRole, user.Role)
	}
	if run.TriggerType != "manual" {
		t.Fatalf("run trigger_type=%s, want manual", run.TriggerType)
	}
	if run.TriggerSource != "pipeline_detail" {
		t.Fatalf("run trigger_source=%s, want pipeline_detail", run.TriggerSource)
	}
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Trigger.Type != "manual" || runConfig.Trigger.Source != "pipeline_detail" || runConfig.Trigger.Operator != user.Username {
		t.Fatalf("run config trigger=%+v, want manual pipeline_detail %s", runConfig.Trigger, user.Username)
	}

	var taskCount int64
	if err := db.Model(&models.AgentTask{}).Where("pipeline_run_id = ?", run.ID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks failed: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no pre-created tasks for queued run, got=%d", taskCount)
	}
}

func TestPipelineOperationTriggerExecutorPersistsMCPTriggerSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "mcp-trigger-source-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "mcp-trigger-source",
		OwnerID:     user.ID,
		WorkspaceID: workspace.ID,
		Environment: "testing",
		Config:      `{"version":"2.0","nodes":[],"edges":[]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	result, err := (pipelineOperationTriggerExecutor{handler: h}).TriggerPipeline(context.Background(), services.TriggerPipelineExecutionRequest{
		Actor: services.ActorContext{
			UserID:     user.ID,
			Username:   user.Username,
			SystemRole: user.Role,
		},
		Pipeline:      pipeline,
		TriggerType:   "mcp",
		TriggerSource: "streamable_http",
	})
	if err != nil {
		t.Fatalf("TriggerPipeline returned error: %v", err)
	}

	var run models.PipelineRun
	if err := db.First(&run, result.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.TriggerType != "mcp" {
		t.Fatalf("trigger_type=%s, want mcp", run.TriggerType)
	}
	if run.TriggerSource != "streamable_http" {
		t.Fatalf("trigger_source=%s, want streamable_http", run.TriggerSource)
	}
	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Trigger.Type != "mcp" || runConfig.Trigger.Source != "streamable_http" || runConfig.Trigger.Operator != user.Username {
		t.Fatalf("run config trigger=%+v, want mcp streamable_http %s", runConfig.Trigger, user.Username)
	}
}

func TestRunPipeline_ServerOnlyNodeStartsImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "server-only-run-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "server-only-run",
		Description: "server node should start immediately",
		OwnerID:     user.ID,
		WorkspaceID: workspace.ID,
		Environment: "testing",
		Config: `{
			"version":"2.0",
			"nodes":[
				{"id":"n1","type":"in_app","name":"Notify","config":{"content":"hello"}}
			],
			"edges":[]
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", user.Role)
	c.Set("username", user.Username)
	c.Set("workspace_id", workspace.ID)

	h.RunPipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			RunID  uint64 `json:"run_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.Status != models.PipelineRunStatusRunning {
		t.Fatalf("response status=%s, want=%s", resp.Data.Status, models.PipelineRunStatusRunning)
	}

	var run models.PipelineRun
	if err := db.First(&run, resp.Data.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
	if run.Status == models.PipelineRunStatusQueued {
		t.Fatalf("run status should not be queued for server-only pipeline, got=%s", run.Status)
	}
	if run.StartTime == 0 {
		t.Fatalf("run start_time should be set for immediate run")
	}
}

func TestRunPipeline_StoresManualNodeScopedInputsWithoutMutatingPipelineSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "manual-runtime-inputs-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "manual-runtime-inputs",
		Description: "manual runtime inputs should live in run_config_json",
		OwnerID:     user.ID,
		WorkspaceID: workspace.ID,
		Environment: "testing",
		Config: `{
			"version":"2.0",
			"nodes":[
				{"id":"n1","type":"shell","name":"Build","config":{"script":"${inputs.script}"}}
			],
			"edges":[]
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := strings.NewReader(`{"inputs":{"n1":{"script":"echo manual override"}}}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	setPipelineOperationTestActor(c, user, workspace)

	h.RunPipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			RunID uint64 `json:"run_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}

	var run models.PipelineRun
	if err := db.First(&run, resp.Data.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}

	var runConfig models.PipelineRunConfigSnapshot
	if err := json.Unmarshal([]byte(run.RunConfig), &runConfig); err != nil {
		t.Fatalf("unmarshal run config failed: %v", err)
	}
	if runConfig.Inputs["n1"]["script"] != "echo manual override" {
		t.Fatalf("expected manual node-scoped inputs in run config, got %#v", runConfig.Inputs)
	}

	var pipelineSnapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &pipelineSnapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if pipelineSnapshot.Nodes[0].Config["script"] != "${inputs.script}" {
		t.Fatalf("expected pipeline snapshot to preserve authored config, got %#v", pipelineSnapshot.Nodes[0].Config)
	}

	var executionConfig PipelineConfig
	if err := json.Unmarshal([]byte(run.Config), &executionConfig); err != nil {
		t.Fatalf("unmarshal legacy execution config failed: %v", err)
	}
	if executionConfig.Nodes[0].Config["script"] != "echo manual override" {
		t.Fatalf("expected execution config to resolve runtime input, got %#v", executionConfig.Nodes[0].Config)
	}
}

func TestRunPipeline_UsesDefinitionJSONAsSourceOfTruth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}
	user, workspace := seedCredentialTestUserAndWorkspace(t, db, "definition-source-of-truth-user", models.WorkspaceRoleDeveloper)

	pipeline := models.Pipeline{
		Name:        "definition-source-of-truth",
		Description: "run should use definition_json instead of legacy config",
		OwnerID:     user.ID,
		WorkspaceID: workspace.ID,
		Environment: "testing",
		Config:      "{invalid-json",
		Definition: `{
			"version":"2.0",
			"nodes":[
				{
					"node_id":"node_1",
					"node_name":"Build",
					"task_key":"shell",
					"task_version":1,
					"params":[{"key":"script","label":"脚本","value":"${inputs.script}","is_flexible":true}],
					"credential_bindings":{},
					"resource_bindings":{},
					"metadata":{"x":100,"y":100}
				}
			],
			"edges":[],
			"triggers":[],
			"metadata":{"version":"2.0"}
		}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	body := strings.NewReader(`{"inputs":{"node_1":{"script":"echo from definition"}}}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/pipelines/%d/run", pipeline.ID), body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(pipeline.ID, 10)}}
	setPipelineOperationTestActor(c, user, workspace)

	h.RunPipeline(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			RunID uint64 `json:"run_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v, body=%s", err, w.Body.String())
	}

	var run models.PipelineRun
	if err := db.First(&run, resp.Data.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}

	var snapshot PipelineConfig
	if err := json.Unmarshal([]byte(run.PipelineSnapshot), &snapshot); err != nil {
		t.Fatalf("unmarshal pipeline snapshot failed: %v", err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ID != "node_1" {
		t.Fatalf("expected definition snapshot node_1, got %#v", snapshot.Nodes)
	}
	if len(snapshot.Nodes[0].DefinitionParams) != 1 || snapshot.Nodes[0].DefinitionParams[0].Value != "${inputs.script}" {
		t.Fatalf("expected authored params from definition_json, got %#v", snapshot.Nodes[0].DefinitionParams)
	}

	var executionConfig PipelineConfig
	if err := json.Unmarshal([]byte(run.Config), &executionConfig); err != nil {
		t.Fatalf("unmarshal execution config failed: %v", err)
	}
	if executionConfig.Nodes[0].Config["script"] != "echo from definition" {
		t.Fatalf("expected execution config to resolve definition_json runtime input, got %#v", executionConfig.Nodes[0].Config)
	}
}

func TestPipelineRun_BuildNumberIsUniquePerPipeline(t *testing.T) {
	db := openHandlerTestDB(t)

	first := models.PipelineRun{PipelineID: 11, BuildNumber: 1, Status: models.PipelineRunStatusQueued}
	second := models.PipelineRun{PipelineID: 11, BuildNumber: 1, Status: models.PipelineRunStatusQueued}

	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first run failed: %v", err)
	}
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("expected duplicate build number insert to fail")
	}
}
