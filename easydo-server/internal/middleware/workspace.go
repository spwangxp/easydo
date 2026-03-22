package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"github.com/gin-gonic/gin"
)

const WorkspaceHeaderKey = "X-Workspace-ID"

const workspaceAuthCacheTTL = time.Minute

type cachedWorkspaceResolution struct {
	Workspace models.Workspace       `json:"workspace"`
	Member    models.WorkspaceMember `json:"member"`
	Version   int64                  `json:"version"`
}

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
		"credential.read":       {},
		"resource.read":         {},
		"resource.monitor.read": {},
		"store.template.read":   {},
	}

	if WorkspaceRoleAtLeast(role, models.WorkspaceRoleDeveloper) {
		for _, capability := range []string{
			"project.write",
			"pipeline.write",
			"pipeline.run",
			"credential.write",
			"credential.value.read",
			"resource.use",
			"store.template.use",
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
			"resource.write",
			"resource.operate",
			"resource.monitor.write",
			"store.template.manage",
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
	if cachedWorkspace, cachedMember, ok := getCachedWorkspaceResolution(context.Background(), userID, requestedWorkspaceID); ok {
		return cachedWorkspace, cachedMember, nil
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
	cacheWorkspaceResolution(context.Background(), userID, requestedWorkspaceID, &workspace, &member)

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

func workspaceAuthCacheKey(userID uint64, requestedWorkspaceID uint64) string {
	return fmt.Sprintf("easydo:workspace:auth:%d:%d", userID, requestedWorkspaceID)
}

func workspaceAuthVersionKey(workspaceID uint64) string {
	return fmt.Sprintf("easydo:workspace:auth-version:%d", workspaceID)
}

func getWorkspaceAuthVersion(ctx context.Context, workspaceID uint64) int64 {
	if utils.RedisClient == nil || workspaceID == 0 {
		return 1
	}
	version, err := utils.RedisClient.Get(ctx, workspaceAuthVersionKey(workspaceID)).Int64()
	if err == nil && version > 0 {
		return version
	}
	if err := utils.RedisClient.Set(ctx, workspaceAuthVersionKey(workspaceID), 1, 0).Err(); err != nil {
		return 1
	}
	return 1
}

func BumpWorkspaceAuthVersion(ctx context.Context, workspaceID uint64) error {
	if utils.RedisClient == nil || workspaceID == 0 {
		return nil
	}
	_, err := utils.RedisClient.Incr(ctx, workspaceAuthVersionKey(workspaceID)).Result()
	return err
}

func getCachedWorkspaceResolution(ctx context.Context, userID uint64, requestedWorkspaceID uint64) (*models.Workspace, *models.WorkspaceMember, bool) {
	if utils.RedisClient == nil || userID == 0 {
		return nil, nil, false
	}
	raw, err := utils.RedisClient.Get(ctx, workspaceAuthCacheKey(userID, requestedWorkspaceID)).Result()
	if err != nil || raw == "" {
		return nil, nil, false
	}
	var cached cachedWorkspaceResolution
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return nil, nil, false
	}
	if cached.Workspace.ID == 0 || cached.Member.WorkspaceID == 0 {
		return nil, nil, false
	}
	if getWorkspaceAuthVersion(ctx, cached.Workspace.ID) != cached.Version {
		return nil, nil, false
	}
	workspace := cached.Workspace
	member := cached.Member
	return &workspace, &member, true
}

func cacheWorkspaceResolution(ctx context.Context, userID uint64, requestedWorkspaceID uint64, workspace *models.Workspace, member *models.WorkspaceMember) {
	if utils.RedisClient == nil || workspace == nil || member == nil || userID == 0 {
		return
	}
	payload, err := json.Marshal(cachedWorkspaceResolution{
		Workspace: *workspace,
		Member:    *member,
		Version:   getWorkspaceAuthVersion(ctx, workspace.ID),
	})
	if err != nil {
		return
	}
	_ = utils.RedisClient.Set(ctx, workspaceAuthCacheKey(userID, requestedWorkspaceID), payload, workspaceAuthCacheTTL).Err()
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
