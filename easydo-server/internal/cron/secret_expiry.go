package cron

import (
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"gorm.io/gorm"
)

func StartSecretExpiryChecker(db *gorm.DB) {
	webhookService := services.NewWebhookService(db)

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			checkExpiringSecrets(db, webhookService)
		}
	}()
}

func checkExpiringSecrets(db *gorm.DB, webhookService *services.WebhookService) {
	now := time.Now().Unix()
	sevenDaysLater := now + 7*24*3600

	var secrets []models.Secret
	db.Where("expires_at IS NOT NULL AND expires_at <= ? AND expires_at > ?", sevenDaysLater, now).Find(&secrets)

	for _, secret := range secrets {
		if secret.ExpiresAt != nil {
			daysLeft := (*secret.ExpiresAt - now) / 86400
			webhookService.SendWebhook("secret.expiring", map[string]interface{}{
				"secret_id":   secret.ID,
				"secret_name": secret.Name,
				"expires_at":  *secret.ExpiresAt,
				"days_left":   daysLeft,
			})
		}
	}

	var expiredSecrets []models.Secret
	db.Where("expires_at IS NOT NULL AND expires_at <= ?", now).Find(&expiredSecrets)
	for _, secret := range expiredSecrets {
		db.Model(&secret).Update("status", models.SecretStatusExpired)
	}

	var autoRotateSecrets []models.Secret
	db.Where("auto_rotate = ? AND expires_at IS NOT NULL AND expires_at <= ?", true, now+3*24*3600).Find(&autoRotateSecrets)
	for _, secret := range autoRotateSecrets {
		webhookService.SendWebhook("secret.auto_rotate", map[string]interface{}{
			"secret_id":   secret.ID,
			"secret_name": secret.Name,
			"message":     "密钥将在3天内自动轮换",
		})
	}
}
