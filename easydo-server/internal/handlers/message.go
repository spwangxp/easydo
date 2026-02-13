package handlers

import (
	"easydo-server/internal/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type MessageHandler struct {
	DB *gorm.DB
}

func NewMessageHandler() *MessageHandler {
	return &MessageHandler{DB: models.DB}
}

// GetMessageList returns messages for current user
func (h *MessageHandler) GetMessageList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	msgType := c.Query("type")
	unreadOnly := c.Query("unread_only") == "true"

	var messages []models.Message
	var total int64

	query := h.DB.Model(&models.Message{})

	if msgType != "" {
		query = query.Where("type = ?", msgType)
	}

	if unreadOnly {
		query = query.Where("is_read = ?", false)
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&messages)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  messages,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}

// GetUnreadCount returns count of unread messages
func (h *MessageHandler) GetUnreadCount(c *gin.Context) {
	var count int64
	h.DB.Model(&models.Message{}).Where("is_read = ?", false).Count(&count)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"unread_count": count,
		},
	})
}

// MarkAsRead marks a message as read
func (h *MessageHandler) MarkAsRead(c *gin.Context) {
	id := c.Param("id")

	h.DB.Model(&models.Message{}).Where("id = ?", id).Updates(map[string]interface{}{
		"is_read": true,
		"read_at": time.Now().Unix(),
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "标记成功",
	})
}

// MarkAllAsRead marks all messages as read
func (h *MessageHandler) MarkAllAsRead(c *gin.Context) {
	h.DB.Model(&models.Message{}).Where("is_read = ?", false).Updates(map[string]interface{}{
		"is_read": true,
		"read_at": time.Now().Unix(),
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "全部已读",
	})
}

// SendMessageToUser sends a message to a specific user
func (h *MessageHandler) SendMessageToUser(msg *models.Message, userID uint64) error {
	message := &models.Message{
		Type:       msg.Type,
		Title:      msg.Title,
		Content:    msg.Content,
		SenderID:   msg.SenderID,
		SenderType: msg.SenderType,
		Priority:   msg.Priority,
		Metadata:   msg.Metadata,
	}
	return h.DB.Create(message).Error
}

// SendMessageToAllAdmins sends a message to all admin users
func (h *MessageHandler) SendMessageToAllAdmins(msg *models.Message) error {
	var admins []models.User
	if err := h.DB.Where("role = ?", "admin").Find(&admins).Error; err != nil {
		return err
	}

	for range admins {
		message := &models.Message{
			Type:       msg.Type,
			Title:      msg.Title,
			Content:    msg.Content,
			SenderID:   msg.SenderID,
			SenderType: msg.SenderType,
			Priority:   msg.Priority,
			Metadata:   msg.Metadata,
		}
		if err := h.DB.Create(message).Error; err != nil {
			return err
		}
	}

	return nil
}
