package services

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/safety"

	"gorm.io/gorm"
)

const (
	defaultPipelinePageLimit = 20
	maxPipelinePageLimit     = 100
	deploymentRequestTrigger = "deployment_request"
)

type PipelineQueryUseCase struct {
	DB *gorm.DB
}

type ListPipelinesRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	Page        int
	Limit       int
	Query       string
}

type GetPipelineRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
}

type ListPipelineRunsRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	Page        int
	Limit       int
}

type GetPipelineRunRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	RunID       uint64
}

type GetPipelineTaskRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	RunID       uint64
	TaskID      uint64
}

type PipelineSummary struct {
	ID               uint64    `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	WorkspaceID      uint64    `json:"workspace_id"`
	ProjectID        uint64    `json:"project_id,omitempty"`
	OwnerID          uint64    `json:"owner_id"`
	Environment      string    `json:"environment,omitempty"`
	IsPublic         bool      `json:"is_public"`
	IsFavorite       bool      `json:"is_favorite"`
	ManagementHidden bool      `json:"management_hidden"`
	Version          int       `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PipelineDetail struct {
	PipelineSummary
}

type PipelineListResult struct {
	List  []PipelineSummary `json:"list"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

type PipelineRunSummary struct {
	ID              uint64    `json:"id"`
	WorkspaceID     uint64    `json:"workspace_id"`
	PipelineID      uint64    `json:"pipeline_id"`
	BuildNumber     int       `json:"build_number"`
	Status          string    `json:"status"`
	TriggerType     string    `json:"trigger_type,omitempty"`
	TriggerUser     string    `json:"trigger_user,omitempty"`
	TriggerUserID   uint64    `json:"trigger_user_id,omitempty"`
	TriggerUserRole string    `json:"trigger_user_role,omitempty"`
	TriggerSource   string    `json:"trigger_source,omitempty"`
	StartTime       int64     `json:"start_time,omitempty"`
	EndTime         int64     `json:"end_time,omitempty"`
	Duration        int       `json:"duration,omitempty"`
	ErrorMsg        string    `json:"error_msg,omitempty"`
	AgentID         uint64    `json:"agent_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PipelineRunListResult struct {
	List  []PipelineRunSummary `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Limit int                  `json:"limit"`
}

type PipelineRunNodeSummary struct {
	NodeID         string `json:"node_id,omitempty"`
	TaskKey        string `json:"task_key,omitempty"`
	TaskType       string `json:"task_type,omitempty"`
	Name           string `json:"name,omitempty"`
	Status         string `json:"status,omitempty"`
	StartTime      int64  `json:"start_time,omitempty"`
	EndTime        int64  `json:"end_time,omitempty"`
	Duration       int    `json:"duration,omitempty"`
	IgnoreFailure  bool   `json:"ignore_failure,omitempty"`
	Skipped        bool   `json:"skipped,omitempty"`
	ExecutionOrder int    `json:"execution_order,omitempty"`
}

type PipelineTaskSummary struct {
	ID            uint64    `json:"id"`
	WorkspaceID   uint64    `json:"workspace_id"`
	PipelineRunID uint64    `json:"pipeline_run_id"`
	NodeID        string    `json:"node_id,omitempty"`
	TaskType      string    `json:"task_type,omitempty"`
	Name          string    `json:"name,omitempty"`
	Status        string    `json:"status,omitempty"`
	AgentID       uint64    `json:"agent_id,omitempty"`
	Priority      int       `json:"priority,omitempty"`
	Timeout       int       `json:"timeout,omitempty"`
	RetryCount    int       `json:"retry_count,omitempty"`
	MaxRetries    int       `json:"max_retries,omitempty"`
	ExitCode      int       `json:"exit_code,omitempty"`
	ErrorMsg      string    `json:"error_msg,omitempty"`
	StartTime     int64     `json:"start_time,omitempty"`
	EndTime       int64     `json:"end_time,omitempty"`
	Duration      int       `json:"duration,omitempty"`
	Result        any       `json:"result,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type PipelineRunDetail struct {
	PipelineRunSummary
	Nodes []PipelineRunNodeSummary `json:"nodes,omitempty"`
	Tasks []PipelineTaskSummary    `json:"tasks,omitempty"`
}

func (u *PipelineQueryUseCase) ListPipelines(ctx context.Context, req ListPipelinesRequest) (PipelineListResult, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineListResult{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineListResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineListResult{}, err
	}
	page, limit := normalizePipelinePagination(req.Page, req.Limit)
	query := strings.TrimSpace(req.Query)

	dbQuery := u.DB.WithContext(ctx).Model(&models.Pipeline{}).Where("workspace_id = ?", req.WorkspaceID)
	if query != "" {
		like := "%" + query + "%"
		dbQuery = dbQuery.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return PipelineListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count pipelines"}
	}
	var pipelines []models.Pipeline
	if err := dbQuery.Order("updated_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&pipelines).Error; err != nil {
		return PipelineListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list pipelines"}
	}
	result := PipelineListResult{List: make([]PipelineSummary, 0, len(pipelines)), Total: total, Page: page, Limit: limit}
	for _, pipeline := range pipelines {
		result.List = append(result.List, buildPipelineSummary(pipeline))
	}
	return result, nil
}

func (u *PipelineQueryUseCase) GetPipeline(ctx context.Context, req GetPipelineRequest) (PipelineDetail, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineDetail{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineDetail{}, err
	}
	pipeline, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID)
	if err != nil {
		return PipelineDetail{}, err
	}
	return PipelineDetail{PipelineSummary: buildPipelineSummary(*pipeline)}, nil
}

func (u *PipelineQueryUseCase) ListPipelineRuns(ctx context.Context, req ListPipelineRunsRequest) (PipelineRunListResult, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineRunListResult{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineRunListResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineRunListResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineRunListResult{}, err
	}
	if _, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID); err != nil {
		return PipelineRunListResult{}, err
	}
	page, limit := normalizePipelinePagination(req.Page, req.Limit)

	dbQuery := regularPipelineRunsQuery(u.DB.WithContext(ctx).Model(&models.PipelineRun{})).Where("workspace_id = ? AND pipeline_id = ?", req.WorkspaceID, req.PipelineID)
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return PipelineRunListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count pipeline runs"}
	}
	var runs []models.PipelineRun
	if err := dbQuery.Order("build_number DESC, id DESC").Offset((page - 1) * limit).Limit(limit).Find(&runs).Error; err != nil {
		return PipelineRunListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list pipeline runs"}
	}
	result := PipelineRunListResult{List: make([]PipelineRunSummary, 0, len(runs)), Total: total, Page: page, Limit: limit}
	for _, run := range runs {
		result.List = append(result.List, buildPipelineRunSummary(run))
	}
	return result, nil
}

func (u *PipelineQueryUseCase) GetPipelineRun(ctx context.Context, req GetPipelineRunRequest) (PipelineRunDetail, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineRunDetail{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineRunDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineRunDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if req.RunID == 0 {
		return PipelineRunDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "run_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineRunDetail{}, err
	}
	if _, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID); err != nil {
		return PipelineRunDetail{}, err
	}

	run, err := u.loadPipelineRun(ctx, req.WorkspaceID, req.PipelineID, req.RunID)
	if err != nil {
		return PipelineRunDetail{}, err
	}
	var tasks []models.AgentTask
	if err := u.DB.WithContext(ctx).Where("workspace_id = ? AND pipeline_run_id = ?", req.WorkspaceID, run.ID).Order("created_at ASC, id ASC").Find(&tasks).Error; err != nil {
		return PipelineRunDetail{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline tasks"}
	}
	result := PipelineRunDetail{
		PipelineRunSummary: buildPipelineRunSummary(*run),
		Nodes:              summarizeRunNodes(run.ResolvedNodes),
		Tasks:              make([]PipelineTaskSummary, 0, len(tasks)),
	}
	for _, task := range tasks {
		result.Tasks = append(result.Tasks, buildPipelineTaskSummary(task, false))
	}
	return result, nil
}

func (u *PipelineQueryUseCase) GetPipelineTask(ctx context.Context, req GetPipelineTaskRequest) (PipelineTaskSummary, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineTaskSummary{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if req.RunID == 0 {
		return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "run_id is required"}
	}
	if req.TaskID == 0 {
		return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "task_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineTaskSummary{}, err
	}
	if _, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID); err != nil {
		return PipelineTaskSummary{}, err
	}
	if err := u.ensurePipelineRunExists(ctx, req.WorkspaceID, req.PipelineID, req.RunID); err != nil {
		return PipelineTaskSummary{}, err
	}

	var task models.AgentTask
	if err := u.DB.WithContext(ctx).Where("id = ? AND workspace_id = ? AND pipeline_run_id = ?", req.TaskID, req.WorkspaceID, req.RunID).First(&task).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline task not found"}
		}
		return PipelineTaskSummary{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline task"}
	}
	return buildPipelineTaskSummary(task, true), nil
}

func validatePipelineQueryActor(actor ActorContext) error {
	if actor.UserID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	return nil
}

func (u *PipelineQueryUseCase) resolveWorkspace(ctx context.Context, actor ActorContext, workspaceID uint64) (WorkspaceContext, error) {
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

func (u *PipelineQueryUseCase) loadPipeline(ctx context.Context, workspaceID uint64, pipelineID uint64) (*models.Pipeline, error) {
	var pipeline models.Pipeline
	if err := u.DB.WithContext(ctx).Where("id = ? AND workspace_id = ?", pipelineID, workspaceID).First(&pipeline).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline not found"}
		}
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline"}
	}
	return &pipeline, nil
}

func (u *PipelineQueryUseCase) loadPipelineRun(ctx context.Context, workspaceID uint64, pipelineID uint64, runID uint64) (*models.PipelineRun, error) {
	var run models.PipelineRun
	if err := regularPipelineRunsQuery(u.DB.WithContext(ctx)).Where("id = ? AND workspace_id = ? AND pipeline_id = ?", runID, workspaceID, pipelineID).First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ServiceError{Code: ErrorCodeNotFound, Message: "pipeline run not found"}
		}
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline run"}
	}
	return &run, nil
}

func (u *PipelineQueryUseCase) ensurePipelineRunExists(ctx context.Context, workspaceID uint64, pipelineID uint64, runID uint64) error {
	var run models.PipelineRun
	if err := regularPipelineRunsQuery(u.DB.WithContext(ctx)).Select("id").Where("id = ? AND workspace_id = ? AND pipeline_id = ?", runID, workspaceID, pipelineID).First(&run).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ServiceError{Code: ErrorCodeNotFound, Message: "pipeline run not found"}
		}
		return ServiceError{Code: ErrorCodeInternalError, Message: "failed to load pipeline run"}
	}
	return nil
}

func buildPipelineSummary(pipeline models.Pipeline) PipelineSummary {
	return PipelineSummary{
		ID:               pipeline.ID,
		Name:             sanitizeText(pipeline.Name),
		Description:      sanitizeText(pipeline.Description),
		WorkspaceID:      pipeline.WorkspaceID,
		ProjectID:        pipeline.ProjectID,
		OwnerID:          pipeline.OwnerID,
		Environment:      sanitizeText(pipeline.Environment),
		IsPublic:         pipeline.IsPublic,
		IsFavorite:       pipeline.IsFavorite,
		ManagementHidden: pipeline.ManagementHidden,
		Version:          pipeline.Version,
		CreatedAt:        pipeline.CreatedAt,
		UpdatedAt:        pipeline.UpdatedAt,
	}
}

func buildPipelineRunSummary(run models.PipelineRun) PipelineRunSummary {
	return PipelineRunSummary{
		ID:              run.ID,
		WorkspaceID:     run.WorkspaceID,
		PipelineID:      run.PipelineID,
		BuildNumber:     run.BuildNumber,
		Status:          sanitizeText(run.Status),
		TriggerType:     sanitizeText(run.TriggerType),
		TriggerUser:     sanitizeText(run.TriggerUser),
		TriggerUserID:   run.TriggerUserID,
		TriggerUserRole: sanitizeText(run.TriggerUserRole),
		TriggerSource:   sanitizeText(run.TriggerSource),
		StartTime:       run.StartTime,
		EndTime:         run.EndTime,
		Duration:        run.Duration,
		ErrorMsg:        sanitizeText(run.ErrorMsg),
		AgentID:         run.AgentID,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
}

func buildPipelineTaskSummary(task models.AgentTask, includeResult bool) PipelineTaskSummary {
	summary := PipelineTaskSummary{
		ID:            task.ID,
		WorkspaceID:   task.WorkspaceID,
		PipelineRunID: task.PipelineRunID,
		NodeID:        sanitizeText(task.NodeID),
		TaskType:      sanitizeText(task.TaskType),
		Name:          sanitizeText(task.Name),
		Status:        sanitizeText(task.Status),
		AgentID:       task.AgentID,
		Priority:      task.Priority,
		Timeout:       task.Timeout,
		RetryCount:    task.RetryCount,
		MaxRetries:    task.MaxRetries,
		ExitCode:      task.ExitCode,
		ErrorMsg:      sanitizeText(task.ErrorMsg),
		StartTime:     task.StartTime,
		EndTime:       task.EndTime,
		Duration:      task.Duration,
		CreatedAt:     task.CreatedAt,
		UpdatedAt:     task.UpdatedAt,
	}
	if includeResult {
		summary.Result = sanitizeJSONText(task.ResultData)
	}
	return summary
}

func summarizeRunNodes(raw string) []PipelineRunNodeSummary {
	items := decodeJSONArray(raw)
	if len(items) == 0 {
		return nil
	}
	result := make([]PipelineRunNodeSummary, 0, len(items))
	for _, item := range items {
		result = append(result, PipelineRunNodeSummary{
			NodeID:         stringValue(item, "node_id", "id"),
			TaskKey:        stringValue(item, "task_key"),
			TaskType:       stringValue(item, "task_type", "type"),
			Name:           stringValue(item, "name", "node_name"),
			Status:         stringValue(item, "status"),
			StartTime:      int64Value(item, "start_time"),
			EndTime:        int64Value(item, "end_time"),
			Duration:       intValue(item, "duration"),
			IgnoreFailure:  boolValue(item, "ignore_failure"),
			Skipped:        boolValue(item, "skipped"),
			ExecutionOrder: intValue(item, "execution_order"),
		})
	}
	return result
}

func decodeJSONArray(raw string) []map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		return nil
	}
	return items
}

func sanitizeText(value string) string {
	sanitized, _ := sanitizeAndDropSensitiveKeys(value).(string)
	return sanitized
}

func sanitizeJSONText(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return sanitizeAndDropSensitiveKeys(trimmed)
	}
	return sanitizeAndDropSensitiveKeys(decoded)
}

func sanitizeAndDropSensitiveKeys(value any) any {
	return dropSensitiveKeys(safety.SanitizeJSONValue(value, safety.DefaultOptions()))
}

func dropSensitiveKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, item := range typed {
			if containsSensitiveKeyPart(key) {
				continue
			}
			result[key] = dropSensitiveKeys(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, dropSensitiveKeys(item))
		}
		return result
	case string:
		return redactSensitiveText(typed)
	default:
		return value
	}
}

func redactSensitiveText(value string) string {
	lower := strings.ToLower(value)
	for _, pattern := range []string{"bearer ", "authorization:", "authorization=", "token=", "token:", "secret=", "secret:", "password=", "password:", "api_key=", "api-key=", "credential=", "credential:"} {
		if strings.Contains(lower, pattern) {
			return "[REDACTED]"
		}
	}
	return value
}

func containsSensitiveKeyPart(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, part := range []string{"config", "credential", "token", "secret", "password", "api_key", "authorization"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func normalizePipelinePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultPipelinePageLimit
	}
	if limit > maxPipelinePageLimit {
		limit = maxPipelinePageLimit
	}
	return page, limit
}

func regularPipelineRunsQuery(db *gorm.DB) *gorm.DB {
	return db.Where("(trigger_type IS NULL OR trigger_type = '' OR trigger_type <> ?)", deploymentRequestTrigger)
}

func stringValue(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			if text := strings.TrimSpace(toString(value)); text != "" {
				return sanitizeText(text)
			}
		}
	}
	return ""
}

func int64Value(item map[string]any, key string) int64 {
	value, ok := item[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}

func intValue(item map[string]any, key string) int {
	return int(int64Value(item, key))
}

func boolValue(item map[string]any, key string) bool {
	value, ok := item[key]
	if !ok {
		return false
	}
	typed, _ := value.(bool)
	return typed
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		bytes, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.Trim(string(bytes), `"`)
	}
}
