package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *UserHandler) resolveExplicitCreateUserWorkspace(c *gin.Context, actorID uint64) (uint64, string, int, string) {
	rawWorkspaceID := strings.TrimSpace(c.GetHeader(middleware.WorkspaceHeaderKey))
	if rawWorkspaceID == "" {
		rawWorkspaceID = strings.TrimSpace(c.Query("workspace_id"))
	}
	if rawWorkspaceID == "" {
		return 0, "", http.StatusBadRequest, "必须指定当前工作空间"
	}

	workspaceID, err := strconv.ParseUint(rawWorkspaceID, 10, 64)
	if err != nil || workspaceID == 0 {
		return 0, "", http.StatusBadRequest, "无效的工作空间ID"
	}

	workspaceRole, ok := userWorkspaceRole(h.DB, workspaceID, actorID)
	if !ok {
		return 0, "", http.StatusForbidden, "无权访问该工作空间"
	}

	var workspace models.Workspace
	if err := h.DB.Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		return 0, "", http.StatusForbidden, "无权访问该工作空间"
	}

	return workspaceID, workspaceRole, http.StatusOK, ""
}

func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}

	var existing models.User
	if h.DB.Where("username = ?", req.Username).First(&existing).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "用户名已存在"})
		return
	}

	actorSystemRole := c.GetString("role")
	actorID := c.GetUint64("user_id")
	currentWorkspaceID := uint64(0)
	currentWorkspaceRole := ""
	if !isAdminRole(actorSystemRole) {
		resolvedWorkspaceID, resolvedWorkspaceRole, statusCode, message := h.resolveExplicitCreateUserWorkspace(c, actorID)
		if statusCode != http.StatusOK {
			c.JSON(statusCode, gin.H{"code": statusCode, "message": message})
			return
		}
		currentWorkspaceID = resolvedWorkspaceID
		currentWorkspaceRole = resolvedWorkspaceRole
	}

	allowCreate := isAdminRole(actorSystemRole) || currentWorkspaceRole == models.WorkspaceRoleOwner || currentWorkspaceRole == models.WorkspaceRoleMaintainer
	if !allowCreate {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权创建用户"})
		return
	}

	targetSystemRole := "user"
	targetWorkspaceID := uint64(0)
	targetWorkspaceRole := ""

	if isAdminRole(actorSystemRole) {
		if strings.TrimSpace(req.SystemRole) != "" && !isValidSystemRole(req.SystemRole) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的平台角色"})
			return
		}
		if strings.TrimSpace(req.WorkspaceRole) != "" && !isValidWorkspaceRole(req.WorkspaceRole) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作空间角色"})
			return
		}
		targetSystemRole = normalizeSystemRole(req.SystemRole)
		targetWorkspaceID = req.WorkspaceID
		if targetWorkspaceID > 0 {
			targetWorkspaceRole = models.NormalizeWorkspaceRole(req.WorkspaceRole)
			var workspace models.Workspace
			if err := h.DB.Where("id = ? AND status = ?", targetWorkspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "目标工作空间不存在"})
				return
			}
		} else if strings.TrimSpace(req.WorkspaceRole) != "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "未绑定工作空间时不能指定工作空间角色"})
			return
		}
	} else {
		if strings.TrimSpace(req.SystemRole) != "" && !isValidSystemRole(req.SystemRole) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的平台角色"})
			return
		}
		if strings.TrimSpace(req.WorkspaceRole) != "" && !isValidWorkspaceRole(req.WorkspaceRole) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作空间角色"})
			return
		}
		if strings.TrimSpace(req.SystemRole) != "" && normalizeSystemRole(req.SystemRole) != "user" {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权设置平台管理员角色"})
			return
		}
		if req.WorkspaceID != 0 && req.WorkspaceID != currentWorkspaceID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "只能绑定当前工作空间"})
			return
		}
		targetWorkspaceID = currentWorkspaceID
		targetWorkspaceRole = models.NormalizeWorkspaceRole(req.WorkspaceRole)
		if !workspaceRoleEditableBy(currentWorkspaceRole, models.WorkspaceRoleViewer, targetWorkspaceRole) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权设置该工作空间角色"})
			return
		}
	}

	var createdUser *models.User
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		user, err := h.createUserWithWorkspaceBinding(tx, req, actorID, targetSystemRole, targetWorkspaceID, targetWorkspaceRole)
		if err != nil {
			return err
		}
		createdUser = user
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建用户失败"})
		return
	}

	response := gin.H{
		"id":          createdUser.ID,
		"username":    createdUser.Username,
		"email":       createdUser.Email,
		"nickname":    createdUser.Nickname,
		"system_role": createdUser.Role,
	}
	if targetWorkspaceID > 0 {
		response["workspace_id"] = targetWorkspaceID
		response["workspace_role"] = targetWorkspaceRole
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "创建用户成功", "data": response})
}
