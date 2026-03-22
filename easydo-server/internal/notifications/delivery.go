package notifications

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"gorm.io/gorm"
)

const defaultEmailBatchLimit = 50
const maxEmailAttempts = 3

var smtpSendMail = smtp.SendMail

func SMTPConfigured() bool {
	if config.Config == nil {
		config.Init()
	}
	return config.Config.GetBool("notification.smtp.enabled") &&
		strings.TrimSpace(config.Config.GetString("notification.smtp.host")) != "" &&
		config.Config.GetInt("notification.smtp.port") > 0 &&
		strings.TrimSpace(config.Config.GetString("notification.smtp.from_address")) != ""
}

func DispatchPendingEmailDeliveries(db *gorm.DB, now time.Time, limit int) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("notification db is nil")
	}
	if !SMTPConfigured() {
		return 0, nil
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

	fromAddress := strings.TrimSpace(config.Config.GetString("notification.smtp.from_address"))
	fromName := strings.TrimSpace(config.Config.GetString("notification.smtp.from_name"))
	host := strings.TrimSpace(config.Config.GetString("notification.smtp.host"))
	port := config.Config.GetInt("notification.smtp.port")
	username := strings.TrimSpace(config.Config.GetString("notification.smtp.username"))
	password := config.Config.GetString("notification.smtp.password")
	address := fmt.Sprintf("%s:%d", host, port)

	auth := smtp.Auth(nil)
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}

	message := buildSMTPMessage(fromAddress, fromName, delivery.Destination, notification.Title, buildEmailBody(db, notification))
	delivery.AttemptCount++
	delivery.LastAttemptAt = now.Unix()
	err := smtpSendMail(address, auth, fromAddress, []string{delivery.Destination}, []byte(message))
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

func buildEmailBody(db *gorm.DB, notification models.Notification) string {
	fallback := strings.TrimSpace(notification.Content)
	if fallback == "" {
		fallback = notification.Title
	}
	if notification.ResourceType != models.NotificationResourceTypeWorkspaceInvite || notification.EventType != EventTypeWorkspaceInvitationCreated || notification.ResourceID == 0 || db == nil {
		return fallback
	}

	var invitation models.WorkspaceInvitation
	if err := db.First(&invitation, notification.ResourceID).Error; err != nil {
		return fallback
	}

	path := fmt.Sprintf("/workspace-invitations/%d", invitation.ID)
	publicURL := strings.TrimRight(strings.TrimSpace(config.Config.GetString("server.public_url")), "/")
	acceptURL := path
	if publicURL != "" {
		acceptURL = publicURL + path
	}

	lines := []string{fallback}
	if strings.TrimSpace(invitation.Role) != "" {
		lines = append(lines, fmt.Sprintf("邀请角色：%s", invitation.Role))
	}
	if invitation.ExpiresAt > 0 {
		lines = append(lines, fmt.Sprintf("有效期至：%s", time.Unix(invitation.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05 MST")))
	}
	if publicURL != "" {
		lines = append(lines, fmt.Sprintf("接受邀请：%s", acceptURL))
	} else {
		lines = append(lines, fmt.Sprintf("登录 EasyDo 后访问：%s", acceptURL))
	}
	return strings.Join(lines, "\n")
}
