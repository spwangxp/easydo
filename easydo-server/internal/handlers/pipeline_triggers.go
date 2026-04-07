package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	pipelineRunTriggerTypeWebhook  = "webhook"
	pipelineRunTriggerTypeSchedule = "schedule"
)

var errScheduledTriggerNotDue = errors.New("scheduled trigger is not due")

type pipelineTriggerPayload struct {
	Provider                        string `json:"provider"`
	WebhookEnabled                  bool   `json:"webhook_enabled"`
	PushEnabled                     bool   `json:"push_enabled"`
	TagEnabled                      bool   `json:"tag_enabled"`
	MergeRequestEnabled             bool   `json:"merge_request_enabled"`
	ScheduleEnabled                 bool   `json:"schedule_enabled"`
	CronExpression                  string `json:"cron_expression"`
	Timezone                        string `json:"timezone"`
	PushBranchFilters               string `json:"push_branch_filters"`
	TagFilters                      string `json:"tag_filters"`
	MergeRequestSourceBranchFilters string `json:"merge_request_source_branch_filters"`
	MergeRequestTargetBranchFilters string `json:"merge_request_target_branch_filters"`
	RotateSecret                    bool   `json:"rotate_secret"`
}

type pipelineTriggerResponse struct {
	Provider                        string     `json:"provider"`
	Manual                          bool       `json:"manual"`
	WebhookEnabled                  bool       `json:"webhook_enabled"`
	PushEnabled                     bool       `json:"push_enabled"`
	TagEnabled                      bool       `json:"tag_enabled"`
	MergeRequestEnabled             bool       `json:"merge_request_enabled"`
	ScheduleEnabled                 bool       `json:"schedule_enabled"`
	CronExpression                  string     `json:"cron_expression"`
	Timezone                        string     `json:"timezone"`
	PushBranchFilters               string     `json:"push_branch_filters"`
	TagFilters                      string     `json:"tag_filters"`
	MergeRequestSourceBranchFilters string     `json:"merge_request_source_branch_filters"`
	MergeRequestTargetBranchFilters string     `json:"merge_request_target_branch_filters"`
	SecretToken                     string     `json:"secret_token"`
	WebhookToken                    string     `json:"webhook_token"`
	WebhookURL                      string     `json:"webhook_url"`
	NextRunAt                       *time.Time `json:"next_run_at,omitempty"`
	LastRunAt                       *time.Time `json:"last_run_at,omitempty"`
	LastTriggeredAt                 *time.Time `json:"last_triggered_at,omitempty"`
}

type pipelineRunTriggerContext struct {
	TriggerType     string
	TriggerUser     string
	TriggerUserID   uint64
	TriggerUserRole string
	TriggerSource   string
	IdempotencyKey  *string
	RunConfig       models.PipelineRunConfigSnapshot
	AuthoredConfig  *PipelineConfig
	ExecutionConfig *PipelineConfig
}

type gitlabWebhookPayload struct {
	ObjectKind   string `json:"object_kind"`
	Ref          string `json:"ref"`
	CheckoutSHA  string `json:"checkout_sha"`
	UserUsername string `json:"user_username"`
	Project      struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		Action       string `json:"action"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func (h *PipelineHandler) GetPipelineTriggers(c *gin.Context) {
	pipeline, ok := h.loadWorkspacePipeline(c)
	if !ok {
		return
	}
	trigger, err := h.ensurePipelineTrigger(pipeline.ID, pipeline.WorkspaceID, c.GetUint64("user_id"), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取触发设置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": h.buildPipelineTriggerResponse(c, trigger)})
}

func (h *PipelineHandler) UpdatePipelineTriggers(c *gin.Context) {
	pipeline, ok := h.loadWorkspacePipeline(c)
	if !ok {
		return
	}
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanWriteWorkspaceResource(h.DB, pipeline.WorkspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权修改该流水线触发设置"})
		return
	}

	var req pipelineTriggerPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "gitlab"
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	cronExpr := strings.TrimSpace(req.CronExpression)
	var nextRunAt *time.Time
	if req.ScheduleEnabled {
		if cronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "定时触发已开启时 Cron 表达式不能为空"})
			return
		}
		next, err := services.ComputeNextScheduleTime(cronExpr, timezone, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Cron 表达式无效: " + err.Error()})
			return
		}
		nextRunAt = &next
	}

	trigger, err := h.ensurePipelineTrigger(pipeline.ID, pipeline.WorkspaceID, userID, req.RotateSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "初始化触发设置失败: " + err.Error()})
		return
	}
	trigger.Provider = provider
	trigger.WebhookEnabled = req.WebhookEnabled
	trigger.PushEnabled = req.PushEnabled
	trigger.TagEnabled = req.TagEnabled
	trigger.MergeRequestEnabled = req.MergeRequestEnabled
	trigger.ScheduleEnabled = req.ScheduleEnabled
	trigger.CronExpression = cronExpr
	trigger.Timezone = timezone
	trigger.PushBranchFilters = strings.TrimSpace(req.PushBranchFilters)
	trigger.TagFilters = strings.TrimSpace(req.TagFilters)
	trigger.MergeRequestSourceBranchFilters = strings.TrimSpace(req.MergeRequestSourceBranchFilters)
	trigger.MergeRequestTargetBranchFilters = strings.TrimSpace(req.MergeRequestTargetBranchFilters)
	trigger.UpdatedBy = userID
	trigger.NextRunAt = nextRunAt
	if !req.ScheduleEnabled {
		trigger.LastRunAt = nil
		trigger.LastTriggeredAt = nil
	}
	trigger.LastEventTypes = strings.Join(enabledGitLabTriggerEvents(*trigger), ",")

	if err := h.DB.Save(trigger).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存触发设置失败: " + err.Error()})
		return
	}
	if config, err := h.loadPipelineDefinitionConfig(h.DB, pipeline); err == nil {
		config.Triggers = buildPipelineTriggerDefinitions(trigger)
		if payload, marshalErr := json.Marshal(config); marshalErr == nil {
			_ = h.DB.Model(&models.Pipeline{}).Where("id = ?", pipeline.ID).Updates(map[string]interface{}{
				"definition_json": string(payload),
			}).Error
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": h.buildPipelineTriggerResponse(c, trigger)})
}

func (h *PipelineHandler) HandleGitLabWebhook(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "触发令牌不能为空"})
		return
	}

	var trigger models.PipelineTrigger
	if err := h.DB.Where("webhook_token = ? AND provider = ?", token, "gitlab").First(&trigger).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "触发器不存在"})
		return
	}
	if !trigger.WebhookEnabled {
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Webhook 触发未启用", "data": gin.H{"ignored": true}})
		return
	}
	if strings.TrimSpace(c.GetHeader("X-Gitlab-Token")) != trigger.SecretToken {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "Webhook 密钥无效"})
		return
	}

	var payload gitlabWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "Webhook Payload 无效: " + err.Error()})
		return
	}

	eventType, refName, commitSHA, matched := resolveGitLabTriggerMatch(trigger, payload)
	if !matched {
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "事件未命中触发规则", "data": gin.H{"ignored": true}})
		return
	}

	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", trigger.PipelineID, trigger.WorkspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "流水线不存在"})
		return
	}

	triggerUserID, triggerRole, fallbackUser, err := h.resolvePipelineAutomationIdentity(h.DB, pipeline)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "自动触发执行身份无效: " + err.Error()})
		return
	}
	triggerUser := strings.TrimSpace(payload.UserUsername)
	if triggerUser == "" {
		triggerUser = fallbackUser
	}

	config, err := h.loadPipelineDefinitionConfig(h.DB, pipeline)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "流水线配置解析失败: " + err.Error()})
		return
	}
	if valid, errMsg := config.ValidateTaskTypes(); !valid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": errMsg})
		return
	}
	if _, err := h.validatePipelineCredentialBindings(&config, triggerUserID, triggerRole, pipeline.ID, pipeline.WorkspaceID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "流水线凭据配置无效: " + err.Error()})
		return
	}
	runConfigSnapshot := models.PipelineRunConfigSnapshot{
		Inputs: buildGitReferenceRuntimeInputs(config, refName, commitSHA),
	}

	idempotencyKeyValue := buildWebhookIdempotencyKey(trigger.ID, eventType, refName, commitSHA)
	run, buildNumber, err := h.launchPipelineRun(pipeline, config, pipelineRunTriggerContext{
		TriggerType:     pipelineRunTriggerTypeWebhook,
		TriggerUser:     triggerUser,
		TriggerUserID:   triggerUserID,
		TriggerUserRole: triggerRole,
		TriggerSource:   fmt.Sprintf("gitlab:%s:%s", eventType, payload.Project.PathWithNamespace),
		IdempotencyKey:  &idempotencyKeyValue,
		RunConfig:       runConfigSnapshot,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "Webhook 触发流水线失败: " + err.Error()})
		return
	}
	now := time.Now().UTC()
	trigger.LastTriggeredAt = &now
	trigger.LastEventTypes = strings.Join(enabledGitLabTriggerEvents(trigger), ",")
	_ = h.DB.Model(&trigger).Select("last_triggered_at", "last_event_types").Updates(trigger).Error

	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"run_id": run.ID, "build_number": buildNumber, "status": run.Status}})
}

func (h *PipelineHandler) EvaluateScheduledPipelineTriggers(now time.Time) int {
	if h == nil || h.DB == nil {
		return 0
	}
	current := now.UTC()
	var dueTriggers []models.PipelineTrigger
	if err := h.DB.Where("schedule_enabled = ? AND cron_expression <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, current).Order("next_run_at ASC, id ASC").Find(&dueTriggers).Error; err != nil {
		return 0
	}
	launched := 0
	for i := range dueTriggers {
		ok, err := h.evaluateOneScheduledPipelineTrigger(dueTriggers[i].ID, current)
		if err == nil && ok {
			launched++
		}
	}
	return launched
}

func (h *PipelineHandler) evaluateOneScheduledPipelineTrigger(triggerID uint64, now time.Time) (bool, error) {
	var pipeline models.Pipeline
	var config PipelineConfig
	var run *models.PipelineRun
	var triggerUserID uint64
	var triggerRole string
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var trigger models.PipelineTrigger
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&trigger, triggerID).Error; err != nil {
			return err
		}
		if !trigger.ScheduleEnabled || strings.TrimSpace(trigger.CronExpression) == "" || trigger.NextRunAt == nil || trigger.NextRunAt.After(now) {
			return errScheduledTriggerNotDue
		}
		if err := tx.Where("id = ? AND workspace_id = ?", trigger.PipelineID, trigger.WorkspaceID).First(&pipeline).Error; err != nil {
			return err
		}
		var activeCount int64
		if err := tx.Model(&models.PipelineRun{}).
			Where("pipeline_id = ? AND trigger_type = ? AND status IN ?", pipeline.ID, pipelineRunTriggerTypeSchedule, []string{models.PipelineRunStatusQueued, models.PipelineRunStatusPending, models.PipelineRunStatusRunning}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		dueAt := trigger.NextRunAt.UTC()
		nextRunAt, err := services.ComputeNextScheduleTime(trigger.CronExpression, trigger.Timezone, dueAt)
		if err != nil {
			return err
		}
		trigger.NextRunAt = &nextRunAt
		trigger.LastRunAt = &dueAt
		trigger.LastTriggeredAt = &now
		trigger.LastEventTypes = strings.Join(enabledGitLabTriggerEvents(trigger), ",")
		if activeCount > 0 {
			return tx.Save(&trigger).Error
		}
		triggerUserID, triggerRole, _, err = h.resolvePipelineAutomationIdentity(tx, pipeline)
		if err != nil {
			return err
		}
		config, err = h.loadPipelineDefinitionConfig(tx, pipeline)
		if err != nil {
			return err
		}
		if valid, errMsg := config.ValidateTaskTypes(); !valid {
			return fmt.Errorf(errMsg)
		}
		if _, err := h.validatePipelineCredentialBindings(&config, triggerUserID, triggerRole, pipeline.ID, pipeline.WorkspaceID); err != nil {
			return err
		}
		runConfigSnapshot := models.PipelineRunConfigSnapshot{
			Options: map[string]interface{}{
				"scheduled_at": dueAt.Format(time.RFC3339),
			},
		}
		idempotencyKeyValue := buildScheduleIdempotencyKey(trigger.ID, dueAt)
		run, _, err = h.createPipelineRunRecordWithSnapshot(tx, pipeline, config, pipelineRunTriggerContext{
			TriggerType:     pipelineRunTriggerTypeSchedule,
			TriggerUser:     "schedule",
			TriggerUserID:   triggerUserID,
			TriggerUserRole: triggerRole,
			TriggerSource:   fmt.Sprintf("schedule:%d", trigger.ID),
			IdempotencyKey:  &idempotencyKeyValue,
			RunConfig:       runConfigSnapshot,
		})
		if err != nil {
			return err
		}
		return tx.Save(&trigger).Error
	})
	if err != nil {
		if errors.Is(err, errScheduledTriggerNotDue) {
			return false, nil
		}
		return false, err
	}
	if run == nil {
		return false, nil
	}
	syncLiveRunStateFromRun(run)
	executionConfig, err := buildExecutionPipelineConfig(config, models.PipelineRunConfigSnapshot{
		Options: map[string]interface{}{
			"scheduled_at": run.CreatedAt.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return false, err
	}
	h.startPipelineRunExecution(pipeline, run, executionConfig, triggerUserID, triggerRole)
	return true, nil
}

func (h *PipelineHandler) loadWorkspacePipeline(c *gin.Context) (models.Pipeline, bool) {
	id := c.Param("id")
	workspaceID := c.GetUint64("workspace_id")
	var pipeline models.Pipeline
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&pipeline).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "流水线不存在"})
		return models.Pipeline{}, false
	}
	return pipeline, true
}

func (h *PipelineHandler) ensurePipelineTrigger(pipelineID uint64, workspaceID uint64, userID uint64, rotateSecret bool) (*models.PipelineTrigger, error) {
	var trigger models.PipelineTrigger
	err := h.DB.Where("pipeline_id = ? AND workspace_id = ?", pipelineID, workspaceID).First(&trigger).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		trigger = models.PipelineTrigger{
			WorkspaceID:  workspaceID,
			PipelineID:   pipelineID,
			Provider:     "gitlab",
			Timezone:     "UTC",
			SecretToken:  uuid.NewString(),
			WebhookToken: uuid.NewString(),
			CreatedBy:    userID,
			UpdatedBy:    userID,
		}
		if err := h.DB.Create(&trigger).Error; err != nil {
			return nil, err
		}
	}
	updated := false
	if strings.TrimSpace(trigger.SecretToken) == "" || rotateSecret {
		trigger.SecretToken = uuid.NewString()
		updated = true
	}
	if strings.TrimSpace(trigger.WebhookToken) == "" {
		trigger.WebhookToken = uuid.NewString()
		updated = true
	}
	if strings.TrimSpace(trigger.Timezone) == "" {
		trigger.Timezone = "UTC"
		updated = true
	}
	if updated {
		trigger.UpdatedBy = userID
		if err := h.DB.Save(&trigger).Error; err != nil {
			return nil, err
		}
	}
	return &trigger, nil
}

func (h *PipelineHandler) buildPipelineTriggerResponse(c *gin.Context, trigger *models.PipelineTrigger) pipelineTriggerResponse {
	resp := pipelineTriggerResponse{Manual: true, Provider: "gitlab", Timezone: "UTC"}
	if trigger == nil {
		return resp
	}
	resp.Provider = trigger.Provider
	if resp.Provider == "" {
		resp.Provider = "gitlab"
	}
	resp.WebhookEnabled = trigger.WebhookEnabled
	resp.PushEnabled = trigger.PushEnabled
	resp.TagEnabled = trigger.TagEnabled
	resp.MergeRequestEnabled = trigger.MergeRequestEnabled
	resp.ScheduleEnabled = trigger.ScheduleEnabled
	resp.CronExpression = trigger.CronExpression
	resp.Timezone = trigger.Timezone
	resp.PushBranchFilters = trigger.PushBranchFilters
	resp.TagFilters = trigger.TagFilters
	resp.MergeRequestSourceBranchFilters = trigger.MergeRequestSourceBranchFilters
	resp.MergeRequestTargetBranchFilters = trigger.MergeRequestTargetBranchFilters
	if resp.Timezone == "" {
		resp.Timezone = "UTC"
	}
	resp.SecretToken = trigger.SecretToken
	resp.WebhookToken = trigger.WebhookToken
	resp.WebhookURL = buildGitLabWebhookURL(c, trigger.WebhookToken)
	resp.NextRunAt = trigger.NextRunAt
	resp.LastRunAt = trigger.LastRunAt
	resp.LastTriggeredAt = trigger.LastTriggeredAt
	return resp
}

func buildGitLabWebhookURL(c *gin.Context, token string) string {
	if c == nil || token == "" || c.Request == nil {
		return ""
	}
	scheme := strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(c.Request.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return "/api/pipeline/run/webhook/" + token
	}
	return fmt.Sprintf("%s://%s/api/pipeline/run/webhook/%s", scheme, host, token)
}

func enabledGitLabTriggerEvents(trigger models.PipelineTrigger) []string {
	events := make([]string, 0, 3)
	if trigger.PushEnabled {
		events = append(events, "push")
	}
	if trigger.TagEnabled {
		events = append(events, "tag_push")
	}
	if trigger.MergeRequestEnabled {
		events = append(events, "merge_request")
	}
	return events
}

func resolveGitLabTriggerMatch(trigger models.PipelineTrigger, payload gitlabWebhookPayload) (eventType string, refName string, commitSHA string, matched bool) {
	switch payload.ObjectKind {
	case "push":
		if !trigger.PushEnabled {
			return "", "", "", false
		}
		branch := trimGitRefPrefix(payload.Ref, "refs/heads/")
		if !matchesTriggerFilters(trigger.PushBranchFilters, branch) {
			return "", "", "", false
		}
		return "push", branch, strings.TrimSpace(payload.CheckoutSHA), true
	case "tag_push":
		if !trigger.TagEnabled {
			return "", "", "", false
		}
		tagName := trimGitRefPrefix(payload.Ref, "refs/tags/")
		if !matchesTriggerFilters(trigger.TagFilters, tagName) {
			return "", "", "", false
		}
		return "tag_push", tagName, strings.TrimSpace(payload.CheckoutSHA), true
	case "merge_request":
		if !trigger.MergeRequestEnabled {
			return "", "", "", false
		}
		action := strings.ToLower(strings.TrimSpace(payload.ObjectAttributes.Action))
		if action != "open" && action != "update" && action != "reopen" {
			return "", "", "", false
		}
		commitSHA = strings.TrimSpace(payload.ObjectAttributes.LastCommit.ID)
		if commitSHA == "" {
			commitSHA = strings.TrimSpace(payload.CheckoutSHA)
		}
		sourceBranch := strings.TrimSpace(payload.ObjectAttributes.SourceBranch)
		targetBranch := strings.TrimSpace(payload.ObjectAttributes.TargetBranch)
		if !matchesTriggerFilters(trigger.MergeRequestSourceBranchFilters, sourceBranch) {
			return "", "", "", false
		}
		if !matchesTriggerFilters(trigger.MergeRequestTargetBranchFilters, targetBranch) {
			return "", "", "", false
		}
		return "merge_request", sourceBranch, commitSHA, true
	default:
		return "", "", "", false
	}
}

func trimGitRefPrefix(ref string, prefix string) string {
	value := strings.TrimSpace(ref)
	if strings.HasPrefix(value, prefix) {
		return strings.TrimPrefix(value, prefix)
	}
	return value
}

func buildWebhookIdempotencyKey(triggerID uint64, eventType string, refName string, commitSHA string) string {
	return fmt.Sprintf("webhook:%d:%s:%s:%s", triggerID, eventType, refName, commitSHA)
}

func buildScheduleIdempotencyKey(triggerID uint64, dueAt time.Time) string {
	return fmt.Sprintf("schedule:%d:%d", triggerID, dueAt.UTC().Unix())
}

func matchesTriggerFilters(raw string, value string) bool {
	filters := parseTriggerFilters(raw)
	if len(filters) == 0 {
		return true
	}
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return false
	}
	for _, filter := range filters {
		matched, err := path.Match(filter, trimmedValue)
		if err == nil && matched {
			return true
		}
		if strings.EqualFold(filter, trimmedValue) {
			return true
		}
	}
	return false
}

func parseTriggerFilters(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	filters := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		filters = append(filters, value)
	}
	return filters
}

func applyGitReferenceToPipelineConfig(config *PipelineConfig, refName string, commitSHA string) {
	if config == nil {
		return
	}
	for i := range config.Nodes {
		node := &config.Nodes[i]
		if normalizePipelineTaskType(node.Type) != "git_clone" {
			continue
		}
		nodeCfg := normalizePipelineNodeConfig(node.Type, normalizePipelineTaskType(node.Type), node.getNodeConfig())
		if strings.TrimSpace(refName) != "" {
			nodeCfg["git_ref"] = refName
		}
		if strings.TrimSpace(commitSHA) != "" {
			nodeCfg["git_commit"] = commitSHA
		}
		node.Type = "git_clone"
		node.Config = nodeCfg
		node.DefinitionParams = nil
		node.Params = nil
	}
}

func normalizePipelineTaskType(taskType string) string {
	canonical, _, ok := getPipelineTaskDefinition(taskType)
	if !ok {
		return taskType
	}
	return canonical
}

func buildGitReferenceRuntimeInputs(config PipelineConfig, refName string, commitSHA string) map[string]map[string]interface{} {
	inputs := make(map[string]map[string]interface{})
	for _, node := range config.Nodes {
		if normalizePipelineTaskType(node.Type) != "git_clone" {
			continue
		}
		nodeInputs := make(map[string]interface{})
		if strings.TrimSpace(refName) != "" {
			nodeInputs["git_ref"] = refName
		}
		if strings.TrimSpace(commitSHA) != "" {
			nodeInputs["git_commit"] = commitSHA
		}
		if len(nodeInputs) > 0 {
			inputs[node.ID] = nodeInputs
		}
	}
	if len(inputs) == 0 {
		return nil
	}
	return inputs
}

func (h *PipelineHandler) resolvePipelineAutomationIdentity(db *gorm.DB, pipeline models.Pipeline) (uint64, string, string, error) {
	var owner models.User
	if err := db.First(&owner, pipeline.OwnerID).Error; err != nil {
		return 0, "", "", err
	}
	var member models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ? AND status = ?", pipeline.WorkspaceID, pipeline.OwnerID, models.WorkspaceMemberStatusActive).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fallbackRole := owner.Role
			if strings.TrimSpace(fallbackRole) == "" {
				fallbackRole = models.WorkspaceRoleDeveloper
			}
			return owner.ID, fallbackRole, owner.Username, nil
		}
		return 0, "", "", err
	}
	return owner.ID, member.Role, owner.Username, nil
}

func (h *PipelineHandler) launchPipelineRun(pipeline models.Pipeline, config PipelineConfig, trigger pipelineRunTriggerContext) (*models.PipelineRun, int, error) {
	authoredConfig := config
	if trigger.AuthoredConfig != nil {
		authoredConfig = clonePipelineConfig(*trigger.AuthoredConfig)
	}
	executionConfig, err := buildExecutionPipelineConfigForTrigger(authoredConfig, trigger)
	if err != nil {
		return nil, 0, err
	}
	run, buildNumber, err := h.createPipelineRunRecordWithSnapshot(h.DB, pipeline, authoredConfig, trigger)
	if err != nil {
		return nil, 0, err
	}
	syncLiveRunStateFromRun(run)
	h.startPipelineRunExecution(pipeline, run, executionConfig, trigger.TriggerUserID, trigger.TriggerUserRole)
	return run, buildNumber, nil
}

func (h *PipelineHandler) createPipelineRunRecordWithSnapshot(db *gorm.DB, pipeline models.Pipeline, config PipelineConfig, trigger pipelineRunTriggerContext) (*models.PipelineRun, int, error) {
	if err := h.syncPipelineDefinitionTriggers(db, pipeline.ID, &config); err != nil {
		return nil, 0, err
	}
	executionConfig, err := buildExecutionPipelineConfigForTrigger(config, trigger)
	if err != nil {
		return nil, 0, err
	}
	pipelineSnapshotJSON, err := json.Marshal(config)
	if err != nil {
		return nil, 0, err
	}
	executionConfigJSON, err := json.Marshal(executionConfig)
	if err != nil {
		return nil, 0, err
	}
	runConfigSnapshot := trigger.RunConfig
	if strings.TrimSpace(runConfigSnapshot.Trigger.Type) == "" {
		runConfigSnapshot.Trigger.Type = trigger.TriggerType
	}
	if strings.TrimSpace(runConfigSnapshot.Trigger.Source) == "" {
		runConfigSnapshot.Trigger.Source = trigger.TriggerSource
	}
	if strings.TrimSpace(runConfigSnapshot.Trigger.Operator) == "" {
		runConfigSnapshot.Trigger.Operator = trigger.TriggerUser
	}
	runConfigJSON, err := json.Marshal(runConfigSnapshot)
	if err != nil {
		return nil, 0, err
	}
	outputsJSON, err := json.Marshal(map[string]map[string]interface{}{})
	if err != nil {
		return nil, 0, err
	}
	hasAgentNode := pipelineConfigHasAgentNode(config)
	runStatus := models.PipelineRunStatusRunning
	startTime := time.Now().Unix()
	if hasAgentNode {
		runStatus = models.PipelineRunStatusQueued
		startTime = 0
	}
	resolvedNodesJSON, err := json.Marshal(buildInitialResolvedNodeSnapshots(config, runStatus))
	if err != nil {
		return nil, 0, err
	}
	bindingsSnapshotJSON, err := json.Marshal(buildRunBindingsSnapshot(db, pipeline.WorkspaceID, config))
	if err != nil {
		return nil, 0, err
	}
	events := []map[string]interface{}{{
		"event_type": "run_created",
		"time":       time.Now().Unix(),
		"payload":    map[string]interface{}{},
	}}
	if hasAgentNode {
		events = append(events, map[string]interface{}{
			"event_type": "run_queued",
			"time":       time.Now().Unix(),
			"payload":    map[string]interface{}{},
		})
	} else {
		events = append(events, map[string]interface{}{
			"event_type": "run_started",
			"time":       time.Now().Unix(),
			"payload":    map[string]interface{}{},
		})
	}
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return nil, 0, err
	}
	if trigger.IdempotencyKey != nil && strings.TrimSpace(*trigger.IdempotencyKey) != "" {
		var existing models.PipelineRun
		if err := db.Where("idempotency_key = ?", *trigger.IdempotencyKey).First(&existing).Error; err == nil {
			return &existing, existing.BuildNumber, nil
		}
	}
	run := &models.PipelineRun{
		WorkspaceID:      pipeline.WorkspaceID,
		PipelineID:       pipeline.ID,
		Status:           runStatus,
		TriggerType:      trigger.TriggerType,
		TriggerUser:      trigger.TriggerUser,
		TriggerUserID:    trigger.TriggerUserID,
		TriggerUserRole:  trigger.TriggerUserRole,
		TriggerSource:    trigger.TriggerSource,
		IdempotencyKey:   trigger.IdempotencyKey,
		StartTime:        startTime,
		Config:           string(executionConfigJSON),
		RunConfig:        string(runConfigJSON),
		PipelineSnapshot: string(pipelineSnapshotJSON),
		ResolvedNodes:    string(resolvedNodesJSON),
		Outputs:          string(outputsJSON),
		BindingsSnapshot: string(bindingsSnapshotJSON),
		Events:           string(eventsJSON),
	}
	buildNumber, err := (&PipelineHandler{DB: db}).createPipelineRunWithUniqueBuildNumber(run)
	if err != nil {
		if trigger.IdempotencyKey != nil && strings.TrimSpace(*trigger.IdempotencyKey) != "" {
			var existing models.PipelineRun
			if getErr := db.Where("idempotency_key = ?", *trigger.IdempotencyKey).First(&existing).Error; getErr == nil {
				return &existing, existing.BuildNumber, nil
			}
		}
		return nil, 0, err
	}
	return run, buildNumber, nil
}

func buildExecutionPipelineConfigForTrigger(config PipelineConfig, trigger pipelineRunTriggerContext) (PipelineConfig, error) {
	if trigger.ExecutionConfig != nil {
		return clonePipelineConfig(*trigger.ExecutionConfig), nil
	}
	return buildExecutionPipelineConfig(config, trigger.RunConfig)
}

func buildExecutionPipelineConfig(config PipelineConfig, runConfig models.PipelineRunConfigSnapshot) (PipelineConfig, error) {
	resolved := clonePipelineConfig(config)
	for i := range resolved.Nodes {
		node := &resolved.Nodes[i]
		canonical := normalizePipelineTaskType(node.Type)
		nodeConfig := normalizePipelineNodeConfig(node.Type, canonical, node.getNodeConfig())
		nodeInputs := cloneMap(runConfig.Inputs[node.ID])
		nodeConfig = applyRuntimeInputsToNodeConfig(canonical, nodeConfig, nodeInputs)
		if len(nodeInputs) > 0 {
			resolver := NewVariableResolver()
			resolver.SetInputs(nodeInputs)
			resolvedConfig, err := resolver.ResolveNodeConfig(nodeConfig)
			if err != nil {
				return PipelineConfig{}, err
			}
			nodeConfig = normalizePipelineNodeConfig(node.Type, canonical, resolvedConfig)
		}
		node.Config = nodeConfig
		node.DefinitionParams = nil
		node.Params = nil
	}
	return resolved, nil
}

func clonePipelineConfig(config PipelineConfig) PipelineConfig {
	data, err := json.Marshal(config)
	if err != nil {
		return config
	}
	var cloned PipelineConfig
	if err := json.Unmarshal(data, &cloned); err != nil {
		return config
	}
	return cloned
}

func applyRuntimeInputsToNodeConfig(taskType string, nodeConfig map[string]interface{}, nodeInputs map[string]interface{}) map[string]interface{} {
	if len(nodeInputs) == 0 {
		return nodeConfig
	}
	if nodeConfig == nil {
		nodeConfig = make(map[string]interface{})
	}
	for key, value := range nodeInputs {
		nodeConfig[key] = value
	}
	switch taskType {
	case "git_clone":
		if value, ok := nodeInputs["git_repo_url"]; ok {
			nodeConfig["git_repo_url"] = value
		}
		if value, ok := nodeInputs["git_ref"]; ok {
			nodeConfig["git_ref"] = value
		}
		if value, ok := nodeInputs["git_commit"]; ok {
			nodeConfig["git_commit"] = value
		}
		if value, ok := nodeInputs["git_checkout_path"]; ok {
			nodeConfig["git_checkout_path"] = value
		}
	}
	return nodeConfig
}

func (h *PipelineHandler) startPipelineRunExecution(pipeline models.Pipeline, run *models.PipelineRun, config PipelineConfig, triggerUserID uint64, triggerRole string) {
	if pipelineConfigHasAgentNode(config) {
		go h.scheduleQueuedPipelineRuns(h.DB)
		return
	}
	go h.executePipelineTasks(pipeline, run, config, triggerUserID, triggerRole)
}

func pipelineConfigHasAgentNode(config PipelineConfig) bool {
	for _, node := range config.Nodes {
		if isAgentPipelineTaskType(node.Type) {
			return true
		}
	}
	return false
}
