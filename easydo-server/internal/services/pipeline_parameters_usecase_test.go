package services

import (
	"context"
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestPipelineParameterSchemaReturnsManualParamsAndNestedExample(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-param-schema-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{
		Name:        "schema-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Definition:  `{"version":"2.0","nodes":[{"node_id":"node-build","node_name":"Build","task_key":"shell","params":[{"key":"script","label":"Script","value":"echo default","is_flexible":true},{"key":"internal_timeout","label":"Timeout","value":30,"is_flexible":false}]}]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	result, err := usecase.GetPipelineParameterSchema(context.Background(), GetPipelineParameterSchemaRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
	})
	if err != nil {
		t.Fatalf("GetPipelineParameterSchema returned error: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("nodes=%+v, want one manual node", result.Nodes)
	}
	node := result.Nodes[0]
	if node.NodeID != "node-build" || node.NodeName != "Build" || node.TaskType != "shell" {
		t.Fatalf("node=%+v, want normalized node metadata", node)
	}
	if len(node.Params) != 1 || node.Params[0].Key != "script" || !node.Params[0].IsFlexible {
		t.Fatalf("params=%+v, want only flexible script param", node.Params)
	}
	if !strings.Contains(result.Prompt, "不得自行沿用当前值") || len(result.InputPrompts) != 1 {
		t.Fatalf("prompt=%q input_prompts=%+v, want explicit user interaction prompt", result.Prompt, result.InputPrompts)
	}
	if result.InputPrompts[0].InputPath != "inputs.node-build.script" || result.InputPrompts[0].Message == "" {
		t.Fatalf("input prompt=%+v, want nested input path and message", result.InputPrompts[0])
	}
	if len(result.ParameterTable) != 1 {
		t.Fatalf("parameter_table=%+v, want one table row", result.ParameterTable)
	}
	tableRow := result.ParameterTable[0]
	if tableRow.ParamName != "script" || tableRow.ValueType != "string" || tableRow.DefaultValue != "echo default" || tableRow.TaskName != "Build" || tableRow.TaskType != "shell" {
		t.Fatalf("table row=%+v, want param/task/default/type columns", tableRow)
	}
	if result.ExampleInputs["node-build"]["script"] != "echo default" {
		t.Fatalf("example inputs=%+v, want nested node/key payload", result.ExampleInputs)
	}
}

func TestPipelineTriggerPreviewReportsMissingAndUnknownParams(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-preview-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{
		Name:        "preview-pipeline",
		WorkspaceID: workspace.ID,
		OwnerID:     user.ID,
		Config:      `{"version":"2.0","nodes":[{"id":"node-deploy","name":"Deploy","type":"shell","params":[{"key":"image","label":"Image","value":"repo/app:latest","is_flexible":true}]}]}`,
	}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	result, err := usecase.PreviewPipelineTriggerParameters(context.Background(), PreviewPipelineTriggerParametersRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		Inputs: map[string]map[string]any{
			"node-deploy": {"unknown": "value"},
			"ghost-node":  {"image": "repo/app:v2"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewPipelineTriggerParameters returned error: %v", err)
	}
	if result.CanTrigger {
		t.Fatalf("preview=%+v, want can_trigger false", result)
	}
	if len(result.Missing) != 1 || result.Missing[0].NodeID != "node-deploy" || result.Missing[0].Key != "image" {
		t.Fatalf("missing=%+v, want missing image", result.Missing)
	}
	if !strings.Contains(result.Prompt, "不得自行沿用当前值") || len(result.InputPrompts) != 1 || result.InputPrompts[0].Key != "image" {
		t.Fatalf("prompt=%q input_prompts=%+v, want missing parameter prompt", result.Prompt, result.InputPrompts)
	}
	if len(result.Unknown) != 2 {
		t.Fatalf("unknown=%+v, want unknown node and param", result.Unknown)
	}
	if result.ExampleInputs["node-deploy"]["image"] != "repo/app:latest" {
		t.Fatalf("example inputs=%+v, want default image", result.ExampleInputs)
	}

	complete, err := usecase.PreviewPipelineTriggerParameters(context.Background(), PreviewPipelineTriggerParametersRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		Inputs: map[string]map[string]any{
			"node-deploy": {"image": "repo/app:v2"},
		},
	})
	if err != nil {
		t.Fatalf("PreviewPipelineTriggerParameters complete returned error: %v", err)
	}
	if !complete.CanTrigger || len(complete.Missing) != 0 || len(complete.Unknown) != 0 {
		t.Fatalf("complete preview=%+v, want triggerable with no validation issues", complete)
	}
	if !strings.Contains(complete.Prompt, "不得自行沿用当前值") || len(complete.ParameterTable) != 1 || complete.ParameterTable[0].ParamName != "image" {
		t.Fatalf("complete prompt=%q parameter_table=%+v, want interaction table even when inputs are complete", complete.Prompt, complete.ParameterTable)
	}
}

func TestPipelineRunParametersGetReturnsHistoricalParameterView(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-history-params-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "history-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{
		WorkspaceID:      workspace.ID,
		PipelineID:       pipeline.ID,
		BuildNumber:      9,
		Status:           models.PipelineRunStatusSuccess,
		TriggerType:      "manual",
		TriggerSource:    "mcp",
		TriggerUser:      user.Username,
		RunConfig:        `{"trigger":{"type":"mcp","source":"streamable_http","operator":"admin"},"inputs":{"node-build":{"script":"echo runtime"}}}`,
		PipelineSnapshot: `{"version":"2.0","nodes":[{"node_id":"node-build","node_name":"Build","task_key":"shell","params":[{"key":"script","label":"Script","value":"echo default","is_flexible":true},{"key":"password","label":"Password","value":"hidden","is_flexible":true}]}]}`,
		ResolvedNodes:    `[{"node_id":"node-build","resolved_inputs":{"script":"echo resolved"}}]`,
		BindingsSnapshot: `{"token":"hidden"}`,
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	result, err := usecase.GetPipelineRunParameters(context.Background(), GetPipelineRunParametersRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		RunID:       run.ID,
	})
	if err != nil {
		t.Fatalf("GetPipelineRunParameters returned error: %v", err)
	}
	if result.RunID != run.ID || result.BuildNumber != 9 || result.Trigger.Source != "streamable_http" {
		t.Fatalf("result metadata=%+v, want run/build/trigger", result)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].NodeID != "node-build" {
		t.Fatalf("nodes=%+v, want node-build", result.Nodes)
	}
	node := result.Nodes[0]
	if len(node.RuntimeParams) != 1 || node.RuntimeParams[0].Key != "script" || node.RuntimeParams[0].Value != "echo runtime" {
		t.Fatalf("runtime params=%+v, want runtime script", node.RuntimeParams)
	}
	if len(node.DefaultParams) != 1 || node.DefaultParams[0].Key != "script" || !node.DefaultParams[0].Overridden {
		t.Fatalf("default params=%+v, want overridden script and no password", node.DefaultParams)
	}
}
