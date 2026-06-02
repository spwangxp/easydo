package handlers

import (
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *UserHandler) resolveCreateUserActorContext(c *gin.Context, actorID uint64, actorSystemRole string) (GovernanceContext, int, string) {
	rawWorkspaceID := strings.TrimSpace(c.GetHeader(middleware.WorkspaceHeaderKey))
	if rawWorkspaceID == "" {
		rawWorkspaceID = strings.TrimSpace(c.Query("workspace_id"))
	}
	if rawWorkspaceID == "" {
		return GovernanceContext{}, http.StatusBadRequest, "必须指定当前工作空间"
	}

	workspaceID, err := strconv.ParseUint(rawWorkspaceID, 10, 64)
	if err != nil || workspaceID == 0 {
		return GovernanceContext{}, http.StatusBadRequest, "无效的工作空间ID"
	}

	var workspace models.Workspace
	if err := h.DB.Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		return GovernanceContext{}, http.StatusForbidden, "无权访问该工作空间"
	}

	workspaceKind := normalizeWorkspaceKind(workspace.Kind)
	if workspaceKind == "" {
		workspaceKind = models.WorkspaceKindNormal
	}

	ctx := GovernanceContext{
		UserID:        actorID,
		SystemRole:    actorSystemRole,
		WorkspaceID:   workspace.ID,
		WorkspaceRole: models.WorkspaceRoleOwner,
		WorkspaceKind: workspaceKind,
	}
	if isAdminRole(actorSystemRole) {
		return ctx, http.StatusOK, ""
	}
	if !middleware.WorkspaceVisibleToSystemRole(actorSystemRole, workspaceKind) {
		return GovernanceContext{}, http.StatusForbidden, "无权访问该工作空间"
	}

	workspaceRole, ok := userWorkspaceRole(h.DB, workspaceID, actorID)
	if !ok {
		return GovernanceContext{}, http.StatusForbidden, "无权访问该工作空间"
	}
	ctx.WorkspaceRole = workspaceRole
	return ctx, http.StatusOK, ""
}

func (h *UserHandler) resolveWorkspaceScopedCreateTarget(req CreateUserRequest, actorCtx GovernanceContext) (string, uint64, string, int, string) {
	if !RequireWorkspaceGovernance(actorCtx) {
		return "", 0, "", http.StatusForbidden, "无权创建用户"
	}
	if strings.TrimSpace(req.SystemRole) != "" && normalizeSystemRole(req.SystemRole) != "user" {
		return "", 0, "", http.StatusForbidden, "无权设置平台管理员角色"
	}
	if req.WorkspaceID != 0 && req.WorkspaceID != actorCtx.WorkspaceID {
		return "", 0, "", http.StatusForbidden, "只能绑定当前工作空间"
	}

	targetWorkspaceRole := models.NormalizeWorkspaceRole(req.WorkspaceRole)
	actorRole := effectiveWorkspaceActorRole(actorCtx.SystemRole, actorCtx.WorkspaceRole)
	if !workspaceRoleEditableBy(actorRole, models.WorkspaceRoleViewer, targetWorkspaceRole) {
		return "", 0, "", http.StatusForbidden, "无权设置该工作空间角色"
	}

	return "user", actorCtx.WorkspaceID, targetWorkspaceRole, http.StatusOK, ""
}

func (h *UserHandler) resolvePlatformScopedCreateTarget(req CreateUserRequest, actorCtx GovernanceContext) (string, uint64, string, int, string) {
	if !RequirePlatformGovernance(actorCtx) {
		return "", 0, "", http.StatusForbidden, "无权创建平台用户"
	}
	if req.WorkspaceID != 0 || strings.TrimSpace(req.WorkspaceRole) != "" {
		return "", 0, "", http.StatusBadRequest, "平台级创建不能绑定工作空间"
	}
	return normalizeSystemRole(req.SystemRole), 0, "", http.StatusOK, ""
}

func normalizeCreateUserEmail(raw string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(parsed.Address) == "" {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
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
	if strings.TrimSpace(req.SystemRole) != "" && !isValidSystemRole(req.SystemRole) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的平台角色"})
		return
	}
	if strings.TrimSpace(req.WorkspaceRole) != "" && !isValidWorkspaceRole(req.WorkspaceRole) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作空间角色"})
		return
	}

	actorSystemRole := c.GetString("role")
	actorID := c.GetUint64("user_id")
	actorCtx, statusCode, message := h.resolveCreateUserActorContext(c, actorID, actorSystemRole)
	if statusCode != http.StatusOK {
		c.JSON(statusCode, gin.H{"code": statusCode, "message": message})
		return
	}

	targetSystemRole := ""
	targetWorkspaceID := uint64(0)
	targetWorkspaceRole := ""
	if actorCtx.WorkspaceKind == models.WorkspaceKindAdmin {
		targetSystemRole, targetWorkspaceID, targetWorkspaceRole, statusCode, message = h.resolvePlatformScopedCreateTarget(req, actorCtx)
	} else {
		targetSystemRole, targetWorkspaceID, targetWorkspaceRole, statusCode, message = h.resolveWorkspaceScopedCreateTarget(req, actorCtx)
	}
	if statusCode != http.StatusOK {
		c.JSON(statusCode, gin.H{"code": statusCode, "message": message})
		return
	}

	normalizedEmail, err := normalizeCreateUserEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "邮箱不能为空或格式不正确"})
		return
	}
	req.Email = normalizedEmail
	var emailCount int64
	if err := h.DB.Model(&models.User{}).Where("LOWER(email) = LOWER(?)", req.Email).Count(&emailCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "校验邮箱失败"})
		return
	}
	if emailCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "邮箱已存在"})
		return
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
		"id":                   createdUser.ID,
		"username":             createdUser.Username,
		"email":                createdUser.Email,
		"nickname":             createdUser.Nickname,
		"system_role":          createdUser.Role,
		"must_change_password": createdUser.MustChangePassword,
	}
	if targetWorkspaceID > 0 {
		response["workspace_id"] = targetWorkspaceID
		response["workspace_role"] = targetWorkspaceRole
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "创建用户成功", "data": response})
}
