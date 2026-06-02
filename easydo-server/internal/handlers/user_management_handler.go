package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

func (h *UserHandler) userManagementUseCase() *services.UserManagementUseCase {
	return &services.UserManagementUseCase{DB: h.DB}
}

func userManagementActor(c *gin.Context) services.ActorContext {
	return services.ActorContext{
		UserID:             c.GetUint64("user_id"),
		Username:           c.GetString("username"),
		SystemRole:         c.GetString("role"),
		SessionID:          c.GetString("session_id"),
		CurrentWorkspaceID: c.GetUint64("workspace_id"),
	}
}

func (h *UserHandler) GetManagedUser(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	result, err := h.userManagementUseCase().GetUser(c.Request.Context(), services.GetUserRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
	})
	if err != nil {
		writeUserManagementError(c, err, "获取用户详情失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}

func (h *UserHandler) GetManagedUserWorkspaces(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	result, err := h.userManagementUseCase().GetUser(c.Request.Context(), services.GetUserRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
	})
	if err != nil {
		writeUserManagementError(c, err, "获取用户工作区失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": result.WorkspaceAssignments, "total": len(result.WorkspaceAssignments)}})
}

func (h *UserHandler) AddManagedUserToWorkspace(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	var req struct {
		WorkspaceID uint64 `json:"workspace_id"`
		Role        string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	result, err := h.userManagementUseCase().AddUserToWorkspace(c.Request.Context(), services.AddUserToWorkspaceRequest{
		Actor:             userManagementActor(c),
		WorkspaceID:       c.GetUint64("workspace_id"),
		UserID:            userID,
		TargetWorkspaceID: req.WorkspaceID,
		Role:              req.Role,
	})
	if err != nil {
		writeUserManagementError(c, err, "添加用户到工作区失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "添加成功", "data": result})
}

func (h *UserHandler) RemoveManagedUserFromWorkspace(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	workspaceID, err := strconv.ParseUint(strings.TrimSpace(c.Param("workspace_id")), 10, 64)
	if err != nil || workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作区ID"})
		return
	}
	result, err := h.userManagementUseCase().RemoveUserFromWorkspace(c.Request.Context(), services.RemoveUserFromWorkspaceRequest{
		Actor:             userManagementActor(c),
		WorkspaceID:       c.GetUint64("workspace_id"),
		UserID:            userID,
		TargetWorkspaceID: workspaceID,
	})
	if err != nil {
		writeUserManagementError(c, err, "移除用户工作区失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "移除成功", "data": result})
}

func (h *UserHandler) UpdateManagedUser(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	result, err := h.userManagementUseCase().UpdateUser(c.Request.Context(), services.UpdateUserRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
		Email:       req.Email,
		Phone:       req.Phone,
		Nickname:    req.Nickname,
	})
	if err != nil {
		writeUserManagementError(c, err, "更新用户失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": result})
}

func (h *UserHandler) DisableManagedUser(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	result, err := h.userManagementUseCase().DisableUser(c.Request.Context(), services.DisableUserRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
		Reason:      req.Reason,
	})
	if err != nil {
		writeUserManagementError(c, err, "禁用用户失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "禁用成功", "data": result})
}

func (h *UserHandler) EnableManagedUser(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	result, err := h.userManagementUseCase().EnableUser(c.Request.Context(), services.EnableUserRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
	})
	if err != nil {
		writeUserManagementError(c, err, "启用用户失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "启用成功", "data": result})
}

func (h *UserHandler) ResetManagedUserPassword(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	result, err := h.userManagementUseCase().ResetPassword(c.Request.Context(), services.ResetPasswordRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		writeUserManagementError(c, err, "重置密码失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "重置成功", "data": result})
}

func (h *UserHandler) UpdateManagedUserSystemRole(c *gin.Context) {
	userID, ok := parseManagedUserID(c)
	if !ok {
		return
	}
	var req struct {
		SystemRole string `json:"system_role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	result, err := h.userManagementUseCase().UpdateSystemRole(c.Request.Context(), services.UpdateSystemRoleRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: c.GetUint64("workspace_id"),
		UserID:      userID,
		SystemRole:  req.SystemRole,
	})
	if err != nil {
		writeUserManagementError(c, err, "更新平台角色失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": result})
}

func parseManagedUserID(c *gin.Context) (uint64, bool) {
	userID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的用户ID"})
		return 0, false
	}
	return userID, true
}

func writeUserManagementError(c *gin.Context, err error, fallback string) {
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": fallback})
		return
	}
	message := strings.TrimSpace(svcErr.Message)
	if message == "" {
		message = fallback
	}
	switch svcErr.Code {
	case services.ErrorCodeNotFound:
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": message})
	case services.ErrorCodeForbidden:
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": message})
	case services.ErrorCodeInvalidArgument:
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": message})
	case services.ErrorCodeConflict:
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": message})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": fallback})
	}
}
