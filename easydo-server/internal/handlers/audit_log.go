package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuditLogHandler struct {
	DB *gorm.DB
}

func NewAuditLogHandler() *AuditLogHandler {
	return &AuditLogHandler{DB: models.DB}
}

func (h *AuditLogHandler) auditLogService() *services.AuditLogService {
	return &services.AuditLogService{DB: h.DB}
}

func (h *AuditLogHandler) ListGlobalAuditLogs(c *gin.Context) {
	result, err := h.auditLogService().ListAuditLogs(c.Request.Context(), h.buildListAuditLogsRequest(c, 0))
	if err != nil {
		writeUserManagementError(c, err, "加载审计日志失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}

func (h *AuditLogHandler) ListWorkspaceAuditLogs(c *gin.Context) {
	workspaceID, ok := parseAuditWorkspaceID(c)
	if !ok {
		return
	}
	result, err := h.auditLogService().ListAuditLogs(c.Request.Context(), h.buildListAuditLogsRequest(c, workspaceID))
	if err != nil {
		writeUserManagementError(c, err, "加载工作区审计日志失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}

func (h *AuditLogHandler) buildListAuditLogsRequest(c *gin.Context, targetWorkspaceID uint64) services.ListAuditLogsRequest {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	targetID, _ := strconv.ParseUint(strings.TrimSpace(c.Query("target_id")), 10, 64)
	return services.ListAuditLogsRequest{
		Actor:             userManagementActor(c),
		WorkspaceID:       c.GetUint64("workspace_id"),
		TargetWorkspaceID: targetWorkspaceID,
		Action:            c.Query("action"),
		TargetType:        c.Query("target_type"),
		TargetID:          targetID,
		Page:              page,
		Limit:             limit,
	}
}

func parseAuditWorkspaceID(c *gin.Context) (uint64, bool) {
	workspaceID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作区ID"})
		return 0, false
	}
	return workspaceID, true
}
