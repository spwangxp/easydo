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
	userID := c.GetUint64("user_id")
	workspaceID := c.GetUint64("workspace_id")

	var messages []models.Message
	var total int64

	query := h.DB.Model(&models.Message{}).Where("recipient_id = ? AND workspace_id = ?", userID, workspaceID)

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
	userID := c.GetUint64("user_id")
	workspaceID := c.GetUint64("workspace_id")
	var count int64
	h.DB.Model(&models.Message{}).Where("recipient_id = ? AND workspace_id = ? AND is_read = ?", userID, workspaceID, false).Count(&count)

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
	userID := c.GetUint64("user_id")
	workspaceID := c.GetUint64("workspace_id")

	result := h.DB.Model(&models.Message{}).Where("id = ? AND recipient_id = ? AND workspace_id = ?", id, userID, workspaceID).Updates(map[string]interface{}{
		"is_read": true,
		"read_at": time.Now().Unix(),
	})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "消息不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "标记成功",
	})
}

// MarkAllAsRead marks all messages as read
func (h *MessageHandler) MarkAllAsRead(c *gin.Context) {
	userID := c.GetUint64("user_id")
	workspaceID := c.GetUint64("workspace_id")
	h.DB.Model(&models.Message{}).Where("recipient_id = ? AND workspace_id = ? AND is_read = ?", userID, workspaceID, false).Updates(map[string]interface{}{
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
		WorkspaceID: msg.WorkspaceID,
		RecipientID: &userID,
		Type:        msg.Type,
		Title:       msg.Title,
		Content:     msg.Content,
		SenderID:    msg.SenderID,
		SenderType:  msg.SenderType,
		Priority:    msg.Priority,
		Metadata:    msg.Metadata,
	}
	return h.DB.Create(message).Error
}

// SendMessageToAllAdmins sends a message to all admin users
func (h *MessageHandler) SendMessageToAllAdmins(msg *models.Message) error {
	var admins []models.User
	if err := h.DB.Where("role = ?", "admin").Find(&admins).Error; err != nil {
		return err
	}

	for _, admin := range admins {
		adminUserID := admin.ID
		message := &models.Message{
			WorkspaceID: msg.WorkspaceID,
			RecipientID: &adminUserID,
			Type:        msg.Type,
			Title:       msg.Title,
			Content:     msg.Content,
			SenderID:    msg.SenderID,
			SenderType:  msg.SenderType,
			Priority:    msg.Priority,
			Metadata:    msg.Metadata,
		}
		if err := h.DB.Create(message).Error; err != nil {
			return err
		}
	}

	return nil
}
