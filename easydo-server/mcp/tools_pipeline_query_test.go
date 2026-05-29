package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"gorm.io/gorm"
)

func openPipelineToolTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openAuditTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.Pipeline{}, &models.PipelineRun{}, &models.AgentTask{}); err != nil {
		t.Fatalf("auto migrate pipeline tool models failed: %v", err)
	}
	return db
}

func seedPipelineToolWorkspaceMember(t *testing.T, db *gorm.DB, username string, role string) (models.User, models.Workspace) {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: username + "-workspace", Slug: username + "-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: role, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}
	return user, workspace
}

func TestPipelineListToolRequiresExplicitWorkspaceIDAndCapsLimit(t *testing.T) {
	db := openPipelineToolTestDB(t)
	usecase := &services.PipelineQueryUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterPipelineTools(registry, usecase); err != nil {
		t.Fatalf("RegisterPipelineTools returned error: %v", err)
	}
	user, workspace := seedPipelineToolWorkspaceMember(t, db, "pipeline-tool-list-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "tool-visible-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_list", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"limit": 500}})
	if err == nil {
		t.Fatal("expected workspace_id invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeInvalidArgument)
	}

	result, err := registry.Invoke(context.Background(), "easydo_pipeline_list", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "page": json.Number("1"), "limit": "500"}})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(services.PipelineListResult)
	if !ok {
		t.Fatalf("structured content type=%T, want services.PipelineListResult", result.StructuredContent)
	}
	if content.Limit != 100 {
		t.Fatalf("limit=%d, want 100", content.Limit)
	}
	if len(content.List) != 1 || content.List[0].ID != pipeline.ID {
		t.Fatalf("list=%+v, want pipeline %d", content.List, pipeline.ID)
	}
}

func TestPipelineGetToolPassesThroughForbiddenAndNotFound(t *testing.T) {
	db := openPipelineToolTestDB(t)
	usecase := &services.PipelineQueryUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterPipelineTools(registry, usecase); err != nil {
		t.Fatalf("RegisterPipelineTools returned error: %v", err)
	}
	user, workspace := seedPipelineToolWorkspaceMember(t, db, "pipeline-tool-get-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedPipelineToolWorkspaceMember(t, db, "pipeline-tool-other-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "tool-get-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": otherWorkspace.ID, "pipeline_id": pipeline.ID}})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeForbidden {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeForbidden)
	}

	_, err = registry.Invoke(context.Background(), "easydo_pipeline_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "pipeline_id": pipeline.ID + 999}})
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeNotFound {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeNotFound)
	}
}

func TestPipelineRunAndTaskToolsReturnStructuredContentWithoutSensitiveFields(t *testing.T) {
	db := openPipelineToolTestDB(t)
	usecase := &services.PipelineQueryUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterPipelineTools(registry, usecase); err != nil {
		t.Fatalf("RegisterPipelineTools returned error: %v", err)
	}
	user, workspace := seedPipelineToolWorkspaceMember(t, db, "pipeline-tool-run-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "tool-run-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"secret":"pipeline-secret"}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 3, Status: models.PipelineRunStatusFailed, TriggerType: "manual", TriggerUser: user.Username, Config: `{"token":"run-token"}`, ResolvedNodes: `[{"node_id":"node-1","task_key":"shell","task_type":"shell","name":"Build","status":"failed","config":{"script":"echo hi"}}]`}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: 8, PipelineRunID: run.ID, NodeID: "node-1", TaskType: "shell", Name: "Build", Status: models.TaskStatusExecuteFailed, ErrorMsg: strings.Repeat("y", 5000), ResultData: `{"stdout":"ok","secret":"hidden"}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	runListResult, err := registry.Invoke(context.Background(), "easydo_pipeline_run_list", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "pipeline_id": pipeline.ID, "page": 1, "limit": 20}})
	if err != nil {
		t.Fatalf("run list invoke returned error: %v", err)
	}
	if _, ok := runListResult.StructuredContent.(services.PipelineRunListResult); !ok {
		t.Fatalf("run list structured content type=%T, want services.PipelineRunListResult", runListResult.StructuredContent)
	}

	runGetResult, err := registry.Invoke(context.Background(), "easydo_pipeline_run_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "pipeline_id": pipeline.ID, "run_id": run.ID}})
	if err != nil {
		t.Fatalf("run get invoke returned error: %v", err)
	}
	runDetail, ok := runGetResult.StructuredContent.(services.PipelineRunDetail)
	if !ok {
		t.Fatalf("run get structured content type=%T, want services.PipelineRunDetail", runGetResult.StructuredContent)
	}
	if len(runDetail.Nodes) != 1 || len(runDetail.Tasks) != 1 {
		t.Fatalf("run detail=%+v, want one node and one task", runDetail)
	}
	serializedRun, err := json.Marshal(runDetail)
	if err != nil {
		t.Fatalf("marshal run detail failed: %v", err)
	}
	lowerRun := strings.ToLower(string(serializedRun))
	for _, forbidden := range []string{"config", "credential", "token", "secret"} {
		if strings.Contains(lowerRun, forbidden) {
			t.Fatalf("run detail leaked forbidden field %q: %s", forbidden, string(serializedRun))
		}
	}

	taskGetResult, err := registry.Invoke(context.Background(), "easydo_pipeline_task_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "pipeline_id": pipeline.ID, "run_id": run.ID, "task_id": task.ID}})
	if err != nil {
		t.Fatalf("task get invoke returned error: %v", err)
	}
	taskDetail, ok := taskGetResult.StructuredContent.(services.PipelineTaskSummary)
	if !ok {
		t.Fatalf("task get structured content type=%T, want services.PipelineTaskSummary", taskGetResult.StructuredContent)
	}
	if !strings.Contains(taskDetail.ErrorMsg, "[truncated]") {
		t.Fatalf("task error_msg was not truncated: %q", taskDetail.ErrorMsg)
	}
	serializedTask, err := json.Marshal(taskDetail)
	if err != nil {
		t.Fatalf("marshal task detail failed: %v", err)
	}
	lowerTask := strings.ToLower(string(serializedTask))
	for _, forbidden := range []string{"config", "credential", "token", "secret"} {
		if strings.Contains(lowerTask, forbidden) {
			t.Fatalf("task detail leaked forbidden field %q: %s", forbidden, string(serializedTask))
		}
	}
}
