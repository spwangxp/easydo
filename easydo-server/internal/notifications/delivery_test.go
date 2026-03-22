package notifications

import (
	"fmt"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
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
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.WorkspaceInvitation{}, &models.NotificationEvent{}, &models.NotificationAudience{}, &models.Notification{}, &models.InboxMessage{}, &models.NotificationDelivery{}, &models.NotificationPreference{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
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
	smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		message := string(msg)
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
	smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		if addr != "smtp.example.com:25" {
			return fmt.Errorf("unexpected smtp addr: %s", addr)
		}
		if from != "noreply@example.com" {
			return fmt.Errorf("unexpected from address: %s", from)
		}
		if len(to) != 1 || to[0] != "user@example.com" {
			return fmt.Errorf("unexpected recipients: %v", to)
		}
		if !strings.Contains(string(msg), "Subject: 通知主题") {
			return fmt.Errorf("missing subject in message: %s", string(msg))
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
	smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		return fmt.Errorf("dial tcp %s: connection refused", addr)
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
