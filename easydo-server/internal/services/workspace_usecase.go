package services

import (
	"context"
	"fmt"
	"strings"

	"easydo-server/internal/models"
	"easydo-server/internal/workspaceauth"

	"gorm.io/gorm"
)

const (
	defaultWorkspacePageLimit = 20
	MaxWorkspacePageLimit     = 100
)

type WorkspaceUseCase struct {
	DB *gorm.DB
}

type ListWorkspacesRequest struct {
	Actor    ActorContext
	Page     int
	Limit    int
	Query    string
	Paginate bool
}

type GetWorkspaceRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
}

type WorkspaceSummary struct {
	ID           uint64   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
	Visibility   string   `json:"visibility"`
	Kind         string   `json:"kind"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

type ListWorkspacesResult struct {
	List               []WorkspaceSummary `json:"list"`
	CurrentWorkspaceID uint64             `json:"current_workspace_id"`
}

type workspaceListRow struct {
	ID          uint64
	Name        string
	Description string
	Status      string
	Visibility  string
	Kind        string
	MemberRole  string
	CreatedAt   int64
}

func (u *WorkspaceUseCase) ListWorkspaces(ctx context.Context, req ListWorkspacesRequest) (ListWorkspacesResult, error) {
	if err := validateWorkspaceUseCaseActor(req.Actor); err != nil {
		return ListWorkspacesResult{}, err
	}
	if u == nil || u.DB == nil {
		return ListWorkspacesResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}

	query := strings.TrimSpace(req.Query)
	dbQuery := u.DB.WithContext(ctx).Model(&models.Workspace{}).
		Where("workspaces.status = ?", models.WorkspaceStatusActive).
		Order("workspaces.created_at ASC")
	if req.Paginate {
		page, limit := normalizeWorkspacePagination(req.Page, req.Limit)
		dbQuery = dbQuery.Limit(limit).Offset((page - 1) * limit)
	}

	if isAdminActor(req.Actor) {
		dbQuery = dbQuery.Select("workspaces.id, workspaces.name, workspaces.description, workspaces.status, workspaces.visibility, workspaces.kind")
	} else {
		visibilityClause, visibilityArgs := workspaceauth.NonAdminVisibleWorkspaceCondition("workspaces")
		dbQuery = dbQuery.
			Select("DISTINCT workspaces.id, workspaces.name, workspaces.description, workspaces.status, workspaces.visibility, workspaces.kind, workspace_members.role AS member_role").
			Joins("JOIN workspace_members ON workspace_members.workspace_id = workspaces.id").
			Where("workspace_members.user_id = ? AND workspace_members.status = ?", req.Actor.UserID, models.WorkspaceMemberStatusActive).
			Where(visibilityClause, visibilityArgs...)
	}
	if query != "" {
		like := "%" + query + "%"
		dbQuery = dbQuery.Where("workspaces.name LIKE ? OR workspaces.slug LIKE ? OR workspaces.description LIKE ?", like, like, like)
	}

	var rows []workspaceListRow
	if err := dbQuery.Find(&rows).Error; err != nil {
		return ListWorkspacesResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list workspaces"}
	}

	result := ListWorkspacesResult{List: make([]WorkspaceSummary, 0, len(rows))}
	for _, row := range rows {
		role := models.WorkspaceRoleOwner
		if !isAdminActor(req.Actor) {
			role = models.NormalizeWorkspaceRole(row.MemberRole)
		}
		result.List = append(result.List, WorkspaceSummary{
			ID:           row.ID,
			Name:         row.Name,
			Description:  row.Description,
			Status:       row.Status,
			Visibility:   row.Visibility,
			Kind:         normalizeWorkspaceKind(row.Kind),
			Role:         role,
			Capabilities: workspaceauth.ExpandWorkspaceCapabilities(role),
		})
	}
	result.CurrentWorkspaceID = selectCurrentWorkspaceID(req.Actor.CurrentWorkspaceID, result.List)
	return result, nil
}

func (u *WorkspaceUseCase) GetWorkspace(ctx context.Context, req GetWorkspaceRequest) (WorkspaceSummary, error) {
	if err := validateWorkspaceUseCaseActor(req.Actor); err != nil {
		return WorkspaceSummary{}, err
	}
	if u == nil || u.DB == nil {
		return WorkspaceSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.WorkspaceID == 0 {
		return WorkspaceSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace_id is required"}
	}

	resolved, err := ResolveWorkspaceForActor(ctx, u.DB, req.Actor, req.WorkspaceID)
	if err != nil {
		return WorkspaceSummary{}, err
	}
	if resolved.Workspace == nil {
		return WorkspaceSummary{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
	}
	role := resolved.Role
	if role == "" {
		role = models.WorkspaceRoleOwner
	}
	return WorkspaceSummary{
		ID:           resolved.Workspace.ID,
		Name:         resolved.Workspace.Name,
		Description:  resolved.Workspace.Description,
		Status:       resolved.Workspace.Status,
		Visibility:   resolved.Workspace.Visibility,
		Kind:         normalizeWorkspaceKind(resolved.Workspace.Kind),
		Role:         role,
		Capabilities: workspaceauth.ExpandWorkspaceCapabilities(role),
	}, nil
}

func validateWorkspaceUseCaseActor(actor ActorContext) error {
	if actor.UserID == 0 {
		return ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	return nil
}

func normalizeWorkspacePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultWorkspacePageLimit
	}
	if limit > MaxWorkspacePageLimit {
		limit = MaxWorkspacePageLimit
	}
	return page, limit
}

func normalizeWorkspaceKind(kind string) string {
	normalized := strings.TrimSpace(strings.ToLower(kind))
	if normalized == models.WorkspaceKindAdmin {
		return models.WorkspaceKindAdmin
	}
	return models.WorkspaceKindNormal
}

func selectCurrentWorkspaceID(currentWorkspaceID uint64, list []WorkspaceSummary) uint64 {
	if currentWorkspaceID != 0 {
		return currentWorkspaceID
	}
	if len(list) == 0 {
		return 0
	}
	return list[0].ID
}

func isAdminActor(actor ActorContext) bool {
	return strings.EqualFold(strings.TrimSpace(actor.SystemRole), "admin")
}

func workspaceListTargetID(result ListWorkspacesResult) string {
	if result.CurrentWorkspaceID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", result.CurrentWorkspaceID)
}
