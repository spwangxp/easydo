package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openPipelineQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openActorTestDB(t)
	if err := db.AutoMigrate(&models.Pipeline{}, &models.PipelineRun{}, &models.AgentTask{}); err != nil {
		t.Fatalf("auto migrate pipeline query models failed: %v", err)
	}
	return db
}

func seedPipelineQueryWorkspaceMember(t *testing.T, db *gorm.DB, username string, role string) (models.User, models.Workspace) {
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

func TestPipelineQueryListPipelinesScopesToWorkspace(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-list-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-other-user", models.WorkspaceRoleViewer)

	visible := models.Pipeline{Name: "visible-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Description: "kept", Config: `{"secret":"hidden"}`}
	hidden := models.Pipeline{Name: "hidden-pipeline", WorkspaceID: otherWorkspace.ID, OwnerID: user.ID, Description: "other workspace", Config: `{"secret":"hidden"}`}
	for _, pipeline := range []*models.Pipeline{&visible, &hidden} {
		if err := db.Create(pipeline).Error; err != nil {
			t.Fatalf("create pipeline failed: %v", err)
		}
	}

	result, err := usecase.ListPipelines(context.Background(), ListPipelinesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("ListPipelines returned error: %v", err)
	}
	if result.Total != 1 {
		t.Fatalf("total=%d, want 1", result.Total)
	}
	if len(result.List) != 1 || result.List[0].ID != visible.ID {
		t.Fatalf("list=%+v, want only pipeline %d", result.List, visible.ID)
	}
	serialized, err := json.Marshal(result.List[0])
	if err != nil {
		t.Fatalf("marshal summary failed: %v", err)
	}
	body := string(serialized)
	for _, forbidden := range []string{"config", "credential", "secret", "token"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("summary leaked forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestPipelineQueryGetPipelineRejectsOtherWorkspace(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, _ := seedPipelineQueryWorkspaceMember(t, db, "pipeline-forbidden-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-forbidden-owner", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "other-workspace-pipeline", WorkspaceID: otherWorkspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	_, err := usecase.GetPipeline(context.Background(), GetPipelineRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: otherWorkspace.ID, PipelineID: pipeline.ID})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeForbidden {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeForbidden)
	}
}

func TestPipelineQueryListPipelineRunsUsesPagingAndWorkspaceScope(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-run-list-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "run-history-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	_, otherWorkspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-run-list-other", models.WorkspaceRoleViewer)
	otherPipeline := models.Pipeline{Name: "other-pipeline", WorkspaceID: otherWorkspace.ID, OwnerID: user.ID}
	if err := db.Create(&otherPipeline).Error; err != nil {
		t.Fatalf("create other pipeline failed: %v", err)
	}
	for i := 1; i <= 3; i++ {
		run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: i, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", TriggerUser: user.Username}
		if err := db.Create(&run).Error; err != nil {
			t.Fatalf("create run failed: %v", err)
		}
	}
	otherRun := models.PipelineRun{WorkspaceID: otherWorkspace.ID, PipelineID: otherPipeline.ID, BuildNumber: 99, Status: models.PipelineRunStatusFailed, TriggerType: "manual", TriggerUser: "other"}
	if err := db.Create(&otherRun).Error; err != nil {
		t.Fatalf("create other run failed: %v", err)
	}

	result, err := usecase.ListPipelineRuns(context.Background(), ListPipelineRunsRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, Page: 2, Limit: 2})
	if err != nil {
		t.Fatalf("ListPipelineRuns returned error: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total=%d, want 3", result.Total)
	}
	if result.Page != 2 || result.Limit != 2 {
		t.Fatalf("page=%d limit=%d, want page=2 limit=2", result.Page, result.Limit)
	}
	if len(result.List) != 1 || result.List[0].BuildNumber != 1 {
		t.Fatalf("list=%+v, want only build 1 on second page", result.List)
	}
}

func TestPipelineQueryGetPipelineRunReturnsNodeAndTaskSummaryWithoutLeaks(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-run-get-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "detail-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"token":"pipeline-token"}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	resolvedNodes := `[{"node_id":"node-1","task_key":"shell","task_type":"shell","name":"Build","status":"success","credentials":{"ssh":{"credential_id":1}},"config":{"script":"echo hi"}}]`
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 7, Status: models.PipelineRunStatusSuccess, TriggerType: "manual", TriggerUser: user.Username, Config: `{"secret":"run-secret"}`, PipelineSnapshot: `{"config":{"secret":"run-secret"}}`, ResolvedNodes: resolvedNodes, BindingsSnapshot: `{"credentials":{"node-1":{"ssh":{"credential_id":1}}}}`, Outputs: `{"token":"run-output-token","summary":"ok"}`}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: 9, PipelineRunID: run.ID, NodeID: "node-1", TaskType: "shell", Name: "Build", Status: models.TaskStatusExecuteSuccess, ExitCode: 0, ErrorMsg: "", ResultData: `{"stdout":"hello","credential":"keep-hidden"}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	result, err := usecase.GetPipelineRun(context.Background(), GetPipelineRunRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, RunID: run.ID})
	if err != nil {
		t.Fatalf("GetPipelineRun returned error: %v", err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].NodeID != "node-1" {
		t.Fatalf("nodes=%+v, want node summary", result.Nodes)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].NodeID != "node-1" {
		t.Fatalf("tasks=%+v, want task summary", result.Tasks)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal run detail failed: %v", err)
	}
	body := strings.ToLower(string(serialized))
	for _, forbidden := range []string{"config", "credential", "token", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("run detail leaked forbidden field %q: %s", forbidden, string(serialized))
		}
	}
}

func TestPipelineQueryGetPipelineTaskTruncatesLargeOutput(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-task-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "task-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	largeOutput := strings.Repeat("x", 5000)
	task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: 10, PipelineRunID: run.ID, NodeID: "node-1", TaskType: "shell", Name: "Build", Status: models.TaskStatusExecuteFailed, ErrorMsg: largeOutput, ResultData: `{"stdout":"` + largeOutput + `"}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	result, err := usecase.GetPipelineTask(context.Background(), GetPipelineTaskRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, RunID: run.ID, TaskID: task.ID})
	if err != nil {
		t.Fatalf("GetPipelineTask returned error: %v", err)
	}
	if !strings.Contains(result.ErrorMsg, "[truncated]") {
		t.Fatalf("error_msg was not truncated: %q", result.ErrorMsg)
	}
	payload, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type=%T, want map[string]any", result.Result)
	}
	stdout, _ := payload["stdout"].(string)
	if !strings.Contains(stdout, "[truncated]") {
		t.Fatalf("stdout was not truncated: %q", stdout)
	}
}

func TestPipelineQueryRedactsSensitiveStringContent(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-sensitive-string-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "sensitive-string-pipeline", Description: "secret=pipeline-description-secret", Environment: "token=pipeline-environment-token", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	resolvedNodes := `[{"node_id":"token=node-token-value","task_key":"secret=node-task-key-secret","task_type":"credential=node-task-type-credential","name":"password=node-name-password","status":"api_key=node-status-api-key"}]`
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusFailed, TriggerSource: "https://hooks.example/run?token=run-token-value", TriggerUser: "secret=trigger-user-secret", TriggerUserRole: "token=trigger-role-token", ResolvedNodes: resolvedNodes}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: 10, PipelineRunID: run.ID, NodeID: "token=task-node-token", TaskType: "secret=task-type-secret", Name: "password=task-name-password", Status: "api_key=task-status-api-key", ErrorMsg: "password=task-password-value", ResultData: `{"stdout":"token=stdout-token-value","nested":{"message":"Authorization: Bearer nested-secret-value"}}`}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	pipelineResult, err := usecase.GetPipeline(context.Background(), GetPipelineRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID})
	if err != nil {
		t.Fatalf("GetPipeline returned error: %v", err)
	}
	runResult, err := usecase.GetPipelineRun(context.Background(), GetPipelineRunRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, RunID: run.ID})
	if err != nil {
		t.Fatalf("GetPipelineRun returned error: %v", err)
	}
	taskResult, err := usecase.GetPipelineTask(context.Background(), GetPipelineTaskRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, RunID: run.ID, TaskID: task.ID})
	if err != nil {
		t.Fatalf("GetPipelineTask returned error: %v", err)
	}
	for name, value := range map[string]any{"pipeline": pipelineResult, "run": runResult, "task": taskResult} {
		serialized, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s result failed: %v", name, err)
		}
		body := strings.ToLower(string(serialized))
		for _, forbidden := range []string{
			"pipeline-description-secret",
			"pipeline-environment-token",
			"run-token-value",
			"trigger-user-secret",
			"trigger-role-token",
			"node-token-value",
			"node-task-key-secret",
			"node-task-type-credential",
			"node-name-password",
			"node-status-api-key",
			"task-node-token",
			"task-type-secret",
			"task-name-password",
			"task-status-api-key",
			"task-password-value",
			"stdout-token-value",
			"nested-secret-value",
			"authorization: bearer",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s result leaked sensitive text %q: %s", name, forbidden, string(serialized))
			}
		}
	}
}

type pipelineQuerySQLRecorder struct {
	statements []string
}

func (r *pipelineQuerySQLRecorder) LogMode(logger.LogLevel) logger.Interface {
	return r
}

func (r *pipelineQuerySQLRecorder) Info(context.Context, string, ...any) {}

func (r *pipelineQuerySQLRecorder) Warn(context.Context, string, ...any) {}

func (r *pipelineQuerySQLRecorder) Error(context.Context, string, ...any) {}

func (r *pipelineQuerySQLRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

func TestPipelineQueryGetPipelineTaskDoesNotLoadFullRunTasks(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-task-query-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "task-query-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	for _, nodeID := range []string{"target", "sibling"} {
		task := models.AgentTask{WorkspaceID: workspace.ID, AgentID: 10, PipelineRunID: run.ID, NodeID: nodeID, TaskType: "shell", Name: nodeID, Status: models.TaskStatusExecuteSuccess}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create task %s failed: %v", nodeID, err)
		}
	}
	var target models.AgentTask
	if err := db.Where("node_id = ?", "target").First(&target).Error; err != nil {
		t.Fatalf("load target task failed: %v", err)
	}
	recorder := &pipelineQuerySQLRecorder{}
	usecase := &PipelineQueryUseCase{DB: db.Session(&gorm.Session{Logger: recorder})}

	_, err := usecase.GetPipelineTask(context.Background(), GetPipelineTaskRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID, RunID: run.ID, TaskID: target.ID})
	if err != nil {
		t.Fatalf("GetPipelineTask returned error: %v", err)
	}
	taskSelects := 0
	for _, statement := range recorder.statements {
		lower := strings.ToLower(statement)
		if strings.Contains(lower, "from `agent_tasks`") || strings.Contains(lower, "from \"agent_tasks\"") || strings.Contains(lower, "from agent_tasks") {
			taskSelects++
		}
		if (strings.Contains(lower, "from `pipeline_runs`") || strings.Contains(lower, "from \"pipeline_runs\"") || strings.Contains(lower, "from pipeline_runs")) && strings.Contains(lower, "select *") {
			t.Fatalf("pipeline run existence check selected full row: %s", statement)
		}
	}
	if taskSelects != 1 {
		t.Fatalf("agent task select count=%d, want 1; statements=%v", taskSelects, recorder.statements)
	}
}

func TestPipelineQueryRejectsNonMemberWorkspace(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user := models.User{Username: "pipeline-nonmember-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "private-workspace", Slug: "private-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID + 100}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	_, err := usecase.ListPipelines(context.Background(), ListPipelinesRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeForbidden {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeForbidden)
	}
}

func TestPipelineQueryViewerCanReadWorkspacePipelines(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineQueryUseCase{DB: db}
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, "pipeline-viewer-user", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "viewer-visible-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	result, err := usecase.GetPipeline(context.Background(), GetPipelineRequest{Actor: ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, WorkspaceID: workspace.ID, PipelineID: pipeline.ID})
	if err != nil {
		t.Fatalf("GetPipeline returned error for viewer role: %v", err)
	}
	if result.ID != pipeline.ID {
		t.Fatalf("pipeline id=%d, want %d", result.ID, pipeline.ID)
	}
}
