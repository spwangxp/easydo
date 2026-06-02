package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/internal/smtpclient"
	"gorm.io/gorm"
)

const defaultEmailBatchLimit = 50
const maxEmailAttempts = 3

var smtpSendMail = smtpclient.Send

type smtpDeliveryConfig struct {
	Configured  bool
	FromAddress string
	FromName    string
	Host        string
	Port        int
	TLSMode     string
	Username    string
	Password    string
	Source      string
}

func SMTPConfigured() bool {
	return legacySMTPDeliveryConfig().Configured
}

func SMTPConfiguredForWorkspace(db *gorm.DB, workspaceID uint64) bool {
	cfg, err := resolveSMTPDeliveryConfig(db, workspaceID)
	return err == nil && cfg.Configured
}

func legacySMTPDeliveryConfig() smtpDeliveryConfig {
	if config.Config == nil {
		config.Init()
	}
	cfg := smtpDeliveryConfig{
		FromAddress: strings.TrimSpace(config.Config.GetString("notification.smtp.from_address")),
		FromName:    strings.TrimSpace(config.Config.GetString("notification.smtp.from_name")),
		Host:        strings.TrimSpace(config.Config.GetString("notification.smtp.host")),
		Port:        config.Config.GetInt("notification.smtp.port"),
		TLSMode:     strings.TrimSpace(config.Config.GetString("notification.smtp.tls_mode")),
		Username:    strings.TrimSpace(config.Config.GetString("notification.smtp.username")),
		Password:    config.Config.GetString("notification.smtp.password"),
		Source:      "legacy",
	}
	cfg.Configured = config.Config.GetBool("notification.smtp.enabled") && smtpDeliveryConfigUsable(cfg)
	return cfg
}

func resolveSMTPDeliveryConfig(db *gorm.DB, workspaceID uint64) (smtpDeliveryConfig, error) {
	if db != nil {
		if workspaceID > 0 {
			cfg, ok, err := loadDBSMTPDeliveryConfig(db, models.NotificationSenderScopeWorkspace, workspaceID)
			if err != nil {
				return smtpDeliveryConfig{}, err
			}
			if ok {
				return cfg, nil
			}
		}
		cfg, ok, err := loadDBSMTPDeliveryConfig(db, models.NotificationSenderScopePlatform, 0)
		if err != nil {
			return smtpDeliveryConfig{}, err
		}
		if ok {
			return cfg, nil
		}
	}
	if cfg := legacySMTPDeliveryConfig(); cfg.Configured {
		return cfg, nil
	}
	return smtpDeliveryConfig{}, nil
}

func loadDBSMTPDeliveryConfig(db *gorm.DB, scope string, workspaceID uint64) (smtpDeliveryConfig, bool, error) {
	var sender models.NotificationSenderConfig
	err := db.Where("scope = ? AND workspace_id = ? AND enabled = ?", scope, workspaceID, true).First(&sender).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || missingNotificationSenderConfigTable(err) {
			return smtpDeliveryConfig{}, false, nil
		}
		return smtpDeliveryConfig{}, false, err
	}

	cfg := smtpDeliveryConfig{
		FromAddress: strings.TrimSpace(sender.FromAddress),
		FromName:    strings.TrimSpace(sender.FromName),
		Host:        strings.TrimSpace(sender.SMTPHost),
		Port:        sender.SMTPPort,
		TLSMode:     strings.TrimSpace(sender.SMTPTLSMode),
		Username:    strings.TrimSpace(sender.SMTPUsername),
		Source:      sender.Scope,
	}
	if !smtpDeliveryConfigUsable(cfg) {
		return smtpDeliveryConfig{}, false, nil
	}
	if strings.TrimSpace(sender.SMTPPasswordEncrypted) != "" {
		password, err := models.DecryptCredentialPayload(sender.SMTPPasswordEncrypted)
		if err != nil {
			return smtpDeliveryConfig{}, false, err
		}
		cfg.Password = string(password)
	}
	cfg.Configured = true
	return cfg, true, nil
}

func smtpDeliveryConfigUsable(cfg smtpDeliveryConfig) bool {
	return strings.TrimSpace(cfg.FromAddress) != "" && strings.TrimSpace(cfg.Host) != "" && cfg.Port > 0
}

func missingNotificationSenderConfigTable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "doesn't exist")
}

func DispatchPendingEmailDeliveries(db *gorm.DB, now time.Time, limit int) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("notification db is nil")
	}
	if limit <= 0 {
		limit = defaultEmailBatchLimit
	}

	var deliveries []models.NotificationDelivery
	if err := db.Where("channel = ? AND status = ? AND (next_retry_at IS NULL OR next_retry_at = 0 OR next_retry_at <= ?)", models.NotificationChannelEmail, models.NotificationDeliveryStatusPending, now.Unix()).
		Order("id ASC").
		Limit(limit).
		Find(&deliveries).Error; err != nil {
		return 0, err
	}

	processed := 0
	for _, delivery := range deliveries {
		if err := dispatchOneEmailDelivery(db, &delivery, now); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func dispatchOneEmailDelivery(db *gorm.DB, delivery *models.NotificationDelivery, now time.Time) error {
	if delivery == nil || delivery.ID == 0 {
		return nil
	}
	var notification models.Notification
	if err := db.First(&notification, delivery.NotificationID).Error; err != nil {
		return err
	}

	smtpConfig, err := resolveSMTPDeliveryConfig(db, notification.WorkspaceID)
	if err != nil {
		return err
	}
	if !smtpConfig.Configured {
		delivery.Status = models.NotificationDeliveryStatusNotConfigured
		delivery.ErrorMessage = "smtp delivery is not configured"
		delivery.NextRetryAt = 0
		return db.Save(delivery).Error
	}

	message := buildSMTPMessage(smtpConfig.FromAddress, smtpConfig.FromName, delivery.Destination, buildEmailSubject(db, notification), buildEmailBody(db, notification))
	delivery.AttemptCount++
	delivery.LastAttemptAt = now.Unix()
	err = smtpSendMail(smtpclient.SendRequest{
		Host:     smtpConfig.Host,
		Port:     smtpConfig.Port,
		TLSMode:  smtpConfig.TLSMode,
		Username: smtpConfig.Username,
		Password: smtpConfig.Password,
		From:     smtpConfig.FromAddress,
		To:       []string{delivery.Destination},
		Message:  []byte(message),
	})
	if err != nil {
		delivery.ErrorMessage = err.Error()
		delivery.Status = models.NotificationDeliveryStatusFailed
		delivery.NextRetryAt = 0
		return db.Save(delivery).Error
	}

	delivery.Status = models.NotificationDeliveryStatusDelivered
	delivery.ErrorMessage = ""
	delivery.NextRetryAt = 0
	delivery.SentAt = now.Unix()
	return db.Save(delivery).Error
}

func buildSMTPMessage(fromAddress, fromName, toAddress, subject, body string) string {
	fromHeader := fromAddress
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, fromAddress)
	}
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody == "" {
		trimmedBody = subject
	}
	return strings.Join([]string{
		fmt.Sprintf("From: %s", fromHeader),
		fmt.Sprintf("To: %s", toAddress),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		trimmedBody,
		"",
	}, "\r\n")
}

func buildEmailSubject(db *gorm.DB, notification models.Notification) string {
	label := notificationEventLabel(notification.EventType)
	if label == "" {
		return notification.Title
	}
	object := notificationEmailObjectLabel(db, notification)
	if object != "" {
		return fmt.Sprintf("[EasyDo] %s · %s", label, object)
	}
	return fmt.Sprintf("[EasyDo] %s", label)
}

func buildEmailBody(db *gorm.DB, notification models.Notification) string {
	fallback := strings.TrimSpace(notification.Content)
	if fallback == "" {
		fallback = notification.Title
	}
	label := notificationEventLabel(notification.EventType)
	if label == "" {
		return fallback
	}

	metadata := parseNotificationMetadata(notification.Metadata)
	lines := []string{
		fmt.Sprintf("事件：%s", label),
	}
	if fallback != "" {
		lines = append(lines, fmt.Sprintf("摘要：%s", fallback))
	}
	if workspace := notificationWorkspaceName(db, notification.WorkspaceID); workspace != "" {
		lines = append(lines, fmt.Sprintf("工作区：%s", workspace))
	}
	lines = append(lines, notificationEventDetailLines(db, notification, metadata)...)
	if detailURL := buildNotificationDetailURL(db, notification, metadata); detailURL != "" {
		lines = append(lines, fmt.Sprintf("详情：%s", detailURL))
	}
	if !notification.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("时间：%s", notification.CreatedAt.Local().Format("2006-01-02 15:04:05 MST")))
	}
	return strings.Join(compactUniqueLines(lines), "\n")
}

func notificationEventLabel(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	for _, item := range EventDefinitions() {
		if item.EventType == eventType {
			return item.Label
		}
	}
	return ""
}

func notificationEmailObjectLabel(db *gorm.DB, notification models.Notification) string {
	metadata := parseNotificationMetadata(notification.Metadata)
	switch notification.ResourceType {
	case models.NotificationResourceTypePipelineRun:
		run, pipeline, ok := loadNotificationPipelineRun(db, notification, metadata)
		if ok {
			pipelineName := strings.TrimSpace(pipeline.Name)
			if pipelineName == "" && run.PipelineID > 0 {
				pipelineName = fmt.Sprintf("流水线 #%d", run.PipelineID)
			}
			if run.BuildNumber > 0 && pipelineName != "" {
				return fmt.Sprintf("%s #%d", pipelineName, run.BuildNumber)
			}
			return pipelineName
		}
	case models.NotificationResourceTypeAgent:
		if agentName := notificationAgentName(db, notification, metadata); agentName != "" {
			return agentName
		}
	case models.NotificationResourceTypeDeploymentRequest:
		requestID := notification.ResourceID
		if requestID == 0 {
			requestID = metadataUint(metadata, "deployment_request_id")
		}
		if requestID > 0 {
			return fmt.Sprintf("发布请求 #%d", requestID)
		}
	case models.NotificationResourceTypeWorkspaceInvite, models.NotificationResourceTypeWorkspaceMember, models.NotificationResourceTypeWorkspace:
		return notificationWorkspaceName(db, notification.WorkspaceID)
	}
	return ""
}

func notificationEventDetailLines(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) []string {
	switch notification.ResourceType {
	case models.NotificationResourceTypeWorkspaceInvite:
		return workspaceInvitationEmailLines(db, notification)
	case models.NotificationResourceTypePipelineRun:
		return pipelineRunEmailLines(db, notification, metadata)
	case models.NotificationResourceTypeAgent:
		return agentEmailLines(db, notification, metadata)
	case models.NotificationResourceTypeDeploymentRequest:
		return deploymentRequestEmailLines(db, notification, metadata)
	case models.NotificationResourceTypeWorkspaceMember:
		return workspaceMemberEmailLines(db, notification, metadata)
	default:
		return nil
	}
}

func workspaceInvitationEmailLines(db *gorm.DB, notification models.Notification) []string {
	if notification.ResourceID == 0 || db == nil {
		return nil
	}
	var invitation models.WorkspaceInvitation
	if err := db.First(&invitation, notification.ResourceID).Error; err != nil {
		return nil
	}

	lines := []string{}
	if strings.TrimSpace(invitation.Role) != "" {
		lines = append(lines, fmt.Sprintf("邀请角色：%s", invitation.Role))
	}
	if invitation.ExpiresAt > 0 {
		lines = append(lines, fmt.Sprintf("有效期至：%s", time.Unix(invitation.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05 MST")))
	}
	if notification.EventType == EventTypeWorkspaceInvitationCreated {
		lines = append(lines, fmt.Sprintf("接受邀请：%s", absoluteNotificationURL(fmt.Sprintf("/workspace-invitations/%d", invitation.ID))))
	}
	return lines
}

func pipelineRunEmailLines(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) []string {
	run, pipeline, ok := loadNotificationPipelineRun(db, notification, metadata)
	if !ok {
		return nil
	}
	lines := []string{}
	if strings.TrimSpace(pipeline.Name) != "" {
		lines = append(lines, fmt.Sprintf("流水线：%s", pipeline.Name))
	} else if run.PipelineID > 0 {
		lines = append(lines, fmt.Sprintf("流水线：#%d", run.PipelineID))
	}
	if run.BuildNumber > 0 {
		lines = append(lines, fmt.Sprintf("运行编号：#%d", run.BuildNumber))
	}
	status := firstNonEmpty(run.Status, metadataString(metadata, "status", "run_status"))
	if status != "" {
		lines = append(lines, fmt.Sprintf("状态：%s", status))
	}
	triggerType := firstNonEmpty(run.TriggerType, metadataString(metadata, "trigger_type"))
	if triggerType != "" {
		lines = append(lines, fmt.Sprintf("触发方式：%s", triggerType))
	}
	errorMessage := firstNonEmpty(run.ErrorMsg, metadataString(metadata, "error_msg", "failure_reason"))
	if errorMessage != "" {
		lines = append(lines, fmt.Sprintf("失败原因：%s", truncateEmailValue(errorMessage, 500)))
	}
	return lines
}

func agentEmailLines(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) []string {
	lines := []string{}
	if agentName := notificationAgentName(db, notification, metadata); agentName != "" {
		lines = append(lines, fmt.Sprintf("执行器：%s", agentName))
	}
	if agentStatus := notificationAgentStatus(db, notification, metadata); agentStatus != "" {
		lines = append(lines, fmt.Sprintf("状态：%s", agentStatus))
	}
	return lines
}

func deploymentRequestEmailLines(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) []string {
	requestID := notification.ResourceID
	if requestID == 0 {
		requestID = metadataUint(metadata, "deployment_request_id")
	}
	lines := []string{}
	if requestID > 0 {
		lines = append(lines, fmt.Sprintf("发布请求：#%d", requestID))
	}
	status := metadataString(metadata, "status")
	var loadedRequest models.DeploymentRequest
	requestLoaded := false
	if db != nil && requestID > 0 {
		if err := db.First(&loadedRequest, requestID).Error; err == nil {
			requestLoaded = true
			if strings.TrimSpace(string(loadedRequest.Status)) != "" {
				status = string(loadedRequest.Status)
			}
		}
	}
	if status != "" {
		lines = append(lines, fmt.Sprintf("状态：%s", status))
	}
	if buildNumber := metadataInt(metadata, "build_number"); buildNumber > 0 {
		lines = append(lines, fmt.Sprintf("流水线运行：#%d", buildNumber))
	}
	failureReason := metadataString(metadata, "failure_reason", "error_msg", "validation_error")
	if failureReason == "" && requestLoaded {
		failureReason = loadedRequest.ValidationError
	}
	if failureReason != "" {
		lines = append(lines, fmt.Sprintf("失败原因：%s", truncateEmailValue(failureReason, 500)))
	}
	return lines
}

func workspaceMemberEmailLines(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) []string {
	role := metadataString(metadata, "role", "workspace_role", "member_role")
	if role == "" && db != nil && notification.ResourceID > 0 {
		var member models.WorkspaceMember
		if err := db.First(&member, notification.ResourceID).Error; err == nil {
			role = member.Role
		}
	}
	if role == "" {
		return nil
	}
	return []string{fmt.Sprintf("工作区角色：%s", role)}
}

func buildNotificationDetailURL(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) string {
	switch notification.ResourceType {
	case models.NotificationResourceTypeWorkspaceInvite:
		if notification.EventType == EventTypeWorkspaceInvitationCreated && notification.ResourceID > 0 {
			return absoluteNotificationURL(fmt.Sprintf("/workspace-invitations/%d", notification.ResourceID))
		}
		return absoluteNotificationURL("/workspace-governance?tab=invitations")
	case models.NotificationResourceTypeWorkspaceMember, models.NotificationResourceTypeWorkspace:
		return absoluteNotificationURL("/workspace-governance?tab=members")
	case models.NotificationResourceTypeAgent:
		agentID := notification.ResourceID
		if agentID == 0 {
			agentID = metadataUint(metadata, "agent_id")
		}
		if agentID > 0 {
			return absoluteNotificationURL(fmt.Sprintf("/agent?agent_id=%d", agentID))
		}
		return absoluteNotificationURL("/agent")
	case models.NotificationResourceTypePipelineRun:
		run, _, ok := loadNotificationPipelineRun(db, notification, metadata)
		if ok && run.PipelineID > 0 {
			path := fmt.Sprintf("/pipeline/%d", run.PipelineID)
			if run.ID > 0 {
				path = fmt.Sprintf("%s?run_id=%d", path, run.ID)
			}
			return absoluteNotificationURL(path)
		}
		if pipelineID := metadataUint(metadata, "pipeline_id"); pipelineID > 0 {
			return absoluteNotificationURL(fmt.Sprintf("/pipeline/%d", pipelineID))
		}
		return absoluteNotificationURL("/pipeline")
	case models.NotificationResourceTypeDeploymentRequest:
		requestID := notification.ResourceID
		if requestID == 0 {
			requestID = metadataUint(metadata, "deployment_request_id")
		}
		if requestID > 0 {
			return absoluteNotificationURL(fmt.Sprintf("/deploy?request_id=%d", requestID))
		}
		return absoluteNotificationURL("/deploy")
	default:
		return absoluteNotificationURL("/messages")
	}
}

func absoluteNotificationURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if config.Config == nil {
		config.Init()
	}
	publicURL := strings.TrimRight(strings.TrimSpace(config.Config.GetString("server.public_url")), "/")
	if publicURL == "" {
		return path
	}
	return publicURL + path
}

func parseNotificationMetadata(raw string) map[string]interface{} {
	result := map[string]interface{}{}
	if strings.TrimSpace(raw) == "" {
		return result
	}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func notificationWorkspaceName(db *gorm.DB, workspaceID uint64) string {
	if db == nil || workspaceID == 0 {
		return ""
	}
	var workspace models.Workspace
	if err := db.Select("id", "name").First(&workspace, workspaceID).Error; err != nil {
		return fmt.Sprintf("#%d", workspaceID)
	}
	if strings.TrimSpace(workspace.Name) != "" {
		return strings.TrimSpace(workspace.Name)
	}
	return fmt.Sprintf("#%d", workspaceID)
}

func loadNotificationPipelineRun(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) (models.PipelineRun, models.Pipeline, bool) {
	var run models.PipelineRun
	if db != nil && notification.ResourceID > 0 {
		if err := db.First(&run, notification.ResourceID).Error; err == nil {
			var pipeline models.Pipeline
			if run.PipelineID > 0 {
				_ = db.Select("id", "name").First(&pipeline, run.PipelineID).Error
			}
			return run, pipeline, true
		}
	}
	run.ID = firstNonZeroUint64(notification.ResourceID, metadataUint(metadata, "pipeline_run_id", "run_id"))
	run.PipelineID = metadataUint(metadata, "pipeline_id")
	run.BuildNumber = metadataInt(metadata, "build_number")
	run.Status = metadataString(metadata, "status", "run_status")
	run.TriggerType = metadataString(metadata, "trigger_type")
	run.ErrorMsg = metadataString(metadata, "error_msg", "failure_reason")
	if run.ID == 0 && run.PipelineID == 0 && run.BuildNumber == 0 {
		return models.PipelineRun{}, models.Pipeline{}, false
	}
	var pipeline models.Pipeline
	if db != nil && run.PipelineID > 0 {
		_ = db.Select("id", "name").First(&pipeline, run.PipelineID).Error
	}
	return run, pipeline, true
}

func notificationAgentName(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) string {
	if db != nil && notification.ResourceID > 0 {
		var agent models.Agent
		if err := db.Select("id", "name").First(&agent, notification.ResourceID).Error; err == nil && strings.TrimSpace(agent.Name) != "" {
			return strings.TrimSpace(agent.Name)
		}
	}
	if name := metadataString(metadata, "agent_name", "name"); name != "" {
		return name
	}
	agentID := firstNonZeroUint64(notification.ResourceID, metadataUint(metadata, "agent_id"))
	if agentID > 0 {
		return fmt.Sprintf("Agent #%d", agentID)
	}
	return ""
}

func notificationAgentStatus(db *gorm.DB, notification models.Notification, metadata map[string]interface{}) string {
	if db != nil && notification.ResourceID > 0 {
		var agent models.Agent
		if err := db.Select("id", "status").First(&agent, notification.ResourceID).Error; err == nil && strings.TrimSpace(agent.Status) != "" {
			return strings.TrimSpace(agent.Status)
		}
	}
	return metadataString(metadata, "status", "agent_status")
}

func metadataString(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case int:
			return fmt.Sprintf("%d", typed)
		case uint64:
			return fmt.Sprintf("%d", typed)
		default:
			text := strings.TrimSpace(fmt.Sprint(typed))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func metadataUint(metadata map[string]interface{}, keys ...string) uint64 {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed > 0 {
				return uint64(typed)
			}
		case int:
			if typed > 0 {
				return uint64(typed)
			}
		case uint64:
			return typed
		case string:
			var parsed uint64
			if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

func metadataInt(metadata map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case string:
			var parsed int
			if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func compactUniqueLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		result = append(result, line)
	}
	return result
}

func truncateEmailValue(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len([]rune(value)) <= maxLen {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxLen]) + "..."
}
