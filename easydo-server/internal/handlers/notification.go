package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	internalnotifications "easydo-server/internal/notifications"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	NotificationFamilyWorkspaceInvitation = internalnotifications.FamilyWorkspaceInvitation
	NotificationFamilyWorkspaceMember     = internalnotifications.FamilyWorkspaceMember
	NotificationFamilyAgentLifecycle      = internalnotifications.FamilyAgentLifecycle
	NotificationFamilyPipelineRun         = internalnotifications.FamilyPipelineRun
	NotificationFamilyDeploymentRequest   = internalnotifications.FamilyDeploymentRequest

	NotificationEventTypeWorkspaceInvitationCreated  = internalnotifications.EventTypeWorkspaceInvitationCreated
	NotificationEventTypeWorkspaceInvitationAccepted = internalnotifications.EventTypeWorkspaceInvitationAccepted
	NotificationEventTypeWorkspaceMemberRoleUpdated  = internalnotifications.EventTypeWorkspaceMemberRoleUpdated
	NotificationEventTypeWorkspaceMemberRemoved      = internalnotifications.EventTypeWorkspaceMemberRemoved
	NotificationEventTypeAgentApproved               = internalnotifications.EventTypeAgentApproved
	NotificationEventTypeAgentRejected               = internalnotifications.EventTypeAgentRejected
	NotificationEventTypeAgentRemoved                = internalnotifications.EventTypeAgentRemoved
	NotificationEventTypeAgentOffline                = internalnotifications.EventTypeAgentOffline
	NotificationEventTypePipelineRunSucceeded        = internalnotifications.EventTypePipelineRunSucceeded
	NotificationEventTypePipelineRunFailed           = internalnotifications.EventTypePipelineRunFailed
	NotificationEventTypePipelineRunCancelled        = internalnotifications.EventTypePipelineRunCancelled
	NotificationEventTypeDeploymentRequestCreated    = internalnotifications.EventTypeDeploymentRequestCreated
	NotificationEventTypeDeploymentRequestQueued     = internalnotifications.EventTypeDeploymentRequestQueued
	NotificationEventTypeDeploymentRequestRunning    = internalnotifications.EventTypeDeploymentRequestRunning
	NotificationEventTypeDeploymentRequestSucceeded  = internalnotifications.EventTypeDeploymentRequestSucceeded
	NotificationEventTypeDeploymentRequestFailed     = internalnotifications.EventTypeDeploymentRequestFailed
	NotificationEventTypeDeploymentRequestCancelled  = internalnotifications.EventTypeDeploymentRequestCancelled
)

type NotificationEventInput = internalnotifications.EventInput
type NotificationEmitResult = internalnotifications.EmitResult

type NotificationHandler struct {
	DB *gorm.DB
}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{DB: models.DB}
}

func EmitNotificationEvent(db *gorm.DB, input NotificationEventInput) (NotificationEmitResult, error) {
	return internalnotifications.Emit(db, input)
}

func (h *NotificationHandler) GetInbox(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	userID := c.GetUint64("user_id")
	query := h.DB.Model(&models.InboxMessage{}).Where("recipient_id = ?", userID)
	if workspaceID, ok := parseNotificationWorkspaceFilter(c); ok {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	if c.Query("unread_only") == "true" {
		query = query.Where("is_read = ?", false)
	}
	if family := strings.TrimSpace(c.Query("family")); family != "" {
		query = query.Where("type = ?", family)
	}
	if resourceType := strings.TrimSpace(c.Query("resource_type")); resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if resourceID := strings.TrimSpace(c.Query("resource_id")); resourceID != "" {
		if parsed, err := strconv.ParseUint(resourceID, 10, 64); err == nil {
			query = query.Where("resource_id = ?", parsed)
		}
	}
	var total int64
	query.Count(&total)
	var list []models.InboxMessage
	query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": list, "total": total, "page": page, "size": pageSize}})
}

func (h *NotificationHandler) GetUnreadInboxCount(c *gin.Context) {
	userID := c.GetUint64("user_id")
	query := h.DB.Model(&models.InboxMessage{}).Where("recipient_id = ? AND is_read = ?", userID, false)
	if workspaceID, ok := parseNotificationWorkspaceFilter(c); ok {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	var count int64
	query.Count(&count)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"unread_count": count}})
}

func (h *NotificationHandler) MarkInboxMessageRead(c *gin.Context) {
	userID := c.GetUint64("user_id")
	id := c.Param("id")
	query := h.DB.Model(&models.InboxMessage{}).Where("id = ? AND recipient_id = ?", id, userID)
	if workspaceID, ok := parseNotificationWorkspaceFilter(c); ok {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	result := query.Updates(map[string]interface{}{"is_read": true, "read_at": time.Now().Unix()})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "通知不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "标记成功"})
}

func (h *NotificationHandler) MarkAllInboxMessagesRead(c *gin.Context) {
	userID := c.GetUint64("user_id")
	query := h.DB.Model(&models.InboxMessage{}).Where("recipient_id = ? AND is_read = ?", userID, false)
	if workspaceID, ok := parseNotificationWorkspaceFilter(c); ok {
		query = query.Where("workspace_id = ?", workspaceID)
	}
	query.Updates(map[string]interface{}{"is_read": true, "read_at": time.Now().Unix()})
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "全部已读"})
}

func (h *NotificationHandler) ListPreferences(c *gin.Context) {
	userID := c.GetUint64("user_id")
	query := h.DB.Model(&models.NotificationPreference{}).Where("user_id = ?", userID)
	if workspaceID, ok := parseNotificationWorkspaceFilter(c); ok {
		query = query.Where("workspace_id IS NULL OR workspace_id = ?", workspaceID)
	}
	var list []models.NotificationPreference
	query.Order("family ASC, event_type ASC, channel ASC, created_at ASC, id ASC").Find(&list)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": list, "total": len(list)}})
}

func (h *NotificationHandler) UpsertPreference(c *gin.Context) {
	userID := c.GetUint64("user_id")
	var req struct {
		WorkspaceID  *uint64 `json:"workspace_id"`
		ResourceType string  `json:"resource_type"`
		ResourceID   *uint64 `json:"resource_id"`
		Family       string  `json:"family"`
		EventType    string  `json:"event_type" binding:"required"`
		Channel      string  `json:"channel" binding:"required"`
		Enabled      bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	eventType := strings.TrimSpace(req.EventType)
	if !internalnotifications.IsKnownEventType(eventType) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "未知通知事件类型"})
		return
	}
	family := strings.TrimSpace(req.Family)
	if family == "" {
		family = internalnotifications.FamilyForEventType(eventType)
	}
	if family != internalnotifications.FamilyForEventType(eventType) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "通知事件与分组不匹配"})
		return
	}
	if req.WorkspaceID != nil && !notificationPreferenceWorkspaceAllowed(h.DB, userID, *req.WorkspaceID, c.GetString("role")) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权配置该工作空间通知偏好"})
		return
	}
	ruleKey := notificationPreferenceRuleKey(userID, req.WorkspaceID, req.ResourceType, req.ResourceID, eventType, req.Channel)
	pref := models.NotificationPreference{
		UserID:       userID,
		WorkspaceID:  req.WorkspaceID,
		ResourceType: strings.TrimSpace(req.ResourceType),
		ResourceID:   req.ResourceID,
		Family:       family,
		EventType:    eventType,
		Channel:      strings.TrimSpace(req.Channel),
		Enabled:      req.Enabled,
		RuleKey:      ruleKey,
	}
	var existing models.NotificationPreference
	if err := h.DB.Where("rule_key = ?", ruleKey).First(&existing).Error; err == nil {
		existing.WorkspaceID = pref.WorkspaceID
		existing.ResourceType = pref.ResourceType
		existing.ResourceID = pref.ResourceID
		existing.Family = pref.Family
		existing.EventType = pref.EventType
		existing.Channel = pref.Channel
		existing.Enabled = pref.Enabled
		existing.RuleKey = pref.RuleKey
		if err := h.DB.Save(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新通知偏好失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": existing})
		return
	}
	if err := h.DB.Create(&pref).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建通知偏好失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": pref})
}

func notificationPreferenceRuleKey(userID uint64, workspaceID *uint64, resourceType string, resourceID *uint64, eventType, channel string) string {
	workspaceText := "*"
	if workspaceID != nil {
		workspaceText = strconv.FormatUint(*workspaceID, 10)
	}
	resourceText := "*"
	if resourceID != nil {
		resourceText = strconv.FormatUint(*resourceID, 10)
	}
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		resourceType = "*"
	}
	return strings.Join([]string{strconv.FormatUint(userID, 10), workspaceText, resourceType, resourceText, strings.TrimSpace(eventType), strings.TrimSpace(channel)}, ":")
}

func parseNotificationWorkspaceFilter(c *gin.Context) (uint64, bool) {
	value := strings.TrimSpace(c.Query("workspace_id"))
	if value == "" {
		return 0, false
	}
	workspaceID, err := strconv.ParseUint(value, 10, 64)
	if err != nil || workspaceID == 0 {
		return 0, false
	}
	return workspaceID, true
}

func notificationPreferenceWorkspaceAllowed(db *gorm.DB, userID uint64, workspaceID uint64, role string) bool {
	if db == nil || userID == 0 || workspaceID == 0 {
		return false
	}
	if strings.EqualFold(role, "admin") {
		return true
	}
	var count int64
	db.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ? AND status = ?", workspaceID, userID, models.WorkspaceMemberStatusActive).Count(&count)
	return count > 0
}
