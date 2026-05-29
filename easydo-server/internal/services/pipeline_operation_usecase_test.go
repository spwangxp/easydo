package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

type testPipelineTriggerRunner struct {
	db      *gorm.DB
	called  int
	lastReq TriggerPipelineExecutionRequest
}

func (r *testPipelineTriggerRunner) TriggerPipeline(ctx context.Context, req TriggerPipelineExecutionRequest) (TriggerPipelineResult, error) {
	r.called++
	r.lastReq = req
	run := models.PipelineRun{
		WorkspaceID: req.Pipeline.WorkspaceID,
		PipelineID:  req.Pipeline.ID,
		BuildNumber: 12,
		Status:      models.PipelineRunStatusQueued,
		TriggerType: "manual",
		TriggerUser: req.Actor.Username,
	}
	if err := r.db.WithContext(ctx).Create(&run).Error; err != nil {
		return TriggerPipelineResult{}, err
	}
	return TriggerPipelineResult{RunID: run.ID, BuildNumber: run.BuildNumber, Status: run.Status}, nil
}

type testPipelineOperationNotifier struct {
	taskRetries []models.AgentTask
	taskCancels []models.AgentTask
	taskUpdates []models.AgentTask
	runUpdates  []models.PipelineRun
}

func (n *testPipelineOperationNotifier) NotifyTaskRetry(task models.AgentTask) {
	n.taskRetries = append(n.taskRetries, task)
}

func (n *testPipelineOperationNotifier) NotifyTaskCancel(task models.AgentTask) {
	n.taskCancels = append(n.taskCancels, task)
}

func (n *testPipelineOperationNotifier) NotifyTaskStatus(runID uint64, task models.AgentTask, message string) {
	n.taskUpdates = append(n.taskUpdates, task)
}

func (n *testPipelineOperationNotifier) NotifyRunStatus(run models.PipelineRun, message string) {
	n.runUpdates = append(n.runUpdates, run)
}

type testPipelineOperationHooks struct {
	cancelRequested []models.PipelineRun
	cancelled       []models.PipelineRun
}

func (h *testPipelineOperationHooks) OnRunCancelRequested(ctx context.Context, run models.PipelineRun) error {
	h.cancelRequested = append(h.cancelRequested, run)
	return nil
}

func (h *testPipelineOperationHooks) OnRunCancelled(ctx context.Context, run models.PipelineRun) error {
	h.cancelled = append(h.cancelled, run)
	return nil
}

func newPipelineOperationUseCaseForTest(t *testing.T) (*PipelineOperationUseCase, *gorm.DB, models.User, models.Workspace, *testPipelineOperationNotifier, *testPipelineOperationHooks) {
	t.Helper()
	db := openPipelineQueryTestDB(t)
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, t.Name()+"-user", models.WorkspaceRoleDeveloper)
	notifier := &testPipelineOperationNotifier{}
	hooks := &testPipelineOperationHooks{}
	usecase := &PipelineOperationUseCase{
		DB:       db,
		Notifier: notifier,
		Hooks:    hooks,
		Now: func() time.Time {
			return time.Unix(1700000000, 0).UTC()
		},
	}
	return usecase, db, user, workspace, notifier, hooks
}

func TestPipelineOperationTriggerCreatesRunThroughUseCase(t *testing.T) {
	usecase, db, user, workspace, _, _ := newPipelineOperationUseCaseForTest(t)
	pipeline := models.Pipeline{Name: "trigger-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Config: `{"version":"2.0"}`}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	runner := &testPipelineTriggerRunner{db: db}
	usecase.TriggerRunner = runner

	result, err := usecase.TriggerPipeline(context.Background(), TriggerPipelineRequest{
		Actor:         ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID:   workspace.ID,
		PipelineID:    pipeline.ID,
		Inputs:        map[string]map[string]any{"node-1": {"script": "echo hi"}},
		Options:       map[string]any{"source": "manual"},
		TriggerType:   "mcp",
		TriggerSource: "streamable_http",
	})
	if err != nil {
		t.Fatalf("TriggerPipeline returned error: %v", err)
	}
	if runner.called != 1 {
		t.Fatalf("trigger runner calls=%d, want 1", runner.called)
	}
	if runner.lastReq.Pipeline.ID != pipeline.ID {
		t.Fatalf("runner pipeline id=%d, want %d", runner.lastReq.Pipeline.ID, pipeline.ID)
	}
	if runner.lastReq.Workspace.WorkspaceID != workspace.ID {
		t.Fatalf("runner workspace id=%d, want %d", runner.lastReq.Workspace.WorkspaceID, workspace.ID)
	}
	if runner.lastReq.RunConfig.Inputs["node-1"]["script"] != "echo hi" {
		t.Fatalf("runner inputs=%v", runner.lastReq.RunConfig.Inputs)
	}
	if runner.lastReq.TriggerType != "mcp" {
		t.Fatalf("runner trigger type=%q, want mcp", runner.lastReq.TriggerType)
	}
	if runner.lastReq.TriggerSource != "streamable_http" {
		t.Fatalf("runner trigger source=%q, want streamable_http", runner.lastReq.TriggerSource)
	}
	if result.RunID == 0 || result.BuildNumber != 12 || result.Status != models.PipelineRunStatusQueued {
		t.Fatalf("result=%+v", result)
	}
	var run models.PipelineRun
	if err := db.First(&run, result.RunID).Error; err != nil {
		t.Fatalf("load run failed: %v", err)
	}
}

func TestPipelineOperationCancelRejectsTerminalRun(t *testing.T) {
	usecase, db, user, workspace, _, _ := newPipelineOperationUseCaseForTest(t)
	pipeline := models.Pipeline{Name: "terminal-cancel-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusSuccess}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}

	_, err := usecase.CancelPipelineRun(context.Background(), CancelPipelineRunRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		RunID:       run.ID,
	})
	if err == nil {
		t.Fatal("expected terminal run rejection")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeInvalidArgument)
	}
}

func TestPipelineOperationCancelMarksCancelableTasks(t *testing.T) {
	usecase, db, user, workspace, notifier, hooks := newPipelineOperationUseCaseForTest(t)
	pipeline := models.Pipeline{Name: "cancel-running-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning, StartTime: 1699999900}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	tasks := []models.AgentTask{
		{WorkspaceID: workspace.ID, PipelineRunID: run.ID, NodeID: "done", TaskType: "shell", Name: "Done", Status: models.TaskStatusExecuteSuccess, StartTime: 1699999900, EndTime: 1699999950, Duration: 50},
		{WorkspaceID: workspace.ID, PipelineRunID: run.ID, NodeID: "run", TaskType: "shell", Name: "Run", Status: models.TaskStatusRunning, StartTime: 1699999970},
		{WorkspaceID: workspace.ID, PipelineRunID: run.ID, NodeID: "queue", TaskType: "shell", Name: "Queue", Status: models.TaskStatusQueued},
	}
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("create task failed: %v", err)
		}
	}

	result, err := usecase.CancelPipelineRun(context.Background(), CancelPipelineRunRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		RunID:       run.ID,
	})
	if err != nil {
		t.Fatalf("CancelPipelineRun returned error: %v", err)
	}
	if result.Status != models.PipelineRunStatusCancelRequested {
		t.Fatalf("result status=%s, want %s", result.Status, models.PipelineRunStatusCancelRequested)
	}
	var updatedRun models.PipelineRun
	if err := db.First(&updatedRun, run.ID).Error; err != nil {
		t.Fatalf("reload run failed: %v", err)
	}
	if updatedRun.Status != models.PipelineRunStatusCancelRequested {
		t.Fatalf("run status=%s, want %s", updatedRun.Status, models.PipelineRunStatusCancelRequested)
	}
	var updatedTasks []models.AgentTask
	if err := db.Where("pipeline_run_id = ?", run.ID).Order("node_id ASC").Find(&updatedTasks).Error; err != nil {
		t.Fatalf("reload tasks failed: %v", err)
	}
	if updatedTasks[0].Status != models.TaskStatusExecuteSuccess {
		t.Fatalf("done task status=%s, want %s", updatedTasks[0].Status, models.TaskStatusExecuteSuccess)
	}
	if updatedTasks[1].Status != models.TaskStatusCancelled {
		t.Fatalf("queued task status=%s, want %s", updatedTasks[1].Status, models.TaskStatusCancelled)
	}
	if updatedTasks[2].Status != models.TaskStatusCancelRequested {
		t.Fatalf("running task status=%s, want %s", updatedTasks[2].Status, models.TaskStatusCancelRequested)
	}
	if len(notifier.taskCancels) != 1 || notifier.taskCancels[0].NodeID != "run" {
		t.Fatalf("task cancels=%+v, want only running task", notifier.taskCancels)
	}
	if len(hooks.cancelRequested) != 1 || hooks.cancelRequested[0].ID != run.ID {
		t.Fatalf("cancel requested hooks=%+v", hooks.cancelRequested)
	}
}

func TestPipelineOperationRetryRejectsNonFailedTask(t *testing.T) {
	usecase, db, user, workspace, _, _ := newPipelineOperationUseCaseForTest(t)
	task := models.AgentTask{WorkspaceID: workspace.ID, PipelineRunID: 1, NodeID: "node-1", AgentID: 1, TaskType: "shell", Name: "retry", Status: models.TaskStatusRunning, MaxRetries: 2}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	_, err := usecase.RetryPipelineTask(context.Background(), RetryPipelineTaskRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		TaskID:      task.ID,
	})
	if err == nil {
		t.Fatal("expected retry rejection")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeInvalidArgument)
	}
}

func TestPipelineOperationRetryResetsDispatchFields(t *testing.T) {
	usecase, db, user, workspace, notifier, _ := newPipelineOperationUseCaseForTest(t)
	task := models.AgentTask{
		WorkspaceID:     workspace.ID,
		PipelineRunID:   1,
		NodeID:          "node-1",
		AgentID:         9,
		TaskType:        "shell",
		Name:            "retry",
		Status:          models.TaskStatusExecuteFailed,
		RetryCount:      0,
		MaxRetries:      3,
		StartTime:       10,
		EndTime:         20,
		Duration:        10,
		ExitCode:        1,
		ErrorMsg:        "boom",
		ResultData:      `{"stdout":"failed"}`,
		DispatchToken:   "old-token",
		DispatchAttempt: 4,
		LeaseExpireAt:   99,
		AgentSessionID:  "session-old",
		OwnerServerID:   "server-old",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	result, err := usecase.RetryPipelineTask(context.Background(), RetryPipelineTaskRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		TaskID:      task.ID,
	})
	if err != nil {
		t.Fatalf("RetryPipelineTask returned error: %v", err)
	}
	if result.Status != models.TaskStatusQueued {
		t.Fatalf("result status=%s, want %s", result.Status, models.TaskStatusQueued)
	}
	var updated models.AgentTask
	if err := db.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("reload task failed: %v", err)
	}
	if updated.Status != models.TaskStatusQueued {
		t.Fatalf("status=%s, want %s", updated.Status, models.TaskStatusQueued)
	}
	if updated.RetryCount != 1 {
		t.Fatalf("retry_count=%d, want 1", updated.RetryCount)
	}
	if updated.StartTime != 0 || updated.EndTime != 0 || updated.Duration != 0 || updated.ExitCode != 0 {
		t.Fatalf("expected execution fields reset, got %+v", updated)
	}
	if updated.ErrorMsg != "" || updated.ResultData != "" || updated.DispatchToken != "" || updated.DispatchAttempt != 0 || updated.LeaseExpireAt != 0 || updated.AgentSessionID != "" || updated.OwnerServerID != "" {
		t.Fatalf("expected dispatch fields reset, got %+v", updated)
	}
	if len(notifier.taskRetries) != 1 || notifier.taskRetries[0].ID != task.ID {
		t.Fatalf("task retries=%+v", notifier.taskRetries)
	}
}

func TestPipelineOperationTriggerRejectsNonMember(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	owner, workspace := seedPipelineQueryWorkspaceMember(t, db, t.Name()+"-owner", models.WorkspaceRoleDeveloper)
	viewer := models.User{Username: t.Name() + "-outsider", Role: "user", Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatalf("create outsider failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "outsider-pipeline", WorkspaceID: workspace.ID, OwnerID: owner.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	usecase := &PipelineOperationUseCase{DB: db, TriggerRunner: &testPipelineTriggerRunner{db: db}}

	_, err := usecase.TriggerPipeline(context.Background(), TriggerPipelineRequest{
		Actor:       ActorContext{UserID: viewer.ID, Username: viewer.Username, SystemRole: viewer.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
	})
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

func TestPipelineOperationAdminDoesNotBypassMissingWorkspace(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	usecase := &PipelineOperationUseCase{DB: db, TriggerRunner: &testPipelineTriggerRunner{db: db}}
	pipeline := models.Pipeline{Name: "orphan-pipeline", WorkspaceID: 9999, OwnerID: 1}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}

	_, err := usecase.TriggerPipeline(context.Background(), TriggerPipelineRequest{
		Actor:       ActorContext{UserID: 1, Username: "admin", SystemRole: "admin"},
		WorkspaceID: 9999,
		PipelineID:  pipeline.ID,
	})
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

func TestPipelineOperationTriggerRejectsInsufficientRole(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, t.Name()+"-viewer", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "viewer-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	usecase := &PipelineOperationUseCase{DB: db, TriggerRunner: &testPipelineTriggerRunner{db: db}}

	_, err := usecase.TriggerPipeline(context.Background(), TriggerPipelineRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
	})
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

func TestPipelineOperationCancelRejectsInsufficientRole(t *testing.T) {
	db := openPipelineQueryTestDB(t)
	user, workspace := seedPipelineQueryWorkspaceMember(t, db, t.Name()+"-viewer", models.WorkspaceRoleViewer)
	pipeline := models.Pipeline{Name: "viewer-cancel-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run failed: %v", err)
	}
	usecase := &PipelineOperationUseCase{DB: db}

	_, err := usecase.CancelPipelineRun(context.Background(), CancelPipelineRunRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		PipelineID:  pipeline.ID,
		RunID:       run.ID,
	})
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

func TestPipelineOperationRetryRejectsOtherWorkspaceTask(t *testing.T) {
	usecase, db, user, workspace, _, _ := newPipelineOperationUseCaseForTest(t)
	_, otherWorkspace := seedPipelineQueryWorkspaceMember(t, db, t.Name()+"-other", models.WorkspaceRoleDeveloper)
	task := models.AgentTask{WorkspaceID: otherWorkspace.ID, PipelineRunID: 1, NodeID: "node-1", AgentID: 1, TaskType: "shell", Name: "retry", Status: models.TaskStatusExecuteFailed, MaxRetries: 2}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	_, err := usecase.RetryPipelineTask(context.Background(), RetryPipelineTaskRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		TaskID:      task.ID,
	})
	if err == nil {
		t.Fatal("expected not found error")
	}
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != ErrorCodeNotFound {
		t.Fatalf("error code=%q, want %q", svcErr.Code, ErrorCodeNotFound)
	}
}
