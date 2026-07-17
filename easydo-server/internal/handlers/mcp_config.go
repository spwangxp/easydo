package handlers

import (
	"errors"
	"net/http"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type MCPConfigHandler struct {
	DB *gorm.DB
}

func NewMCPConfigHandler() *MCPConfigHandler {
	return &MCPConfigHandler{DB: models.DB}
}

func (h *MCPConfigHandler) GetCurrentWorkspaceConfig(c *gin.Context) {
	workspaceID := c.GetUint64("workspace_id")
	if workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "workspace_id is required"})
		return
	}
	actor := services.ActorContext{
		UserID:             c.GetUint64("user_id"),
		Username:           c.GetString("username"),
		SystemRole:         c.GetString("role"),
		SessionID:          c.GetString("session_id"),
		CurrentWorkspaceID: workspaceID,
	}
	config, err := services.NewMCPTokenService(h.DB).GetOrCreateBuiltinToken(c.Request.Context(), actor, workspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{
			"name":         config.Name,
			"token":        config.Token,
			"workspace_id": config.WorkspaceID,
			"user_id":      config.UserID,
		},
	})
}

func writeServiceError(c *gin.Context, err error) {
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		status := http.StatusInternalServerError
		switch svcErr.Code {
		case services.ErrorCodeInvalidArgument:
			status = http.StatusBadRequest
		case services.ErrorCodeUnauthorized:
			status = http.StatusUnauthorized
		case services.ErrorCodeForbidden:
			status = http.StatusForbidden
		case services.ErrorCodeNotFound:
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"code": status, "message": svcErr.Message})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "internal error"})
}
