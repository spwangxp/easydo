package middleware

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

const WorkspaceHeaderKey = "X-Workspace-ID"

var workspaceRoleOrder = map[string]int{
	models.WorkspaceRoleViewer:     10,
	models.WorkspaceRoleDeveloper:  20,
	models.WorkspaceRoleMaintainer: 30,
	models.WorkspaceRoleOwner:      40,
}

func WorkspaceRoleRank(role string) int {
	return workspaceRoleOrder[models.NormalizeWorkspaceRole(role)]
}

func WorkspaceRoleAtLeast(role string, expected string) bool {
	return WorkspaceRoleRank(role) >= WorkspaceRoleRank(expected)
}

func ExpandWorkspaceCapabilities(role string) []string {
	role = models.NormalizeWorkspaceRole(role)
	capSet := map[string]struct{}{
		"workspace.read":        {},
		"workspace.member.read": {},
		"project.read":          {},
		"pipeline.read":         {},
		"pipeline.run.read":     {},
		"agent.read":            {},
		"secret.read":           {},
		"credential.read":       {},
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleDeveloper) {
		for _, capability := range []string{
			"project.write",
			"pipeline.write",
			"pipeline.run",
			"secret.write",
			"secret.value.read",
			"credential.write",
			"credential.value.read",
		} {
			capSet[capability] = struct{}{}
		}
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleMaintainer) {
		for _, capability := range []string{
			"workspace.member.manage",
			"workspace.invitation.manage",
			"agent.write",
			"agent.approve",
			"agent.token.rotate",
		} {
			capSet[capability] = struct{}{}
		}
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleOwner) {
		capSet["workspace.write"] = struct{}{}
		capSet["workspace.delete"] = struct{}{}
	}

	capabilities := make([]string, 0, len(capSet))
	for capability := range capSet {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return capabilities
}

func ResolveUserWorkspace(userID uint64, requestedWorkspaceID uint64) (*models.Workspace, *models.WorkspaceMember, error) {
	if models.DB == nil || userID == 0 {
		return nil, nil, errors.New("invalid workspace lookup context")
	}

	var memberships []models.WorkspaceMember
	query := models.DB.Model(&models.WorkspaceMember{}).
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").
		Where("workspace_members.user_id = ? AND workspace_members.status = ?", userID, models.WorkspaceMemberStatusActive).
		Where("workspaces.status = ?", models.WorkspaceStatusActive).
		Order("workspace_members.created_at ASC")
	if requestedWorkspaceID > 0 {
		query = query.Where("workspace_members.workspace_id = ?", requestedWorkspaceID)
	}
	if err := query.Find(&memberships).Error; err != nil {
		return nil, nil, err
	}
	if len(memberships) == 0 {
		return nil, nil, nil
	}

	member := memberships[0]
	var workspace models.Workspace
	if err := models.DB.Where("id = ? AND status = ?", member.WorkspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		return nil, nil, err
	}

	return &workspace, &member, nil
}

func loadActiveWorkspaceByID(workspaceID uint64) (*models.Workspace, error) {
	if models.DB == nil || workspaceID == 0 {
		return nil, fmt.Errorf("invalid workspace lookup")
	}
	var workspace models.Workspace
	if err := models.DB.Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		return nil, err
	}
	return &workspace, nil
}

func WorkspaceContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetUint64("user_id")
		if userID == 0 {
			c.Next()
			return
		}

		requestedWorkspaceID := uint64(0)
		rawWorkspaceID := strings.TrimSpace(c.GetHeader(WorkspaceHeaderKey))
		if rawWorkspaceID == "" {
			rawWorkspaceID = strings.TrimSpace(c.Query("workspace_id"))
		}
		if rawWorkspaceID != "" {
			parsed, err := strconv.ParseUint(rawWorkspaceID, 10, 64)
			if err != nil {
				c.JSON(400, gin.H{"code": 400, "message": "无效的工作空间ID"})
				c.Abort()
				return
			}
			requestedWorkspaceID = parsed
		}

		role := strings.TrimSpace(c.GetString("role"))
		if strings.EqualFold(role, "admin") {
			if requestedWorkspaceID > 0 {
				workspace, err := loadActiveWorkspaceByID(requestedWorkspaceID)
				if err != nil {
					c.JSON(403, gin.H{"code": 403, "message": "无权访问该工作空间"})
					c.Abort()
					return
				}
				capabilities := ExpandWorkspaceCapabilities(models.WorkspaceRoleOwner)
				c.Set("workspace_id", workspace.ID)
				c.Set("workspace", workspace)
				c.Set("workspace_role", models.WorkspaceRoleOwner)
				c.Set("workspace_member", &models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: userID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive})
				c.Set("capabilities", capabilities)
				c.Writer.Header().Set(WorkspaceHeaderKey, strconv.FormatUint(workspace.ID, 10))
			}
			c.Next()
			return
		}

		workspace, member, err := ResolveUserWorkspace(userID, requestedWorkspaceID)
		if err != nil {
			c.JSON(500, gin.H{"code": 500, "message": "加载工作空间失败"})
			c.Abort()
			return
		}
		if requestedWorkspaceID > 0 && (workspace == nil || member == nil) {
			c.JSON(403, gin.H{"code": 403, "message": "无权访问该工作空间"})
			c.Abort()
			return
		}

		if workspace != nil && member != nil {
			capabilities := ExpandWorkspaceCapabilities(member.Role)
			c.Set("workspace_id", workspace.ID)
			c.Set("workspace", workspace)
			c.Set("workspace_role", models.NormalizeWorkspaceRole(member.Role))
			c.Set("workspace_member", member)
			c.Set("capabilities", capabilities)
			c.Writer.Header().Set(WorkspaceHeaderKey, strconv.FormatUint(workspace.ID, 10))
		}

		c.Next()
	}
}

func WorkspaceRoleRequired(minRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.EqualFold(strings.TrimSpace(c.GetString("role")), "admin") {
			c.Next()
			return
		}
		workspaceRole := c.GetString("workspace_role")
		if workspaceRole == "" || !WorkspaceRoleAtLeast(workspaceRole, minRole) {
			c.JSON(403, gin.H{"code": 403, "message": "工作空间权限不足"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func WorkspaceMemberRequired() gin.HandlerFunc {
	return WorkspaceRoleRequired(models.WorkspaceRoleViewer)
}
