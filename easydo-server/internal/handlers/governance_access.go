package handlers

import (
	"strings"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"gorm.io/gorm"
)

type GovernanceContext struct {
	UserID        uint64
	SystemRole    string
	WorkspaceID   uint64
	WorkspaceRole string
	WorkspaceKind string
}

func normalizeWorkspaceKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case models.WorkspaceKindAdmin:
		return models.WorkspaceKindAdmin
	case models.WorkspaceKindNormal:
		return models.WorkspaceKindNormal
	default:
		return ""
	}
}

func BuildGovernanceContext(c ContextGetter) GovernanceContext {
	userID, systemRole := getRequestUser(c)
	workspaceID, workspaceRole := getRequestWorkspace(c)
	return GovernanceContext{
		UserID:        userID,
		SystemRole:    systemRole,
		WorkspaceID:   workspaceID,
		WorkspaceRole: workspaceRole,
		WorkspaceKind: normalizeWorkspaceKind(c.GetString("workspace_kind")),
	}
}

func RequirePlatformGovernance(ctx GovernanceContext) bool {
	return isAdminRole(ctx.SystemRole) && ctx.WorkspaceKind == models.WorkspaceKindAdmin
}

func RequireWorkspaceGovernance(ctx GovernanceContext) bool {
	if ctx.WorkspaceKind != models.WorkspaceKindNormal {
		return false
	}
	return ctx.WorkspaceRole == models.WorkspaceRoleOwner || isAdminRole(ctx.SystemRole)
}

func RequireNormalWorkspaceKind(ctx GovernanceContext) bool {
	return ctx.WorkspaceKind == models.WorkspaceKindNormal
}

func workspaceKindByID(db *gorm.DB, workspaceID uint64) string {
	if db == nil || workspaceID == 0 {
		return ""
	}
	var kind string
	db.Model(&models.Workspace{}).Where("id = ?", workspaceID).Pluck("kind", &kind)
	normalized := normalizeWorkspaceKind(kind)
	if normalized == "" {
		return models.WorkspaceKindNormal
	}
	return normalized
}

func governanceContextForWorkspace(db *gorm.DB, workspaceID, userID uint64, systemRole string) GovernanceContext {
	ctx := GovernanceContext{
		UserID:        userID,
		SystemRole:    strings.TrimSpace(systemRole),
		WorkspaceID:   workspaceID,
		WorkspaceRole: "",
		WorkspaceKind: workspaceKindByID(db, workspaceID),
	}
	if role, ok := userWorkspaceRole(db, workspaceID, userID); ok {
		ctx.WorkspaceRole = role
	}
	return ctx
}

func canWriteNormalWorkspaceResource(db *gorm.DB, workspaceID, userID uint64, systemRole string, minRole string) bool {
	ctx := governanceContextForWorkspace(db, workspaceID, userID, systemRole)
	if !RequireNormalWorkspaceKind(ctx) {
		return false
	}
	if isAdminRole(ctx.SystemRole) {
		return true
	}
	return middleware.WorkspaceRoleAtLeast(ctx.WorkspaceRole, minRole)
}

func canGovernWorkspace(db *gorm.DB, workspaceID, userID uint64, systemRole string) bool {
	return RequireWorkspaceGovernance(governanceContextForWorkspace(db, workspaceID, userID, systemRole))
}

func hasWorkspaceMembership(db *gorm.DB, workspaceID, userID uint64, systemRole string, minRole string) bool {
	if isAdminRole(systemRole) {
		return true
	}
	role, ok := userWorkspaceRole(db, workspaceID, userID)
	if !ok {
		return false
	}
	return middleware.WorkspaceRoleAtLeast(role, minRole)
}

func belongsToWorkspaceByModel(db *gorm.DB, model interface{}, resourceID, workspaceID uint64) bool {
	if db == nil || resourceID == 0 || workspaceID == 0 {
		return false
	}
	var count int64
	db.Model(model).Where("id = ? AND workspace_id = ?", resourceID, workspaceID).Count(&count)
	return count > 0
}
