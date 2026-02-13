package handlers

import (
	"net/http"
	"strconv"

	"easydo-server/internal/models"
	"gorm.io/gorm"
	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	DB *gorm.DB
}

func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{DB: models.DB}
}

func (h *WebhookHandler) ListConfigs(c *gin.Context) {
	var configs []models.WebhookConfig
	h.DB.Find(&configs)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": configs,
	})
}

func (h *WebhookHandler) GetConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var config models.WebhookConfig
	if err := h.DB.First(&config, id).Error; err != nil {
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
	config.CreatedBy = c.GetUint64("user_id")
	h.DB.Create(&config)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "创建成功",
		"data": config,
	})
}

func (h *WebhookHandler) UpdateConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var config models.WebhookConfig
	if err := h.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	h.DB.Model(&config).Updates(updates)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "更新成功",
		"data": config,
	})
}

func (h *WebhookHandler) DeleteConfig(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	h.DB.Delete(&models.WebhookConfig{}, id)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"message": "删除成功",
	})
}

func (h *WebhookHandler) ListEvents(c *gin.Context) {
	configID := c.Query("config_id")
	var events []models.WebhookEvent
	
	db := h.DB
	if configID != "" {
		db = db.Where("config_id = ?", configID)
	}
	db.Order("created_at DESC").Find(&events)
	
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": events,
	})
}
