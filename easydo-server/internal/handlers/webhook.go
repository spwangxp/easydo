package handlers

import (
	"net/http"
	"strconv"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type WebhookHandler struct {
	DB *gorm.DB
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{DB: models.DB}
}

func (h *WebhookHandler) ListConfigs(c *gin.Context) {
	workspaceID := c.GetUint64("workspace_id")
	var configs []models.WebhookConfig
	h.DB.Where("workspace_id = ?", workspaceID).Find(&configs)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": configs,
	})
}

func (h *WebhookHandler) GetConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspaceID := c.GetUint64("workspace_id")
	var config models.WebhookConfig
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": config,
	})
}

func (h *WebhookHandler) CreateConfig(c *gin.Context) {
	var config models.WebhookConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权在当前工作空间管理Webhook"})
		return
	}
	config.WorkspaceID = workspaceID
	config.CreatedBy = c.GetUint64("user_id")
	h.DB.Create(&config)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "创建成功",
		"data":    config,
	})
}

func (h *WebhookHandler) UpdateConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权在当前工作空间管理Webhook"})
		return
	}
	var config models.WebhookConfig
	if err := h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	delete(updates, "workspace_id")
	delete(updates, "created_by")

	h.DB.Model(&config).Updates(updates)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data":    config,
	})
}

func (h *WebhookHandler) DeleteConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspaceID := c.GetUint64("workspace_id")
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权在当前工作空间管理Webhook"})
		return
	}
	h.DB.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&models.WebhookConfig{})
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

func (h *WebhookHandler) ListEvents(c *gin.Context) {
	configID := c.Query("config_id")
	workspaceID := c.GetUint64("workspace_id")
	var events []models.WebhookEvent

	db := h.DB.Where("workspace_id = ?", workspaceID)
	if configID != "" {
		db = db.Where("config_id = ?", configID)
	}
	db.Order("created_at DESC").Find(&events)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": events,
	})
}
