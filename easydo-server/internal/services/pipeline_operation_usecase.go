package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/workspaceauth"

	"gorm.io/gorm"
)

type PipelineOperationUseCase struct {
	DB            *gorm.DB
	TriggerRunner TriggerPipelineExecutionRunner
	Notifier      PipelineOperationNotifier
	Hooks         PipelineOperationHooks
	Now           func() time.Time
}

type TriggerPipelineRequest struct {
	Actor         ActorContext
	WorkspaceID   uint64
	PipelineID    uint64
	Inputs        map[string]map[string]any
	Options       map[string]any
	TriggerType   string
	TriggerSource string
}

type TriggerPipelineResult struct {
	RunID       uint64 `json:"run_id"`
	BuildNumber int    `json:"build_number"`
	Status      string `json:"status"`
}

type CancelPipelineRunRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	RunID       uint64
}

type CancelPipelineRunResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type RetryPipelineTaskRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	TaskID      uint64
}

type RetryPipelineTaskResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type TriggerPipelineExecutionRequest struct {
	Actor         ActorContext
	Workspace     WorkspaceContext
	Pipeline      models.Pipeline
	RunConfig     models.PipelineRunConfigSnapshot
	TriggerType   string
	TriggerSource string
}

type TriggerPipelineExecutionRunner interface {
	TriggerPipeline(ctx context.Context, req TriggerPipelineExecutionRequest) (TriggerPipelineResult, error)
}

type PipelineOperationNotifier interface {
	NotifyTaskRetry(task models.AgentTask)
	NotifyTaskCancel(task models.AgentTask)
	NotifyTaskStatus(runID uint64, task models.AgentTask, message string)
	NotifyRunStatus(run models.PipelineRun, message string)
}

type PipelineOperationHooks interface {
	OnRunCancelRequested(ctx context.Context, run models.PipelineRun) error
	OnRunCancelled(ctx context.Context, run models.PipelineRun) error
}

func (u *PipelineOperationUseCase) TriggerPipeline(ctx context.Context, req TriggerPipelineRequest) (TriggerPipelineResult, error) {
	if err := validatePipelineOperationActor(req.Actor); err != nil {
		return TriggerPipelineResult{}, err
	}
	if u == nil || u.DB == nil {
		return TriggerPipelineResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return TriggerPipelineResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	resolved, err := u.resolvePipelineOperationWorkspace(ctx, req.Actor, req.WorkspaceID)
	if err != nil {
		return TriggerPipelineResult{}, err
	}
	if err := requirePipelineRunCapability(resolved); err != nil {
		return TriggerPipelineResult{}, err
	}
	pipeline, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID)
	if err != nil {
		return TriggerPipelineResult{}, err
	}
	if u.TriggerRunner == nil {
		return TriggerPipelineResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "trigger runner is required"}
	}
	return u.TriggerRunner.TriggerPipeline(ctx, TriggerPipelineExecutionRequest{
		Actor:         req.Actor,
		Workspace:     resolved,
		Pipeline:      *pipeline,
		RunConfig:     models.PipelineRunConfigSnapshot{Inputs: req.Inputs, Options: req.Options},
		TriggerType:   req.TriggerType,
		TriggerSource: req.TriggerSource,
	})
}

func (u *PipelineOperationUseCase) CancelPipelineRun(ctx context.Context, req CancelPipelineRunRequest) (CancelPipelineRunResult, error) {
	if err := validatePipelineOperationActor(req.Actor); err != nil {
		return CancelPipelineRunResult{}, err
	}
	if u == nil || u.DB == nil {
		return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if req.RunID == 0 {
		return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "run_id is required"}
	}
	resolved, err := u.resolvePipelineOperationWorkspace(ctx, req.Actor, req.WorkspaceID)
	if err != nil {
		return CancelPipelineRunResult{}, err
	}
	if err := requirePipelineRunCapability(resolved); err != nil {
		return CancelPipelineRunResult{}, err
	}
	if _, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID); err != nil {
		return CancelPipelineRunResult{}, err
	}

	var run models.PipelineRun
	if err := u.DB.WithContext(ctx).Where("id = ? AND pipeline_id = ? AND workspace_id = ?", req.RunID, req.PipelineID, req.WorkspaceID).First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline run not found"}
		}
		return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline run"}
	}
	if !IsRunActiveStatus(run.Status) || run.Status == models.PipelineRunStatusCancelRequested {
		return CancelPipelineRunResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: fmt.Sprintf("运行状态 '%s' 不支持取消操作", run.Status)}
	}

	var cancelledTasks []models.AgentTask
	var tasksToNotify []models.AgentTask
	now := u.now().Unix()
	err = u.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tasks []models.AgentTask
		if err := tx.Where("pipeline_run_id = ? AND status NOT IN ?",
			req.RunID,
			[]string{models.TaskStatusExecuteSuccess, models.TaskStatusExecuteFailed, models.TaskStatusScheduleFailed, models.TaskStatusCancelled},
		).Find(&tasks).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline tasks"}
		}

		for i := range tasks {
			task := &tasks[i]
			if !IsTaskCancelable(task.Status) {
				continue
			}
			shouldNotifyAgent := IsExecutionOwnedTaskStatus(task.Status)
			updates := BuildTaskCancelUpdates(task, now)
			if err := tx.Model(task).Updates(updates).Error; err != nil {
				return ServiceError{Code: ErrorCodeInternalError, Message: fmt.Sprintf("failed to update pipeline task %d", task.ID)}
			}
			applyTaskUpdateSnapshot(task, updates)
			cancelledTasks = append(cancelledTasks, *task)
			if shouldNotifyAgent {
				tasksToNotify = append(tasksToNotify, *task)
			}
		}

		runUpdates := map[string]any{}
		runStatus := models.PipelineRunStatusCancelled
		if len(tasksToNotify) > 0 {
			runStatus = models.PipelineRunStatusCancelRequested
			runUpdates["status"] = runStatus
		} else {
			duration := 0
			if run.StartTime > 0 {
				duration = int(now - run.StartTime)
			}
			runUpdates["status"] = runStatus
			runUpdates["end_time"] = now
			runUpdates["duration"] = duration
			run.EndTime = now
			run.Duration = duration
		}
		if err := tx.Model(&run).Updates(runUpdates).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to update pipeline run"}
		}
		run.Status = runStatus
		return nil
	})
	if err != nil {
		return CancelPipelineRunResult{}, err
	}

	for _, task := range tasksToNotify {
		u.notifier().NotifyTaskCancel(task)
	}
	for _, task := range cancelledTasks {
		message := "任务已被取消"
		if task.Status == models.TaskStatusCancelRequested {
			message = "任务取消请求已提交，等待 agent 确认"
		}
		u.notifier().NotifyTaskStatus(req.RunID, task, message)
	}

	runMessage := "流水线运行已取消"
	if run.Status == models.PipelineRunStatusCancelRequested {
		runMessage = "流水线取消请求已提交"
		if err := u.hooks().OnRunCancelRequested(ctx, run); err != nil {
			return CancelPipelineRunResult{}, err
		}
	} else {
		if err := u.hooks().OnRunCancelled(ctx, run); err != nil {
			return CancelPipelineRunResult{}, err
		}
	}
	u.notifier().NotifyRunStatus(run, runMessage)
	return CancelPipelineRunResult{Status: run.Status, Message: runMessage}, nil
}

func (u *PipelineOperationUseCase) RetryPipelineTask(ctx context.Context, req RetryPipelineTaskRequest) (RetryPipelineTaskResult, error) {
	if err := validatePipelineOperationActor(req.Actor); err != nil {
		return RetryPipelineTaskResult{}, err
	}
	if u == nil || u.DB == nil {
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.TaskID == 0 {
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "task_id is required"}
	}
	resolved, err := u.resolvePipelineOperationWorkspace(ctx, req.Actor, req.WorkspaceID)
	if err != nil {
		return RetryPipelineTaskResult{}, err
	}
	if err := requirePipelineRunCapability(resolved); err != nil {
		return RetryPipelineTaskResult{}, err
	}
	var task models.AgentTask
	if err := u.DB.WithContext(ctx).Where("id = ? AND workspace_id = ?", req.TaskID, req.WorkspaceID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline task not found"}
		}
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline task"}
	}
	if !isRetryablePipelineTaskStatus(task.Status) {
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "只能重试失败的任务"}
	}
	if task.RetryCount >= task.MaxRetries {
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "已达到最大重试次数"}
	}
	updates := map[string]interface{}{
		"status":           models.TaskStatusQueued,
		"retry_count":      task.RetryCount + 1,
		"start_time":       0,
		"end_time":         0,
		"duration":         0,
		"exit_code":        0,
		"error_msg":        "",
		"result_data":      "",
		"dispatch_token":   "",
		"dispatch_attempt": 0,
		"lease_expire_at":  0,
		"agent_session_id": "",
		"owner_server_id":  "",
	}
	if err := u.DB.WithContext(ctx).Model(&task).Updates(updates).Error; err != nil {
		return RetryPipelineTaskResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to retry pipeline task"}
	}
	applyTaskRetrySnapshot(&task)
	u.notifier().NotifyTaskRetry(task)
	return RetryPipelineTaskResult{Status: task.Status, Message: "任务已重新排队"}, nil
}

func validatePipelineOperationActor(actor ActorContext) error {
	if actor.UserID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	return nil
}

func (u *PipelineOperationUseCase) resolvePipelineOperationWorkspace(ctx context.Context, actor ActorContext, workspaceID uint64) (WorkspaceContext, error) {
	if workspaceID == 0 {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace_id is required"}
	}
	resolved, err := ResolveWorkspaceForActor(ctx, u.DB, actor, workspaceID)
	if err != nil {
		return WorkspaceContext{}, err
	}
	if resolved.Workspace == nil {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
	}
	return resolved, nil
}

func (u *PipelineOperationUseCase) loadPipeline(ctx context.Context, workspaceID uint64, pipelineID uint64) (*models.Pipeline, error) {
	var pipeline models.Pipeline
	if err := u.DB.WithContext(ctx).Where("id = ? AND workspace_id = ?", pipelineID, workspaceID).First(&pipeline).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline not found"}
		}
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline"}
	}
	return &pipeline, nil
}

func requirePipelineRunCapability(workspace WorkspaceContext) error {
	if workspace.Workspace == nil {
		return ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
	}
	if !workspaceauth.WorkspaceRoleAtLeast(workspace.Role, models.WorkspaceRoleDeveloper) || !hasWorkspaceCapability(workspace.Capabilities, "pipeline.run") {
		return ServiceError{Code: ErrorCodeForbidden, Message: "workspace role is insufficient for pipeline operation"}
	}
	return nil
}

func hasWorkspaceCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == expected {
			return true
		}
	}
	return false
}

func isRetryablePipelineTaskStatus(status string) bool {
	switch status {
	case models.TaskStatusExecuteFailed, models.TaskStatusScheduleFailed, models.TaskStatusDispatchTimeout, models.TaskStatusLeaseExpired:
		return true
	default:
		return false
	}
}

func applyTaskUpdateSnapshot(task *models.AgentTask, updates map[string]any) {
	if task == nil {
		return
	}
	if status, ok := updates["status"].(string); ok {
		task.Status = status
	}
	if endTime, ok := updates["end_time"].(int64); ok {
		task.EndTime = endTime
	}
	if duration, ok := updates["duration"].(int); ok {
		task.Duration = duration
	}
}

func applyTaskRetrySnapshot(task *models.AgentTask) {
	task.Status = models.TaskStatusQueued
	task.RetryCount++
	task.StartTime = 0
	task.EndTime = 0
	task.Duration = 0
	task.ExitCode = 0
	task.ErrorMsg = ""
	task.ResultData = ""
	task.DispatchToken = ""
	task.DispatchAttempt = 0
	task.LeaseExpireAt = 0
	task.AgentSessionID = ""
	task.OwnerServerID = ""
}

func (u *PipelineOperationUseCase) now() time.Time {
	if u != nil && u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *PipelineOperationUseCase) notifier() PipelineOperationNotifier {
	if u != nil && u.Notifier != nil {
		return u.Notifier
	}
	return pipelineOperationNoopNotifier{}
}

func (u *PipelineOperationUseCase) hooks() PipelineOperationHooks {
	if u != nil && u.Hooks != nil {
		return u.Hooks
	}
	return pipelineOperationNoopHooks{}
}

type pipelineOperationNoopNotifier struct{}

func (pipelineOperationNoopNotifier) NotifyTaskRetry(task models.AgentTask)  {}
func (pipelineOperationNoopNotifier) NotifyTaskCancel(task models.AgentTask) {}
func (pipelineOperationNoopNotifier) NotifyTaskStatus(runID uint64, task models.AgentTask, message string) {
}
func (pipelineOperationNoopNotifier) NotifyRunStatus(run models.PipelineRun, message string) {}

type pipelineOperationNoopHooks struct{}

func (pipelineOperationNoopHooks) OnRunCancelRequested(ctx context.Context, run models.PipelineRun) error {
	return nil
}

func (pipelineOperationNoopHooks) OnRunCancelled(ctx context.Context, run models.PipelineRun) error {
	return nil
}
