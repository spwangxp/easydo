package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

type notificationSenderConfigPayload struct {
	Enabled      bool   `json:"enabled"`
	FromName     string `json:"from_name"`
	FromAddress  string `json:"from_address"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	SMTPTLSMode  string `json:"smtp_tls_mode"`
}

type notificationSenderTestPayload struct {
	ToAddress string `json:"to_address"`
}

func (h *NotificationHandler) notificationSenderService() *services.NotificationSenderService {
	return &services.NotificationSenderService{DB: h.DB}
}

func (h *NotificationHandler) GetEffectiveNotificationSender(c *gin.Context) {
	result, err := h.notificationSenderService().GetEffectiveConfigForActor(c.Request.Context(), services.GetEffectiveNotificationSenderConfigRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
	})
	if err != nil {
		writeUserManagementError(c, err, "获取有效邮件发送配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}

func (h *NotificationHandler) SavePlatformNotificationSender(c *gin.Context) {
	payload, ok := bindNotificationSenderPayload(c)
	if !ok {
		return
	}
	result, err := h.notificationSenderService().SaveConfig(c.Request.Context(), services.SaveNotificationSenderConfigRequest{
		Actor:        userManagementActor(c),
		WorkspaceID:  c.GetUint64("workspace_id"),
		Scope:        models.NotificationSenderScopePlatform,
		Enabled:      payload.Enabled,
		FromName:     payload.FromName,
		FromAddress:  payload.FromAddress,
		SMTPHost:     payload.SMTPHost,
		SMTPPort:     payload.SMTPPort,
		SMTPUsername: payload.SMTPUsername,
		SMTPPassword: payload.SMTPPassword,
		SMTPTLSMode:  payload.SMTPTLSMode,
	})
	if err != nil {
		writeUserManagementError(c, err, "保存平台邮件发送配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": result})
}

func (h *NotificationHandler) SaveWorkspaceNotificationSender(c *gin.Context) {
	workspaceID, ok := parseNotificationSenderWorkspaceID(c)
	if !ok {
		return
	}
	payload, ok := bindNotificationSenderPayload(c)
	if !ok {
		return
	}
	result, err := h.notificationSenderService().SaveConfig(c.Request.Context(), services.SaveNotificationSenderConfigRequest{
		Actor:             userManagementActor(c),
		WorkspaceID:       c.GetUint64("workspace_id"),
		Scope:             models.NotificationSenderScopeWorkspace,
		TargetWorkspaceID: workspaceID,
		Enabled:           payload.Enabled,
		FromName:          payload.FromName,
		FromAddress:       payload.FromAddress,
		SMTPHost:          payload.SMTPHost,
		SMTPPort:          payload.SMTPPort,
		SMTPUsername:      payload.SMTPUsername,
		SMTPPassword:      payload.SMTPPassword,
		SMTPTLSMode:       payload.SMTPTLSMode,
	})
	if err != nil {
		writeUserManagementError(c, err, "保存工作区邮件发送配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": result})
}

func (h *NotificationHandler) TestNotificationSender(c *gin.Context) {
	var payload notificationSenderTestPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	result, err := h.notificationSenderService().TestEffectiveConfig(c.Request.Context(), services.TestNotificationSenderConfigRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		ToAddress:   payload.ToAddress,
	})
	if err != nil {
		writeUserManagementError(c, err, "测试邮件发送配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}

func bindNotificationSenderPayload(c *gin.Context) (notificationSenderConfigPayload, bool) {
	var payload notificationSenderConfigPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return notificationSenderConfigPayload{}, false
	}
	return payload, true
}

func parseNotificationSenderWorkspaceID(c *gin.Context) (uint64, bool) {
	workspaceID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作区ID"})
		return 0, false
	}
	return workspaceID, true
}
