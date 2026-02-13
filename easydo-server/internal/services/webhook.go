package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

type WebhookService struct {
	db *gorm.DB
}

func NewWebhookService(db *gorm.DB) *WebhookService {
	return &WebhookService{db: db}
}

func (s *WebhookService) SendWebhook(eventType string, payload map[string]interface{}) error {
	var configs []models.WebhookConfig
	s.db.Where("is_active = ?", true).Find(&configs)

	for _, config := range configs {
		var events []string
		json.Unmarshal([]byte(config.Events), &events)

		if !containsString(events, eventType) {
			continue
		}

		go s.sendToURL(config, eventType, payload)
	}

	return nil
}

func (s *WebhookService) sendToURL(config models.WebhookConfig, eventType string, payload map[string]interface{}) {
	payload["event_type"] = eventType
	payload["timestamp"] = time.Now().Unix()

	body, _ := json.Marshal(payload)
	signature := s.generateSignature(body, config.Secret)

	req, _ := http.NewRequest("POST", config.URL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)
	req.Header.Set("X-Webhook-Timestamp", time.Now().Format(time.RFC3339))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)

	status := "sent"
	responseBody := ""
	if err != nil {
		status = "failed"
	} else {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		responseBody = buf.String()
		resp.Body.Close()
	}

	event := models.WebhookEvent{
		ConfigID:  config.ID,
		EventType: eventType,
		Payload:   string(body),
		Status:    status,
		Response:  responseBody,
	}
	s.db.Create(&event)
}

func (s *WebhookService) generateSignature(body []byte, secret string) string {
	if secret == "" {
		return ""
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
