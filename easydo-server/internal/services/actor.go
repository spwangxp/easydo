package services

import (
	"context"
	"errors"
	"strings"

	"easydo-server/internal/models"
	"easydo-server/internal/workspaceauth"

	"gorm.io/gorm"
)

type ActorContext struct {
	UserID             uint64
	Username           string
	SystemRole         string
	SessionID          string
	CurrentWorkspaceID uint64
}

type WorkspaceContext struct {
	Workspace     *models.Workspace
	Member        *models.WorkspaceMember
	Role          string
	Capabilities  []string
	WorkspaceID   uint64
	WorkspaceKind string
}

func ResolveWorkspaceForActor(ctx context.Context, db *gorm.DB, actor ActorContext, workspaceID uint64) (WorkspaceContext, error) {
	if actor.UserID == 0 {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	if workspaceID == 0 {
		return WorkspaceContext{}, nil
	}
	if db == nil {
		return WorkspaceContext{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}

	if strings.EqualFold(strings.TrimSpace(actor.SystemRole), "admin") {
		workspace, err := loadActiveWorkspaceForActor(ctx, db, workspaceID)
		if err != nil {
			return WorkspaceContext{}, err
		}
		role := models.WorkspaceRoleOwner
		return WorkspaceContext{
			Workspace:     workspace,
			Member:        &models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.UserID, Role: role, Status: models.WorkspaceMemberStatusActive},
			Role:          role,
			Capabilities:  workspaceauth.ExpandWorkspaceCapabilities(role),
			WorkspaceID:   workspace.ID,
			WorkspaceKind: workspace.Kind,
		}, nil
	}

	workspace, member, err := loadVisibleMemberWorkspaceForActor(ctx, db, actor.UserID, workspaceID)
	if err != nil {
		return WorkspaceContext{}, err
	}
	role := models.NormalizeWorkspaceRole(member.Role)
	member.Role = role
	return WorkspaceContext{
		Workspace:     workspace,
		Member:        member,
		Role:          role,
		Capabilities:  workspaceauth.ExpandWorkspaceCapabilities(role),
		WorkspaceID:   workspace.ID,
		WorkspaceKind: workspace.Kind,
	}, nil
}

func loadActiveWorkspaceForActor(ctx context.Context, db *gorm.DB, workspaceID uint64) (*models.Workspace, error) {
	var workspace models.Workspace
	err := db.WithContext(ctx).Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
		}
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace"}
	}
	if strings.TrimSpace(workspace.Kind) == "" {
		workspace.Kind = models.WorkspaceKindNormal
	}
	return &workspace, nil
}

func loadVisibleMemberWorkspaceForActor(ctx context.Context, db *gorm.DB, userID uint64, workspaceID uint64) (*models.Workspace, *models.WorkspaceMember, error) {
	var member models.WorkspaceMember
	query := db.WithContext(ctx).Model(&models.WorkspaceMember{}).
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").
		Where("workspace_members.user_id = ? AND workspace_members.workspace_id = ? AND workspace_members.status = ?", userID, workspaceID, models.WorkspaceMemberStatusActive).
		Where("workspaces.status = ?", models.WorkspaceStatusActive)
	visibilityClause, visibilityArgs := workspaceauth.NonAdminVisibleWorkspaceCondition("workspaces")
	query = query.Where(visibilityClause, visibilityArgs...)
	if err := query.First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
		}
		return nil, nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace membership"}
	}

	var workspace models.Workspace
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", member.WorkspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
		}
		return nil, nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace"}
	}
	if !workspaceauth.WorkspaceVisibleToSystemRole("", workspace.Kind) {
		return nil, nil, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
	}
	if strings.TrimSpace(workspace.Kind) == "" {
		workspace.Kind = models.WorkspaceKindNormal
	}
	return &workspace, &member, nil
}
