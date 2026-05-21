package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WorkspaceHandler struct {
	DB *gorm.DB
}

func NewWorkspaceHandler() *WorkspaceHandler {
	return &WorkspaceHandler{DB: models.DB}
}

var (
	errWorkspaceMemberNotFound      = errors.New("workspace member not found")
	errWorkspaceMemberRoleForbidden = errors.New("workspace member role change forbidden")
	errWorkspaceLastOwnerDenied     = errors.New("workspace must retain at least one owner")
)

func workspaceRoleEditableBy(actorRole string, targetRole string, newRole string) bool {
	actorRole = models.NormalizeWorkspaceRole(actorRole)
	targetRole = models.NormalizeWorkspaceRole(targetRole)
	newRole = models.NormalizeWorkspaceRole(newRole)
	if actorRole == models.WorkspaceRoleOwner {
		return true
	}
	if actorRole != models.WorkspaceRoleMaintainer {
		return false
	}
	if targetRole == models.WorkspaceRoleOwner || targetRole == models.WorkspaceRoleMaintainer {
		return false
	}
	return newRole == models.WorkspaceRoleViewer || newRole == models.WorkspaceRoleDeveloper
}

func effectiveWorkspaceActorRole(systemRole string, workspaceRole string) string {
	if isAdminRole(systemRole) {
		return models.WorkspaceRoleOwner
	}
	return models.NormalizeWorkspaceRole(workspaceRole)
}

func activeWorkspaceOwnerCount(db *gorm.DB, workspaceID uint64) (int64, error) {
	var owners []models.WorkspaceMember
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND status = ? AND role = ?", workspaceID, models.WorkspaceMemberStatusActive, models.WorkspaceRoleOwner).
		Find(&owners).Error
	return int64(len(owners)), err
}

func ensureWorkspaceRetainsOwner(db *gorm.DB, workspaceID uint64, member *models.WorkspaceMember, nextRole string) error {
	if member == nil {
		return errWorkspaceMemberNotFound
	}
	if models.NormalizeWorkspaceRole(member.Role) != models.WorkspaceRoleOwner {
		return nil
	}
	if models.NormalizeWorkspaceRole(nextRole) == models.WorkspaceRoleOwner {
		return nil
	}
	ownerCount, err := activeWorkspaceOwnerCount(db, workspaceID)
	if err != nil {
		return err
	}
	if ownerCount <= 1 {
		return errWorkspaceLastOwnerDenied
	}
	return nil
}

func inviterCanGrantWorkspaceRoleNow(db *gorm.DB, invitation *models.WorkspaceInvitation) bool {
	if db == nil || invitation == nil {
		return false
	}
	var inviter models.User
	if err := db.First(&inviter, invitation.InvitedBy).Error; err != nil {
		return false
	}
	if strings.TrimSpace(strings.ToLower(inviter.Status)) != "active" {
		return false
	}
	ctx := governanceContextForWorkspace(db, invitation.WorkspaceID, inviter.ID, inviter.Role)
	if !RequireWorkspaceGovernance(ctx) {
		return false
	}
	return workspaceRoleEditableBy(effectiveWorkspaceActorRole(ctx.SystemRole, ctx.WorkspaceRole), models.WorkspaceRoleViewer, invitation.Role)
}

func generateInviteToken() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func (h *WorkspaceHandler) getWorkspaceForUser(c *gin.Context, workspaceID uint64) (*models.Workspace, string, bool) {
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	if isAdminRole(role) {
		var workspace models.Workspace
		if err := h.DB.Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
			return nil, "", false
		}
		return &workspace, models.WorkspaceRoleOwner, true
	}
	workspaceRole, ok := userWorkspaceRole(h.DB, workspaceID, userID)
	if !ok {
		return nil, "", false
	}
	var workspace models.Workspace
	if err := h.DB.Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		return nil, "", false
	}
	if !middleware.WorkspaceVisibleToSystemRole(role, workspace.Kind) {
		return nil, "", false
	}
	return &workspace, workspaceRole, true
}

func (h *WorkspaceHandler) GetWorkspaceList(c *gin.Context) {
	userID := c.GetUint64("user_id")
	role := c.GetString("role")
	query := h.DB.Model(&models.Workspace{}).Where("status = ?", models.WorkspaceStatusActive).Order("created_at ASC")
	if !isAdminRole(role) {
		workspaceSubQuery := h.DB.Model(&models.WorkspaceMember{}).
			Select("workspace_id").
			Where("user_id = ? AND status = ?", userID, models.WorkspaceMemberStatusActive)
		visibilityClause, visibilityArgs := middleware.NonAdminVisibleWorkspaceCondition("workspaces")
		query = query.Where("id IN (?)", workspaceSubQuery).Where(visibilityClause, visibilityArgs...)
	}

	var workspaces []models.Workspace
	if err := query.Find(&workspaces).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取工作空间失败"})
		return
	}

	result := make([]gin.H, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspaceRole := models.WorkspaceRoleOwner
		if !isAdminRole(role) {
			workspaceRole, _ = userWorkspaceRole(h.DB, workspace.ID, userID)
		}
		workspaceKind := normalizeWorkspaceKind(workspace.Kind)
		if workspaceKind == "" {
			workspaceKind = models.WorkspaceKindNormal
		}
		result = append(result, gin.H{
			"id":           workspace.ID,
			"name":         workspace.Name,
			"description":  workspace.Description,
			"status":       workspace.Status,
			"visibility":   workspace.Visibility,
			"kind":         workspaceKind,
			"role":         workspaceRole,
			"capabilities": middleware.ExpandWorkspaceCapabilities(workspaceRole),
		})
	}

	currentWorkspaceID := c.GetUint64("workspace_id")
	if currentWorkspaceID == 0 && len(result) > 0 {
		currentWorkspaceID = result[0]["id"].(uint64)
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": result, "current_workspace_id": currentWorkspaceID}})
}

func (h *WorkspaceHandler) CreateWorkspace(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required,min=2,max=128"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	if !isValidWorkspaceName(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "工作区名称只允许英文字母和数字"})
		return
	}
	userID := c.GetUint64("user_id")
	workspace := models.Workspace{
		Name:        req.Name,
		Slug:        req.Name,
		Description: req.Description,
		Status:      models.WorkspaceStatusActive,
		Visibility:  models.WorkspaceVisibilityPrivate,
		CreatedBy:   userID,
	}
	if err := h.DB.Create(&workspace).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建工作空间失败: " + err.Error()})
		return
	}
	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      userID,
		Role:        models.WorkspaceRoleOwner,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   userID,
		JoinedAt:    time.Now().Unix(),
	}
	if err := h.DB.Create(&member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建工作空间成员失败: " + err.Error()})
		return
	}
	_ = middleware.BumpWorkspaceAuthVersion(c.Request.Context(), workspace.ID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{
		"id":          workspace.ID,
		"name":        workspace.Name,
		"description": workspace.Description,
		"status":      workspace.Status,
		"visibility":  workspace.Visibility,
		"kind":        workspace.Kind,
		"created_by":  workspace.CreatedBy,
	}})
}

func (h *WorkspaceHandler) GetWorkspace(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspace, workspaceRole, ok := h.getWorkspaceForUser(c, workspaceID)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权访问该工作空间"})
		return
	}
	workspaceKind := normalizeWorkspaceKind(workspace.Kind)
	if workspaceKind == "" {
		workspaceKind = models.WorkspaceKindNormal
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{
		"id":           workspace.ID,
		"name":         workspace.Name,
		"description":  workspace.Description,
		"status":       workspace.Status,
		"visibility":   workspace.Visibility,
		"kind":         workspaceKind,
		"role":         workspaceRole,
		"capabilities": middleware.ExpandWorkspaceCapabilities(workspaceRole),
	}})
}

func (h *WorkspaceHandler) UpdateWorkspace(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspace, _, ok := h.getWorkspaceForUser(c, workspaceID)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权修改该工作空间"})
		return
	}
	governanceCtx := governanceContextForWorkspace(h.DB, workspaceID, c.GetUint64("user_id"), c.GetString("role"))
	if !RequireWorkspaceGovernance(governanceCtx) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权修改该工作空间"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	updates := map[string]any{}
	if req.Name != "" {
		if !isValidWorkspaceName(req.Name) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "工作区名称只允许英文字母和数字"})
			return
		}
		updates["name"] = req.Name
		updates["slug"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Status == models.WorkspaceStatusActive || req.Status == models.WorkspaceStatusArchived {
		updates["status"] = req.Status
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "没有需要更新的字段"})
		return
	}
	if err := h.DB.Model(workspace).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新工作空间失败"})
		return
	}
	_ = middleware.BumpWorkspaceAuthVersion(c.Request.Context(), workspaceID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功"})
}

func (h *WorkspaceHandler) ListMembers(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if _, _, ok := h.getWorkspaceForUser(c, workspaceID); !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权访问该工作空间"})
		return
	}
	viewerIsPlatformAdmin := isAdminRole(c.GetString("role"))
	var members []models.WorkspaceMember
	if err := h.DB.Preload("User").Where("workspace_id = ?", workspaceID).Order("created_at ASC").Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取成员失败"})
		return
	}
	result := make([]gin.H, 0, len(members))
	for _, member := range members {
		if !viewerIsPlatformAdmin && isAdminRole(member.User.Role) {
			continue
		}
		result = append(result, gin.H{
			"id":          member.ID,
			"user_id":     member.UserID,
			"role":        models.NormalizeWorkspaceRole(member.Role),
			"status":      member.Status,
			"joined_at":   member.JoinedAt,
			"username":    member.User.Username,
			"nickname":    member.User.Nickname,
			"email":       member.User.Email,
			"system_role": member.User.Role,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": result, "total": len(result)}})
}

func (h *WorkspaceHandler) UpdateMember(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if _, _, ok := h.getWorkspaceForUser(c, workspaceID); !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权管理成员"})
		return
	}
	governanceCtx := governanceContextForWorkspace(h.DB, workspaceID, c.GetUint64("user_id"), c.GetString("role"))
	if !RequireWorkspaceGovernance(governanceCtx) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权管理成员"})
		return
	}
	memberID, _ := strconv.ParseUint(c.Param("member_id"), 10, 64)
	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	newRole := models.NormalizeWorkspaceRole(req.Role)
	actorRole := effectiveWorkspaceActorRole(governanceCtx.SystemRole, governanceCtx.WorkspaceRole)

	var member models.WorkspaceMember
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("workspace_id = ? AND id = ?", workspaceID, memberID).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errWorkspaceMemberNotFound
			}
			return err
		}
		if !workspaceRoleEditableBy(actorRole, member.Role, newRole) {
			return errWorkspaceMemberRoleForbidden
		}
		if err := ensureWorkspaceRetainsOwner(tx, workspaceID, &member, newRole); err != nil {
			return err
		}
		if err := tx.Model(&member).Update("role", newRole).Error; err != nil {
			return err
		}
		member.Role = newRole
		return nil
	}); err != nil {
		switch {
		case errors.Is(err, errWorkspaceMemberNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "成员不存在"})
		case errors.Is(err, errWorkspaceMemberRoleForbidden):
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权调整该成员角色"})
		case errors.Is(err, errWorkspaceLastOwnerDenied):
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "至少保留一个 owner"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新成员角色失败"})
		}
		return
	}
	emitWorkspaceMemberRoleUpdatedNotification(h.DB, workspaceID, &member, c.GetUint64("user_id"))
	_ = middleware.BumpWorkspaceAuthVersion(c.Request.Context(), workspaceID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功"})
}

func (h *WorkspaceHandler) RemoveMember(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if _, _, ok := h.getWorkspaceForUser(c, workspaceID); !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权移除成员"})
		return
	}
	governanceCtx := governanceContextForWorkspace(h.DB, workspaceID, c.GetUint64("user_id"), c.GetString("role"))
	if !RequireWorkspaceGovernance(governanceCtx) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权移除成员"})
		return
	}
	memberID, _ := strconv.ParseUint(c.Param("member_id"), 10, 64)
	actorRole := effectiveWorkspaceActorRole(governanceCtx.SystemRole, governanceCtx.WorkspaceRole)

	var member models.WorkspaceMember
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("workspace_id = ? AND id = ?", workspaceID, memberID).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errWorkspaceMemberNotFound
			}
			return err
		}
		if !workspaceRoleEditableBy(actorRole, member.Role, models.WorkspaceRoleViewer) {
			return errWorkspaceMemberRoleForbidden
		}
		if err := ensureWorkspaceRetainsOwner(tx, workspaceID, &member, ""); err != nil {
			return err
		}
		if err := tx.Delete(&member).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		switch {
		case errors.Is(err, errWorkspaceMemberNotFound):
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "成员不存在"})
		case errors.Is(err, errWorkspaceMemberRoleForbidden):
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权移除该成员"})
		case errors.Is(err, errWorkspaceLastOwnerDenied):
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "至少保留一个 owner"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "移除成员失败"})
		}
		return
	}
	emitWorkspaceMemberRemovedNotification(h.DB, workspaceID, &member, c.GetUint64("user_id"))
	_ = middleware.BumpWorkspaceAuthVersion(c.Request.Context(), workspaceID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "移除成功"})
}

func (h *WorkspaceHandler) ListInvitations(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	_, actorRole, ok := h.getWorkspaceForUser(c, workspaceID)
	if !ok || !middleware.WorkspaceRoleAtLeast(actorRole, models.WorkspaceRoleMaintainer) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权查看邀请"})
		return
	}
	var invitations []models.WorkspaceInvitation
	if err := h.DB.Where("workspace_id = ?", workspaceID).Order("created_at DESC").Find(&invitations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取邀请失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"list": invitations, "total": len(invitations)}})
}

func (h *WorkspaceHandler) CreateInvitation(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	workspace, _, ok := h.getWorkspaceForUser(c, workspaceID)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权邀请成员"})
		return
	}
	governanceCtx := governanceContextForWorkspace(h.DB, workspaceID, c.GetUint64("user_id"), c.GetString("role"))
	if !RequireWorkspaceGovernance(governanceCtx) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权邀请成员"})
		return
	}
	actorRole := effectiveWorkspaceActorRole(governanceCtx.SystemRole, governanceCtx.WorkspaceRole)
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Role  string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	inviteRole := models.NormalizeWorkspaceRole(req.Role)
	if !workspaceRoleEditableBy(actorRole, models.WorkspaceRoleViewer, inviteRole) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权邀请该角色"})
		return
	}
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成邀请失败"})
		return
	}
	actorID := c.GetUint64("user_id")
	invitation := models.WorkspaceInvitation{
		WorkspaceID: workspaceID,
		Email:       strings.ToLower(strings.TrimSpace(req.Email)),
		Role:        inviteRole,
		TokenHash:   tokenHash,
		Status:      models.WorkspaceInvitationStatusPending,
		InvitedBy:   actorID,
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	var user models.User
	if err := h.DB.Where("LOWER(email) = ?", invitation.Email).First(&user).Error; err == nil {
		invitation.InvitedUserID = &user.ID
	}
	if err := h.DB.Create(&invitation).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建邀请失败"})
		return
	}
	emitWorkspaceInvitationCreatedNotification(h.DB, workspace, &invitation, actorID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"id": invitation.ID, "token": token, "expires_at": invitation.ExpiresAt}})
}

func (h *WorkspaceHandler) RevokeInvitation(c *gin.Context) {
	workspaceID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if _, _, ok := h.getWorkspaceForUser(c, workspaceID); !ok {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权撤销邀请"})
		return
	}
	governanceCtx := governanceContextForWorkspace(h.DB, workspaceID, c.GetUint64("user_id"), c.GetString("role"))
	if !RequireWorkspaceGovernance(governanceCtx) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权撤销邀请"})
		return
	}
	inviteID, _ := strconv.ParseUint(c.Param("invite_id"), 10, 64)
	if err := h.DB.Model(&models.WorkspaceInvitation{}).
		Where("id = ? AND workspace_id = ?", inviteID, workspaceID).
		Updates(map[string]any{"status": models.WorkspaceInvitationStatusRevoked}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "撤销邀请失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "撤销成功"})
}

func (h *WorkspaceHandler) AcceptInvitation(c *gin.Context) {
	userID := c.GetUint64("user_id")
	var invitation models.WorkspaceInvitation
	lookup := strings.TrimSpace(c.Param("token"))
	if invitationID, err := strconv.ParseUint(lookup, 10, 64); err == nil && invitationID > 0 {
		if err := h.DB.First(&invitation, invitationID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "邀请不存在"})
			return
		}
	} else {
		tokenHash := sha256.Sum256([]byte(lookup))
		if err := h.DB.Where("token_hash = ?", hex.EncodeToString(tokenHash[:])).First(&invitation).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "邀请不存在"})
			return
		}
	}
	if invitation.Status != models.WorkspaceInvitationStatusPending || invitation.ExpiresAt < time.Now().Unix() {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "邀请已失效"})
		return
	}
	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}
	if invitation.InvitedUserID != nil && *invitation.InvitedUserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "当前用户不匹配该邀请"})
		return
	}
	if invitation.InvitedUserID == nil && strings.TrimSpace(strings.ToLower(user.Email)) != invitation.Email {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "当前用户邮箱不匹配该邀请"})
		return
	}
	if !inviterCanGrantWorkspaceRoleNow(h.DB, &invitation) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "邀请已失效"})
		return
	}
	var member models.WorkspaceMember
	if err := h.DB.Where("workspace_id = ? AND user_id = ?", invitation.WorkspaceID, user.ID).First(&member).Error; err == nil {
		acceptedByUser := user.ID
		h.DB.Model(&invitation).Updates(map[string]any{
			"status":           models.WorkspaceInvitationStatusAccepted,
			"accepted_at":      time.Now().Unix(),
			"accepted_by_user": acceptedByUser,
		})
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "你已经是该工作空间成员"})
		return
	}
	member = models.WorkspaceMember{
		WorkspaceID: invitation.WorkspaceID,
		UserID:      user.ID,
		Role:        invitation.Role,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   invitation.InvitedBy,
		JoinedAt:    time.Now().Unix(),
	}
	if err := h.DB.Create(&member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "接受邀请失败"})
		return
	}
	_ = middleware.BumpWorkspaceAuthVersion(c.Request.Context(), invitation.WorkspaceID)
	acceptedByUser := user.ID
	if err := h.DB.Model(&invitation).Updates(map[string]any{
		"status":           models.WorkspaceInvitationStatusAccepted,
		"accepted_at":      time.Now().Unix(),
		"accepted_by_user": acceptedByUser,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "接受邀请失败"})
		return
	}
	var workspace models.Workspace
	if err := h.DB.First(&workspace, invitation.WorkspaceID).Error; err == nil {
		emitWorkspaceInvitationAcceptedNotification(h.DB, &workspace, &invitation, &user)
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "加入工作空间成功", "data": gin.H{"workspace_id": invitation.WorkspaceID}})
}
