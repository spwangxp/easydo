package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

func (h *WorkspaceHandler) AddExistingMember(c *gin.Context) {
	workspaceID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作区ID"})
		return
	}
	var req struct {
		UserID uint64 `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	usecase := services.UserManagementUseCase{DB: h.DB}
	result, err := usecase.AddUserToWorkspace(c.Request.Context(), services.AddUserToWorkspaceRequest{
		Actor:             userManagementActor(c),
		WorkspaceID:       workspaceID,
		UserID:            req.UserID,
		TargetWorkspaceID: workspaceID,
		Role:              req.Role,
	})
	if err != nil {
		writeUserManagementError(c, err, "添加成员失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "添加成功", "data": result})
}

func (h *WorkspaceHandler) SearchMemberCandidates(c *gin.Context) {
	workspaceID, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || workspaceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的工作区ID"})
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "20")))
	usecase := services.UserManagementUseCase{DB: h.DB}
	result, err := usecase.SearchWorkspaceMemberCandidates(c.Request.Context(), services.SearchWorkspaceMemberCandidatesRequest{
		Actor:       userManagementActor(c),
		WorkspaceID: workspaceID,
		Query:       c.Query("q"),
		Limit:       limit,
	})
	if err != nil {
		writeUserManagementError(c, err, "搜索用户失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
}
