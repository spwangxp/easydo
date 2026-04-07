package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRunPipeline_AgentNodeReturnsQueuedWithoutPrecreatedTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	pipeline := models.Pipeline{
		Name:        "queued-regression",
		Description: "regression test for queued run",
		OwnerID:     1,
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
	c.Set("user_id", uint64(99))
	c.Set("role", "admin")
	c.Set("username", "demo-user")

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
	if run.TriggerUserID != 99 {
		t.Fatalf("run trigger_user_id=%d, want=99", run.TriggerUserID)
	}
	if run.TriggerUserRole != "admin" {
		t.Fatalf("run trigger_user_role=%s, want=admin", run.TriggerUserRole)
	}

	var taskCount int64
	if err := db.Model(&models.AgentTask{}).Where("pipeline_run_id = ?", run.ID).Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks failed: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no pre-created tasks for queued run, got=%d", taskCount)
	}
}

func TestRunPipeline_ServerOnlyNodeStartsImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	pipeline := models.Pipeline{
		Name:        "server-only-run",
		Description: "server node should start immediately",
		OwnerID:     1,
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
	c.Set("user_id", uint64(7))
	c.Set("role", "user")
	c.Set("username", "tester")

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

	pipeline := models.Pipeline{
		Name:        "manual-runtime-inputs",
		Description: "manual runtime inputs should live in run_config_json",
		OwnerID:     1,
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
	c.Set("user_id", uint64(99))
	c.Set("role", "admin")
	c.Set("username", "demo-user")

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

	pipeline := models.Pipeline{
		Name:        "definition-source-of-truth",
		Description: "run should use definition_json instead of legacy config",
		OwnerID:     1,
		WorkspaceID: 1,
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
	c.Set("user_id", uint64(99))
	c.Set("role", "admin")
	c.Set("username", "demo-user")
	c.Set("workspace_id", pipeline.WorkspaceID)

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
