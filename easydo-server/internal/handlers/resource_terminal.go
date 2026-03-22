package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TerminalSessionHandler struct {
	DB *gorm.DB
	WS *WebSocketHandler
}

type closeTerminalSessionRequest struct {
	Reason string `json:"reason"`
}

func NewTerminalSessionHandler() *TerminalSessionHandler {
	return &TerminalSessionHandler{DB: models.DB, WS: SharedWebSocketHandler()}
}

func (h *TerminalSessionHandler) CreateResourceTerminalSession(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权创建终端会话"})
		return
	}

	resource, credential, agent, endpoint, err := h.resolveVMTerminalContext(workspaceID, userID, role, c.Param("id"))
	if err != nil {
		h.respondTerminalSessionError(c, err)
		return
	}

	session := models.ResourceTerminalSession{
		SessionID:    uuid.NewString(),
		WorkspaceID:  workspaceID,
		ResourceID:   resource.ID,
		CredentialID: credential.ID,
		AgentID:      agent.ID,
		ResourceType: resource.Type,
		Endpoint:     endpoint,
		Status:       models.ResourceTerminalSessionStatusActive,
		CreatedBy:    userID,
	}
	if err := h.DB.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建终端会话失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildTerminalSessionResponse(session)})
}

func (h *TerminalSessionHandler) ListResourceTerminalSessions(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权查看终端会话"})
		return
	}
	if _, err := h.loadWorkspaceResource(workspaceID, c.Param("id")); err != nil {
		h.respondTerminalSessionError(c, err)
		return
	}

	var sessions []models.ResourceTerminalSession
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, c.Param("id")).Order("created_at DESC, id DESC").Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载终端会话失败"})
		return
	}

	data := make([]gin.H, 0, len(sessions))
	for i := range sessions {
		data = append(data, buildTerminalSessionResponse(sessions[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": data})
}

func (h *TerminalSessionHandler) GetResourceTerminalSession(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权查看终端会话"})
		return
	}

	session, err := h.loadTerminalSession(workspaceID, c.Param("id"), c.Param("session_id"))
	if err != nil {
		h.respondTerminalSessionError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildTerminalSessionResponse(*session)})
}

func (h *TerminalSessionHandler) CloseResourceTerminalSession(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanWriteWorkspaceResource(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权关闭终端会话"})
		return
	}

	session, err := h.loadTerminalSession(workspaceID, c.Param("id"), c.Param("session_id"))
	if err != nil {
		h.respondTerminalSessionError(c, err)
		return
	}

	req := closeTerminalSessionRequest{}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
			return
		}
	}
	reason := defaultIfEmpty(strings.TrimSpace(req.Reason), "user_closed")

	if h.WS != nil {
		closed, closeErr := h.WS.CloseTerminalSession(session.SessionID, reason, userID)
		if closeErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": closeErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildTerminalSessionResponse(*closed)})
		return
	}

	closed, closeErr := closeTerminalSessionRecord(h.DB, session, reason, userID)
	if closeErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": closeErr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": buildTerminalSessionResponse(*closed)})
}

func (h *TerminalSessionHandler) resolveVMTerminalContext(workspaceID, userID uint64, role string, resourceID string) (*models.Resource, *models.Credential, *models.Agent, string, error) {
	resource, err := h.loadWorkspaceResource(workspaceID, resourceID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if resource.Type != models.ResourceTypeVM {
		return nil, nil, nil, "", fmt.Errorf("仅 VM 资源支持终端会话")
	}

	var bindings []models.ResourceCredentialBinding
	if err := h.DB.Where("workspace_id = ? AND resource_id = ?", workspaceID, resource.ID).Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
		return nil, nil, nil, "", fmt.Errorf("加载资源凭据绑定失败")
	}
	binding := preferredResourceCredentialBinding(resource.Type, bindings)
	if binding == nil || binding.CredentialID == 0 {
		return nil, nil, nil, "", fmt.Errorf("资源尚未绑定 SSH 连接凭据")
	}

	credential := models.Credential{}
	if err := h.DB.First(&credential, binding.CredentialID).Error; err != nil {
		return nil, nil, nil, "", fmt.Errorf("资源绑定凭据不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredential(h.DB, &credential, userID, role) {
		return nil, nil, nil, "", fmt.Errorf("无权访问资源绑定凭据")
	}
	if err := validateResourceBindingCredential(resource, &credential); err != nil {
		return nil, nil, nil, "", err
	}

	decrypted, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("连接凭据解密失败: %w", err)
	}
	endpoint := effectiveResourceValidationEndpoint(resource.Type, resource.Endpoint, decrypted)
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil, nil, "", fmt.Errorf("VM 接入地址不能为空")
	}

	agent, err := selectAvailableWorkspaceAgent(h.DB, workspaceID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return resource, &credential, agent, endpoint, nil
}

func (h *TerminalSessionHandler) loadWorkspaceResource(workspaceID uint64, resourceID string) (*models.Resource, error) {
	resource := models.Resource{}
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&resource, resourceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("资源不存在")
		}
		return nil, fmt.Errorf("加载资源失败")
	}
	return &resource, nil
}

func (h *TerminalSessionHandler) loadTerminalSession(workspaceID uint64, resourceID, sessionID string) (*models.ResourceTerminalSession, error) {
	if _, err := h.loadWorkspaceResource(workspaceID, resourceID); err != nil {
		return nil, err
	}
	session := models.ResourceTerminalSession{}
	if err := h.DB.Where("workspace_id = ? AND resource_id = ? AND session_id = ?", workspaceID, resourceID, sessionID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("终端会话不存在")
		}
		return nil, fmt.Errorf("加载终端会话失败")
	}
	return &session, nil
}

func (h *TerminalSessionHandler) respondTerminalSessionError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	switch message {
	case "资源不存在", "终端会话不存在":
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": message})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": message})
	}
}

func buildTerminalSessionResponse(session models.ResourceTerminalSession) gin.H {
	return gin.H{
		"id":                  session.ID,
		"session_id":          session.SessionID,
		"workspace_id":        session.WorkspaceID,
		"resource_id":         session.ResourceID,
		"credential_id":       session.CredentialID,
		"agent_id":            session.AgentID,
		"resource_type":       session.ResourceType,
		"endpoint":            session.Endpoint,
		"status":              session.Status,
		"owner_server_id":     session.OwnerServerID,
		"owner_connection_id": session.OwnerConnectionID,
		"attached_by":         derefOptionalUint64(session.AttachedBy),
		"attached_at":         session.AttachedAt,
		"close_reason":        session.CloseReason,
		"closed_at":           session.ClosedAt,
		"closed_by":           derefOptionalUint64(session.ClosedBy),
		"created_by":          session.CreatedBy,
		"created_at":          session.CreatedAt,
		"updated_at":          session.UpdatedAt,
	}
}

func closeTerminalSessionRecord(db *gorm.DB, session *models.ResourceTerminalSession, reason string, closedBy uint64) (*models.ResourceTerminalSession, error) {
	if db == nil || session == nil {
		return nil, fmt.Errorf("终端会话不存在")
	}
	if session.Status == models.ResourceTerminalSessionStatusClosed {
		return session, nil
	}
	now := time.Now().Unix()
	updates := map[string]interface{}{
		"status":              models.ResourceTerminalSessionStatusClosed,
		"close_reason":        strings.TrimSpace(reason),
		"closed_at":           now,
		"closed_by":           optionalUint64(closedBy),
		"owner_server_id":     "",
		"owner_connection_id": "",
	}
	if err := db.Model(&models.ResourceTerminalSession{}).Where("id = ? AND status = ?", session.ID, models.ResourceTerminalSessionStatusActive).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("关闭终端会话失败")
	}
	if err := db.First(session, session.ID).Error; err != nil {
		return nil, fmt.Errorf("加载终端会话失败")
	}
	return session, nil
}

func derefOptionalUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}
