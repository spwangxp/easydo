package notifications

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/internal/smtpclient"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openNotificationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.WorkspaceInvitation{}, &models.Pipeline{}, &models.PipelineRun{}, &models.Agent{}, &models.DeploymentRequest{}, &models.NotificationEvent{}, &models.NotificationAudience{}, &models.Notification{}, &models.InboxMessage{}, &models.NotificationDelivery{}, &models.NotificationPreference{}, &models.MasterKey{}, &models.NotificationSenderConfig{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("initialize master key failed: %v", err)
	}
	return db
}

func disableLegacySMTPConfig() {
	config.Init()
	config.Config.Set("notification.smtp.enabled", false)
	config.Config.Set("notification.smtp.host", "")
	config.Config.Set("notification.smtp.port", 0)
	config.Config.Set("notification.smtp.username", "")
	config.Config.Set("notification.smtp.password", "")
	config.Config.Set("notification.smtp.from_address", "")
	config.Config.Set("notification.smtp.from_name", "")
}

func createNotificationSenderConfig(t *testing.T, db *gorm.DB, scope string, workspaceID uint64, enabled bool, fromName, fromAddress, host string, port int) {
	t.Helper()
	encryptedPassword, err := models.EncryptCredentialPayload("smtp-secret")
	if err != nil {
		t.Fatalf("encrypt smtp password failed: %v", err)
	}
	config := models.NotificationSenderConfig{
		Scope:                 scope,
		WorkspaceID:           workspaceID,
		Enabled:               enabled,
		FromName:              fromName,
		FromAddress:           fromAddress,
		SMTPHost:              host,
		SMTPPort:              port,
		SMTPUsername:          fromAddress,
		SMTPPasswordEncrypted: encryptedPassword,
		SMTPTLSMode:           "plain",
		UpdatedBy:             1,
	}
	if err := db.Create(&config).Error; err != nil {
		t.Fatalf("create notification sender config failed: %v", err)
	}
}

func TestDispatchPendingEmailDeliveriesBuildsActionableWorkspaceInvitationEmail(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")
	config.Config.Set("server.public_url", "http://easydo.local")

	db := openNotificationTestDB(t)
	workspace := models.Workspace{Name: "admin Workspace", Slug: "admin-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: "user@example.com", Role: models.WorkspaceRoleViewer, TokenHash: "hashed-token", Status: models.WorkspaceInvitationStatusPending, InvitedBy: 1, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}
	notification := models.Notification{
		WorkspaceID:  workspace.ID,
		Family:       FamilyWorkspaceInvitation,
		EventType:    EventTypeWorkspaceInvitationCreated,
		ResourceType: models.NotificationResourceTypeWorkspaceInvite,
		ResourceID:   invitation.ID,
		Title:        "工作空间邀请",
		Content:      "你收到工作空间 \"admin Workspace\" 的加入邀请",
	}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	smtpSendMail = func(req smtpclient.SendRequest) error {
		message := string(req.Message)
		if !strings.Contains(message, "http://easydo.local/workspace-invitations/1") {
			return fmt.Errorf("missing invitation accept url in message: %s", message)
		}
		return nil
	}

	if _, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusDelivered {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusDelivered)
	}
}

func TestDispatchPendingEmailDeliveriesBuildsActionablePipelineRunEmail(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")
	config.Config.Set("server.public_url", "http://easydo.local")

	db := openNotificationTestDB(t)
	workspace := models.Workspace{Name: "demoWorkspace", Slug: "demo-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "发布流水线", WorkspaceID: workspace.ID, OwnerID: 1}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 27, Status: models.PipelineRunStatusFailed, TriggerType: "manual", ErrorMsg: "docker build failed"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}
	notification := models.Notification{
		WorkspaceID:  workspace.ID,
		Family:       FamilyPipelineRun,
		EventType:    EventTypePipelineRunFailed,
		ResourceType: models.NotificationResourceTypePipelineRun,
		ResourceID:   run.ID,
		Title:        "流水线运行状态更新",
		Content:      "流水线运行 #27 状态变更为 failed",
		Metadata:     fmt.Sprintf(`{"pipeline_id":%d,"pipeline_run_id":%d,"build_number":27,"trigger_type":"manual","status":"failed","error_msg":"docker build failed"}`, pipeline.ID, run.ID),
	}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	capturedMessage := ""
	smtpSendMail = func(req smtpclient.SendRequest) error {
		capturedMessage = string(req.Message)
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	for _, want := range []string{
		"Subject: [EasyDo] 流水线运行失败 · 发布流水线 #27",
		"事件：流水线运行失败",
		"工作区：demoWorkspace",
		"流水线：发布流水线",
		"运行编号：#27",
		"状态：failed",
		"触发方式：manual",
		"失败原因：docker build failed",
		fmt.Sprintf("详情：http://easydo.local/pipeline/%d?run_id=%d", pipeline.ID, run.ID),
	} {
		if !strings.Contains(capturedMessage, want) {
			t.Fatalf("missing %q in message: %s", want, capturedMessage)
		}
	}
}

func TestDispatchPendingEmailDeliveriesBuildsActionableDeploymentRequestEmail(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")
	config.Config.Set("server.public_url", "http://easydo.local")

	db := openNotificationTestDB(t)
	workspace := models.Workspace{Name: "demoWorkspace", Slug: "demo-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "发布流水线", WorkspaceID: workspace.ID, OwnerID: 1}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 12, Status: models.PipelineRunStatusFailed, TriggerType: "manual"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}
	request := models.DeploymentRequest{
		WorkspaceID:        workspace.ID,
		TemplateID:         1,
		TemplateVersionID:  1,
		TemplateType:       models.StoreTemplateTypeApp,
		TargetResourceID:   1,
		TargetResourceType: models.ResourceTypeK8sCluster,
		Status:             models.DeploymentRequestStatusFailed,
		PipelineID:         pipeline.ID,
		PipelineRunID:      run.ID,
		RequestedBy:        1,
		ValidationError:    "namespace is required",
	}
	if err := db.Create(&request).Error; err != nil {
		t.Fatalf("create deployment request failed: %v", err)
	}
	notification := models.Notification{
		WorkspaceID:  workspace.ID,
		Family:       FamilyDeploymentRequest,
		EventType:    EventTypeDeploymentRequestFailed,
		ResourceType: models.NotificationResourceTypeDeploymentRequest,
		ResourceID:   request.ID,
		Title:        "部署请求状态更新",
		Content:      "部署请求状态变更为 failed",
		Metadata:     fmt.Sprintf(`{"deployment_request_id":%d,"pipeline_id":%d,"pipeline_run_id":%d,"build_number":12,"status":"failed"}`, request.ID, pipeline.ID, run.ID),
	}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	capturedMessage := ""
	smtpSendMail = func(req smtpclient.SendRequest) error {
		capturedMessage = string(req.Message)
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	for _, want := range []string{
		fmt.Sprintf("Subject: [EasyDo] 发布申请失败 · 发布请求 #%d", request.ID),
		"事件：发布申请失败",
		"工作区：demoWorkspace",
		fmt.Sprintf("发布请求：#%d", request.ID),
		"状态：failed",
		"流水线运行：#12",
		"失败原因：namespace is required",
		fmt.Sprintf("详情：http://easydo.local/deploy?request_id=%d", request.ID),
	} {
		if !strings.Contains(capturedMessage, want) {
			t.Fatalf("missing %q in message: %s", want, capturedMessage)
		}
	}
}

func TestDispatchPendingEmailDeliveriesBuildsActionableAgentEmail(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")
	config.Config.Set("server.public_url", "http://easydo.local")

	db := openNotificationTestDB(t)
	workspace := models.Workspace{Name: "demoWorkspace", Slug: "demo-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	agent := models.Agent{Name: "builder-01", Host: "10.0.0.8", Port: 8080, Token: "token", WorkspaceID: workspace.ID, Status: models.AgentStatusOffline}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	notification := models.Notification{
		WorkspaceID:  workspace.ID,
		Family:       FamilyAgentLifecycle,
		EventType:    EventTypeAgentOffline,
		ResourceType: models.NotificationResourceTypeAgent,
		ResourceID:   agent.ID,
		Title:        "执行器离线",
		Content:      "执行器 builder-01 已离线",
		Metadata:     `{"agent_id":1,"agent_name":"builder-01"}`,
	}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	capturedMessage := ""
	smtpSendMail = func(req smtpclient.SendRequest) error {
		capturedMessage = string(req.Message)
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	for _, want := range []string{
		"Subject: [EasyDo] 执行器离线 · builder-01",
		"事件：执行器离线",
		"工作区：demoWorkspace",
		"执行器：builder-01",
		"详情：http://easydo.local/agent?agent_id=1",
	} {
		if !strings.Contains(capturedMessage, want) {
			t.Fatalf("missing %q in message: %s", want, capturedMessage)
		}
	}
}

func TestEmitQueuesAndDispatchesEmailUsingWorkspaceSenderConfig(t *testing.T) {
	disableLegacySMTPConfig()

	db := openNotificationTestDB(t)
	user := models.User{Username: "workspace-smtp-user", Email: "workspace-smtp-user@example.com", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "workspace smtp", Slug: "workspace-smtp", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	createNotificationSenderConfig(t, db, models.NotificationSenderScopeWorkspace, workspace.ID, true, "Workspace Mail", "workspace@example.com", "smtp.workspace.example.com", 2525)

	result, err := Emit(db, EventInput{
		WorkspaceID:    workspace.ID,
		Family:         FamilyPipelineRun,
		EventType:      EventTypePipelineRunSucceeded,
		Title:          "流水线成功",
		Content:        "pipeline finished",
		UserRecipients: []uint64{user.ID},
		Channels:       []string{models.NotificationChannelEmail},
		IdempotencyKey: "workspace-db-smtp-delivery",
	})
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if len(result.Deliveries) != 1 {
		t.Fatalf("delivery count=%d, want 1", len(result.Deliveries))
	}
	if result.Deliveries[0].Status != models.NotificationDeliveryStatusPending {
		t.Fatalf("delivery status=%s, want %s", result.Deliveries[0].Status, models.NotificationDeliveryStatusPending)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	smtpSendMail = func(req smtpclient.SendRequest) error {
		if req.Host != "smtp.workspace.example.com" || req.Port != 2525 {
			return fmt.Errorf("unexpected smtp target: %s:%d", req.Host, req.Port)
		}
		if req.From != "workspace@example.com" {
			return fmt.Errorf("unexpected from address: %s", req.From)
		}
		message := string(req.Message)
		if !strings.Contains(message, "From: Workspace Mail <workspace@example.com>") {
			return fmt.Errorf("unexpected from header in message: %s", message)
		}
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, result.Deliveries[0].ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusDelivered {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusDelivered)
	}
}

func TestDispatchPendingEmailDeliveriesFallsBackToPlatformSenderConfig(t *testing.T) {
	disableLegacySMTPConfig()

	db := openNotificationTestDB(t)
	workspace := models.Workspace{Name: "platform fallback", Slug: "platform-fallback", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 1}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	createNotificationSenderConfig(t, db, models.NotificationSenderScopePlatform, 0, true, "Platform Mail", "platform@example.com", "smtp.platform.example.com", 2526)
	createNotificationSenderConfig(t, db, models.NotificationSenderScopeWorkspace, workspace.ID, false, "Disabled Workspace Mail", "workspace-disabled@example.com", "smtp.workspace-disabled.example.com", 2527)
	notification := models.Notification{WorkspaceID: workspace.ID, Title: "通知主题", Content: "通知内容"}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	smtpSendMail = func(req smtpclient.SendRequest) error {
		if req.Host != "smtp.platform.example.com" || req.Port != 2526 {
			return fmt.Errorf("unexpected smtp target: %s:%d", req.Host, req.Port)
		}
		if req.From != "platform@example.com" {
			return fmt.Errorf("unexpected from address: %s", req.From)
		}
		if strings.Contains(string(req.Message), "workspace-disabled@example.com") {
			return fmt.Errorf("disabled workspace sender was used: %s", string(req.Message))
		}
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusDelivered {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusDelivered)
	}
}

func TestDispatchPendingEmailDeliveriesMarksNotConfiguredWhenNoSenderConfigExists(t *testing.T) {
	disableLegacySMTPConfig()

	db := openNotificationTestDB(t)
	notification := models.Notification{WorkspaceID: 42, Title: "通知主题", Content: "通知内容"}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusNotConfigured {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusNotConfigured)
	}
	if updated.ErrorMessage == "" {
		t.Fatal("expected error message to explain missing smtp configuration")
	}
}

func TestEmitQueuesEmailDeliveryWhenSMTPConfigured(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")

	db := openNotificationTestDB(t)
	user := models.User{Username: "notify-email-user", Email: "notify-email-user@example.com", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "notify-email-ws", Slug: "notify-email-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	result, err := Emit(db, EventInput{
		WorkspaceID:    workspace.ID,
		Family:         FamilyPipelineRun,
		EventType:      EventTypePipelineRunSucceeded,
		Title:          "流水线成功",
		Content:        "pipeline finished",
		UserRecipients: []uint64{user.ID},
		Channels:       []string{models.NotificationChannelEmail},
		IdempotencyKey: "notification-email-pending",
	})
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}
	if len(result.Deliveries) != 1 {
		t.Fatalf("delivery count=%d, want 1", len(result.Deliveries))
	}
	if result.Deliveries[0].Status != models.NotificationDeliveryStatusPending {
		t.Fatalf("delivery status=%s, want %s", result.Deliveries[0].Status, models.NotificationDeliveryStatusPending)
	}
}

func TestDispatchPendingEmailDeliveriesMarksDelivered(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "smtp.example.com")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")

	db := openNotificationTestDB(t)
	notification := models.Notification{Title: "通知主题", Content: "通知内容"}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	smtpSendMail = func(req smtpclient.SendRequest) error {
		if req.Host != "smtp.example.com" || req.Port != 25 {
			return fmt.Errorf("unexpected smtp target: %s:%d", req.Host, req.Port)
		}
		if req.From != "noreply@example.com" {
			return fmt.Errorf("unexpected from address: %s", req.From)
		}
		if len(req.To) != 1 || req.To[0] != "user@example.com" {
			return fmt.Errorf("unexpected recipients: %v", req.To)
		}
		if !strings.Contains(string(req.Message), "Subject: 通知主题") {
			return fmt.Errorf("missing subject in message: %s", string(req.Message))
		}
		return nil
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusDelivered {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusDelivered)
	}
	if updated.AttemptCount != 1 {
		t.Fatalf("attempt_count=%d, want 1", updated.AttemptCount)
	}
	if updated.SentAt == 0 {
		t.Fatal("expected sent_at to be set")
	}
}

func TestDispatchPendingEmailDeliveriesMarksFailedImmediatelyWhenSMTPRejected(t *testing.T) {
	config.Init()
	config.Config.Set("notification.smtp.enabled", true)
	config.Config.Set("notification.smtp.host", "127.0.0.1")
	config.Config.Set("notification.smtp.port", 25)
	config.Config.Set("notification.smtp.from_address", "noreply@example.com")
	config.Config.Set("notification.smtp.from_name", "EasyDo")

	db := openNotificationTestDB(t)
	notification := models.Notification{Title: "通知主题", Content: "通知内容"}
	if err := db.Create(&notification).Error; err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	delivery := models.NotificationDelivery{NotificationID: notification.ID, Channel: models.NotificationChannelEmail, Destination: "user@example.com", Status: models.NotificationDeliveryStatusPending, Provider: "smtp"}
	if err := db.Create(&delivery).Error; err != nil {
		t.Fatalf("create delivery failed: %v", err)
	}

	original := smtpSendMail
	t.Cleanup(func() { smtpSendMail = original })
	smtpSendMail = func(req smtpclient.SendRequest) error {
		return fmt.Errorf("dial tcp %s:%d: connection refused", req.Host, req.Port)
	}

	processed, err := DispatchPendingEmailDeliveries(db, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1", processed)
	}
	var updated models.NotificationDelivery
	if err := db.First(&updated, delivery.ID).Error; err != nil {
		t.Fatalf("reload delivery failed: %v", err)
	}
	if updated.Status != models.NotificationDeliveryStatusFailed {
		t.Fatalf("delivery status=%s, want %s", updated.Status, models.NotificationDeliveryStatusFailed)
	}
	if updated.AttemptCount != 1 {
		t.Fatalf("attempt_count=%d, want 1", updated.AttemptCount)
	}
	if updated.ErrorMessage == "" {
		t.Fatal("expected error_message to be recorded")
	}
	if updated.NextRetryAt != 0 {
		t.Fatalf("next_retry_at=%d, want 0", updated.NextRetryAt)
	}
}
