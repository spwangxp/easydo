package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"

	"gorm.io/gorm"
)

const (
	userManagementScopePlatform  = "platform"
	userManagementScopeWorkspace = "workspace"
)

type UserManagementUseCase struct {
	DB *gorm.DB
}

type ListUsersRequest struct {
	Actor             ActorContext
	WorkspaceID       uint64
	Query             string
	Role              string
	Status            string
	WorkspaceFilterID uint64
	Page              int
	Limit             int
}

type GetUserRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
}

type UpdateUserRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
	Email       string
	Phone       string
	Nickname    string
}

type DisableUserRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
	Reason      string
}

type EnableUserRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
}

type ResetPasswordRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
	NewPassword string
}

type UpdateSystemRoleRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	UserID      uint64
	SystemRole  string
}

type AddUserToWorkspaceRequest struct {
	Actor             ActorContext
	WorkspaceID       uint64
	UserID            uint64
	TargetWorkspaceID uint64
	Role              string
}

type SearchWorkspaceMemberCandidatesRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	Query       string
	Limit       int
}

type RemoveUserFromWorkspaceRequest struct {
	Actor             ActorContext
	WorkspaceID       uint64
	UserID            uint64
	TargetWorkspaceID uint64
}

type UserSummary struct {
	ID                   uint64                       `json:"id"`
	Username             string                       `json:"username"`
	Email                string                       `json:"email"`
	Phone                string                       `json:"phone"`
	Nickname             string                       `json:"nickname"`
	SystemRole           string                       `json:"system_role"`
	Status               string                       `json:"status"`
	MustChangePassword   bool                         `json:"must_change_password"`
	LastLoginAt          int64                        `json:"last_login_at"`
	DisabledAt           int64                        `json:"disabled_at"`
	WorkspaceAssignments []WorkspaceAssignmentSummary `json:"workspace_assignments,omitempty"`
}

type UserListResult struct {
	List  []UserSummary `json:"list"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}

type UserDetail struct {
	UserSummary
	WorkspaceAssignments   []WorkspaceAssignmentSummary `json:"workspace_assignments"`
	CurrentWorkspaceMember *WorkspaceMemberSummary      `json:"current_workspace_member,omitempty"`
}

type WorkspaceAssignmentSummary struct {
	WorkspaceID   uint64 `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	WorkspaceKind string `json:"workspace_kind"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	JoinedAt      int64  `json:"joined_at"`
}

type WorkspaceMemberSummary struct {
	WorkspaceID uint64 `json:"workspace_id"`
	UserID      uint64 `json:"user_id"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	JoinedAt    int64  `json:"joined_at"`
}

type WorkspaceMemberCandidateSummary struct {
	ID         uint64 `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Nickname   string `json:"nickname"`
	SystemRole string `json:"system_role"`
	Status     string `json:"status"`
}

type WorkspaceMemberCandidateListResult struct {
	List  []WorkspaceMemberCandidateSummary `json:"list"`
	Total int64                             `json:"total"`
	Limit int                               `json:"limit"`
}

type userManagementScope struct {
	Kind        string
	WorkspaceID uint64
}

func (u *UserManagementUseCase) ListUsers(ctx context.Context, req ListUsersRequest) (UserListResult, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserListResult{}, err
	}
	scope, err := resolveUserManagementScope(ctx, db, req.Actor, req.WorkspaceID)
	if err != nil {
		return UserListResult{}, err
	}
	if scope.Kind != userManagementScopePlatform {
		return UserListResult{}, ServiceError{Code: ErrorCodeForbidden, Message: "platform governance is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	page, limit := normalizePagination(req.Page, req.Limit)
	query := db.WithContext(ctx).Model(&models.User{})
	query = applyUserManagementFilters(query, req)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return UserListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count users"}
	}

	var users []models.User
	if err := query.Order("users.id ASC").Offset((page - 1) * limit).Limit(limit).Find(&users).Error; err != nil {
		return UserListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list users"}
	}

	result := UserListResult{List: make([]UserSummary, 0, len(users)), Total: total, Page: page, Limit: limit}
	for _, user := range users {
		result.List = append(result.List, buildUserSummary(user))
	}
	if err := attachWorkspaceAssignmentsToUserSummaries(ctx, db, result.List); err != nil {
		return UserListResult{}, err
	}
	return result, nil
}

func (u *UserManagementUseCase) GetUser(ctx context.Context, req GetUserRequest) (UserDetail, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserDetail{}, err
	}
	if req.UserID == 0 {
		return UserDetail{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	scope, err := resolveUserManagementScope(ctx, db, req.Actor, req.WorkspaceID)
	if err != nil {
		return UserDetail{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var user models.User
	if err := db.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UserDetail{}, ServiceError{Code: ErrorCodeNotFound, Message: "user not found"}
		}
		return UserDetail{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load user"}
	}

	detail := UserDetail{UserSummary: buildUserSummary(user)}
	if scope.Kind == userManagementScopePlatform {
		assignments, err := listUserWorkspaceAssignments(ctx, db, user.ID)
		if err != nil {
			return UserDetail{}, err
		}
		detail.WorkspaceAssignments = assignments
		return detail, nil
	}

	member, err := loadActiveWorkspaceMember(ctx, db, scope.WorkspaceID, user.ID)
	if err != nil {
		return UserDetail{}, err
	}
	detail.CurrentWorkspaceMember = &WorkspaceMemberSummary{
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
		Role:        models.NormalizeWorkspaceRole(member.Role),
		Status:      member.Status,
		JoinedAt:    member.JoinedAt,
	}
	return detail, nil
}

func (u *UserManagementUseCase) UpdateUser(ctx context.Context, req UpdateUserRequest) (UserSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserSummary{}, err
	}
	if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return UserSummary{}, err
	}
	if req.UserID == 0 {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "email is required"}
	}
	phone := strings.TrimSpace(req.Phone)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureUniqueUserEmail(ctx, db, req.UserID, email); err != nil {
		return UserSummary{}, err
	}
	if err := ensureUniqueUserPhone(ctx, db, req.UserID, phone); err != nil {
		return UserSummary{}, err
	}

	var updated models.User
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		before := buildUserSummary(user)
		if err := tx.Model(&user).Updates(map[string]any{
			"email":    email,
			"phone":    phone,
			"nickname": strings.TrimSpace(req.Nickname),
		}).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to update user"}
		}
		updated, err = loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		return appendUserManagementAudit(ctx, tx, req.Actor, req.WorkspaceID, "user.update", req.UserID, before, buildUserSummary(updated), nil)
	})
	if err != nil {
		return UserSummary{}, err
	}
	return buildUserSummary(updated), nil
}

func (u *UserManagementUseCase) DisableUser(ctx context.Context, req DisableUserRequest) (UserSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserSummary{}, err
	}
	if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return UserSummary{}, err
	}
	if req.UserID == 0 {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var disabled models.User
	now := time.Now().Unix()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		before := buildUserSummary(user)
		if err := tx.Model(&user).Updates(map[string]any{
			"status":      "disabled",
			"disabled_at": now,
			"disabled_by": req.Actor.UserID,
		}).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to disable user"}
		}
		disabled, err = loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		metadata := map[string]any{"reason": strings.TrimSpace(req.Reason)}
		return appendUserManagementAudit(ctx, tx, req.Actor, req.WorkspaceID, "user.disable", req.UserID, before, buildUserSummary(disabled), metadata)
	})
	if err != nil {
		return UserSummary{}, err
	}
	if err := middleware.RevokeSessionsForUser(ctx, req.UserID); err != nil {
		return UserSummary{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to revoke user sessions"}
	}
	return buildUserSummary(disabled), nil
}

func (u *UserManagementUseCase) EnableUser(ctx context.Context, req EnableUserRequest) (UserSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserSummary{}, err
	}
	if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return UserSummary{}, err
	}
	if req.UserID == 0 {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var enabled models.User
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		before := buildUserSummary(user)
		if err := tx.Model(&user).Updates(map[string]any{
			"status":      "active",
			"disabled_at": 0,
			"disabled_by": nil,
		}).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to enable user"}
		}
		enabled, err = loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		return appendUserManagementAudit(ctx, tx, req.Actor, req.WorkspaceID, "user.enable", req.UserID, before, buildUserSummary(enabled), nil)
	})
	if err != nil {
		return UserSummary{}, err
	}
	return buildUserSummary(enabled), nil
}

func (u *UserManagementUseCase) ResetPassword(ctx context.Context, req ResetPasswordRequest) (UserSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserSummary{}, err
	}
	if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return UserSummary{}, err
	}
	password := strings.TrimSpace(req.NewPassword)
	if req.UserID == 0 {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if password == "" {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "new password is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var reset models.User
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		before := buildUserSummary(user)
		if err := user.SetPassword(password); err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to hash password"}
		}
		if err := tx.Model(&user).Updates(map[string]any{
			"password":             user.Password,
			"must_change_password": true,
			"password_changed_at":  0,
		}).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to reset password"}
		}
		reset, err = loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		return appendUserManagementAudit(ctx, tx, req.Actor, req.WorkspaceID, "user.reset_password", req.UserID, before, buildUserSummary(reset), nil)
	})
	if err != nil {
		return UserSummary{}, err
	}
	_ = middleware.RevokeSessionsForUser(ctx, req.UserID)
	return buildUserSummary(reset), nil
}

func (u *UserManagementUseCase) UpdateSystemRole(ctx context.Context, req UpdateSystemRoleRequest) (UserSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return UserSummary{}, err
	}
	if err := requirePlatformUserManagement(ctx, db, req.Actor, req.WorkspaceID); err != nil {
		return UserSummary{}, err
	}
	role, ok := normalizeUserManagementSystemRole(req.SystemRole)
	if !ok {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "invalid system role"}
	}
	if req.UserID == 0 {
		return UserSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var updated models.User
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		before := buildUserSummary(user)
		if err := tx.Model(&user).Update("role", role).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to update system role"}
		}
		updated, err = loadUserByID(ctx, tx, req.UserID)
		if err != nil {
			return err
		}
		return appendUserManagementAudit(ctx, tx, req.Actor, req.WorkspaceID, "user.update_system_role", req.UserID, before, buildUserSummary(updated), nil)
	})
	if err != nil {
		return UserSummary{}, err
	}
	return buildUserSummary(updated), nil
}

func (u *UserManagementUseCase) AddUserToWorkspace(ctx context.Context, req AddUserToWorkspaceRequest) (WorkspaceMemberSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.TargetWorkspaceID)
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	if req.UserID == 0 {
		return WorkspaceMemberSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := loadUserByID(ctx, db, req.UserID); err != nil {
		return WorkspaceMemberSummary{}, err
	}

	role := models.NormalizeWorkspaceRole(req.Role)
	var created models.WorkspaceMember
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingCount int64
		if err := tx.Model(&models.WorkspaceMember{}).
			Where("workspace_id = ? AND user_id = ?", targetWorkspaceID, req.UserID).
			Count(&existingCount).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to validate workspace membership"}
		}
		if existingCount > 0 {
			return ServiceError{Code: ErrorCodeConflict, Message: "workspace member already exists"}
		}
		created = models.WorkspaceMember{
			WorkspaceID: targetWorkspaceID,
			UserID:      req.UserID,
			Role:        role,
			Status:      models.WorkspaceMemberStatusActive,
			InvitedBy:   req.Actor.UserID,
			JoinedAt:    time.Now().Unix(),
		}
		if err := tx.Create(&created).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to add workspace member"}
		}
		return appendWorkspaceMemberAudit(ctx, tx, req.Actor, req.WorkspaceID, "workspace_member.add", created.ID, created)
	})
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	_ = middleware.BumpWorkspaceAuthVersion(ctx, targetWorkspaceID)
	return buildWorkspaceMemberSummary(created), nil
}

func (u *UserManagementUseCase) SearchWorkspaceMemberCandidates(ctx context.Context, req SearchWorkspaceMemberCandidatesRequest) (WorkspaceMemberCandidateListResult, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return WorkspaceMemberCandidateListResult{}, err
	}
	targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.WorkspaceID)
	if err != nil {
		return WorkspaceMemberCandidateListResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	keyword := strings.TrimSpace(req.Query)
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	result := WorkspaceMemberCandidateListResult{List: []WorkspaceMemberCandidateSummary{}, Limit: limit}
	if keyword == "" {
		return result, nil
	}

	query := db.WithContext(ctx).
		Model(&models.User{}).
		Where("users.status = ?", "active").
		Where("NOT EXISTS (SELECT 1 FROM workspace_members WHERE workspace_members.workspace_id = ? AND workspace_members.user_id = users.id)", targetWorkspaceID)
	if !strings.EqualFold(strings.TrimSpace(req.Actor.SystemRole), "admin") {
		query = query.Where("users.role <> ?", "admin")
	}
	like := "%" + keyword + "%"
	query = query.Where("users.username LIKE ? OR users.email LIKE ? OR users.phone LIKE ?", like, like, like)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return WorkspaceMemberCandidateListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to count member candidates"}
	}

	var users []models.User
	if err := query.Order("users.id ASC").Limit(limit).Find(&users).Error; err != nil {
		return WorkspaceMemberCandidateListResult{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to list member candidates"}
	}
	result.Total = total
	for _, user := range users {
		systemRole, ok := normalizeUserManagementSystemRole(user.Role)
		if !ok {
			systemRole = strings.TrimSpace(user.Role)
		}
		result.List = append(result.List, WorkspaceMemberCandidateSummary{
			ID:         user.ID,
			Username:   user.Username,
			Email:      user.Email,
			Phone:      user.Phone,
			Nickname:   user.Nickname,
			SystemRole: systemRole,
			Status:     user.Status,
		})
	}
	return result, nil
}

func (u *UserManagementUseCase) RemoveUserFromWorkspace(ctx context.Context, req RemoveUserFromWorkspaceRequest) (WorkspaceMemberSummary, error) {
	db, err := userManagementDB(u)
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	targetWorkspaceID, err := resolveWorkspaceAssignmentTarget(ctx, db, req.Actor, req.WorkspaceID, req.TargetWorkspaceID)
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	if req.UserID == 0 {
		return WorkspaceMemberSummary{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "user id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var removed models.WorkspaceMember
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var member models.WorkspaceMember
		if err := tx.Where("workspace_id = ? AND user_id = ?", targetWorkspaceID, req.UserID).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ServiceError{Code: ErrorCodeNotFound, Message: "workspace member not found"}
			}
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace member"}
		}
		if models.NormalizeWorkspaceRole(member.Role) == models.WorkspaceRoleOwner {
			var remainingOwners int64
			if err := tx.Model(&models.WorkspaceMember{}).
				Where("workspace_id = ? AND id <> ? AND role = ? AND status = ?", targetWorkspaceID, member.ID, models.WorkspaceRoleOwner, models.WorkspaceMemberStatusActive).
				Count(&remainingOwners).Error; err != nil {
				return ServiceError{Code: ErrorCodeInternalError, Message: "failed to validate workspace owners"}
			}
			if remainingOwners == 0 {
				return ServiceError{Code: ErrorCodeForbidden, Message: "at least one owner is required"}
			}
		}
		removed = member
		if err := tx.Delete(&member).Error; err != nil {
			return ServiceError{Code: ErrorCodeInternalError, Message: "failed to remove workspace member"}
		}
		return appendWorkspaceMemberAudit(ctx, tx, req.Actor, req.WorkspaceID, "workspace_member.remove", member.ID, member)
	})
	if err != nil {
		return WorkspaceMemberSummary{}, err
	}
	_ = middleware.BumpWorkspaceAuthVersion(ctx, targetWorkspaceID)
	return buildWorkspaceMemberSummary(removed), nil
}

func userManagementDB(u *UserManagementUseCase) (*gorm.DB, error) {
	if u == nil || u.DB == nil {
		return nil, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	return u.DB, nil
}

func requirePlatformUserManagement(ctx context.Context, db *gorm.DB, actor ActorContext, workspaceID uint64) error {
	scope, err := resolveUserManagementScope(ctx, db, actor, workspaceID)
	if err != nil {
		return err
	}
	if scope.Kind != userManagementScopePlatform {
		return ServiceError{Code: ErrorCodeForbidden, Message: "platform governance is required"}
	}
	return nil
}

func resolveWorkspaceAssignmentTarget(ctx context.Context, db *gorm.DB, actor ActorContext, currentWorkspaceID, targetWorkspaceID uint64) (uint64, error) {
	scope, err := resolveUserManagementScope(ctx, db, actor, currentWorkspaceID)
	if err != nil {
		return 0, err
	}
	if targetWorkspaceID == 0 {
		targetWorkspaceID = currentWorkspaceID
	}
	if targetWorkspaceID == 0 {
		return 0, ServiceError{Code: ErrorCodeInvalidArgument, Message: "target workspace id is required"}
	}
	if scope.Kind == userManagementScopeWorkspace && targetWorkspaceID != scope.WorkspaceID {
		return 0, ServiceError{Code: ErrorCodeForbidden, Message: "only current workspace can be managed"}
	}
	workspace, err := loadUserManagementWorkspace(ctx, db, targetWorkspaceID)
	if err != nil {
		return 0, err
	}
	if normalizeUserManagementWorkspaceKind(workspace.Kind) != models.WorkspaceKindNormal {
		return 0, ServiceError{Code: ErrorCodeForbidden, Message: "admin workspace members cannot be managed here"}
	}
	return targetWorkspaceID, nil
}

func loadUserManagementWorkspace(ctx context.Context, db *gorm.DB, workspaceID uint64) (models.Workspace, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var workspace models.Workspace
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Workspace{}, ServiceError{Code: ErrorCodeNotFound, Message: "workspace not found"}
		}
		return models.Workspace{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace"}
	}
	return workspace, nil
}

func resolveUserManagementScope(ctx context.Context, db *gorm.DB, actor ActorContext, workspaceID uint64) (userManagementScope, error) {
	if actor.UserID == 0 {
		return userManagementScope{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "actor user id is required"}
	}
	if workspaceID == 0 {
		return userManagementScope{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "workspace id is required"}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var workspace models.Workspace
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", workspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return userManagementScope{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace access denied"}
		}
		return userManagementScope{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace"}
	}
	workspaceKind := normalizeUserManagementWorkspaceKind(workspace.Kind)
	systemAdmin := strings.EqualFold(strings.TrimSpace(actor.SystemRole), "admin")

	if workspaceKind == models.WorkspaceKindAdmin {
		if systemAdmin && hasActiveWorkspaceMember(ctx, db, workspaceID, actor.UserID) {
			return userManagementScope{Kind: userManagementScopePlatform, WorkspaceID: workspaceID}, nil
		}
		return userManagementScope{}, ServiceError{Code: ErrorCodeForbidden, Message: "platform governance is required"}
	}

	if systemAdmin {
		return userManagementScope{Kind: userManagementScopeWorkspace, WorkspaceID: workspaceID}, nil
	}
	member, err := loadActiveWorkspaceMember(ctx, db, workspaceID, actor.UserID)
	if err != nil {
		return userManagementScope{}, err
	}
	if models.NormalizeWorkspaceRole(member.Role) != models.WorkspaceRoleOwner {
		return userManagementScope{}, ServiceError{Code: ErrorCodeForbidden, Message: "workspace owner is required"}
	}
	return userManagementScope{Kind: userManagementScopeWorkspace, WorkspaceID: workspaceID}, nil
}

func applyUserManagementFilters(query *gorm.DB, req ListUsersRequest) *gorm.DB {
	if keyword := strings.TrimSpace(req.Query); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("users.username LIKE ? OR users.nickname LIKE ? OR users.email LIKE ?", like, like, like)
	}
	if role := strings.TrimSpace(req.Role); role != "" {
		query = query.Where("users.role = ?", role)
	}
	if status := strings.TrimSpace(req.Status); status != "" {
		query = query.Where("users.status = ?", status)
	}
	if req.WorkspaceFilterID > 0 {
		query = query.Joins("JOIN workspace_members user_management_workspace_members ON user_management_workspace_members.user_id = users.id AND user_management_workspace_members.workspace_id = ? AND user_management_workspace_members.status = ?", req.WorkspaceFilterID, models.WorkspaceMemberStatusActive)
	}
	return query
}

func attachWorkspaceAssignmentsToUserSummaries(ctx context.Context, db *gorm.DB, summaries []UserSummary) error {
	if len(summaries) == 0 {
		return nil
	}
	userIDs := make([]uint64, 0, len(summaries))
	for _, summary := range summaries {
		userIDs = append(userIDs, summary.ID)
	}
	var rows []struct {
		UserID        uint64 `gorm:"column:user_id"`
		WorkspaceID   uint64 `gorm:"column:workspace_id"`
		WorkspaceName string `gorm:"column:workspace_name"`
		WorkspaceKind string `gorm:"column:workspace_kind"`
		Role          string `gorm:"column:role"`
		Status        string `gorm:"column:status"`
		JoinedAt      int64  `gorm:"column:joined_at"`
	}
	if err := db.WithContext(ctx).
		Table("workspace_members").
		Select("workspace_members.user_id AS user_id, workspaces.id AS workspace_id, workspaces.name AS workspace_name, workspaces.kind AS workspace_kind, workspace_members.role AS role, workspace_members.status AS status, workspace_members.joined_at AS joined_at").
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").
		Where("workspace_members.user_id IN ? AND workspace_members.status = ? AND workspaces.status = ?", userIDs, models.WorkspaceMemberStatusActive, models.WorkspaceStatusActive).
		Order("workspace_members.user_id ASC, workspace_members.created_at ASC").
		Scan(&rows).Error; err != nil {
		return ServiceError{Code: ErrorCodeInternalError, Message: "failed to list user workspace assignments"}
	}
	byUserID := make(map[uint64][]WorkspaceAssignmentSummary, len(summaries))
	for _, row := range rows {
		byUserID[row.UserID] = append(byUserID[row.UserID], WorkspaceAssignmentSummary{
			WorkspaceID:   row.WorkspaceID,
			WorkspaceName: row.WorkspaceName,
			WorkspaceKind: normalizeWorkspaceKind(row.WorkspaceKind),
			Role:          models.NormalizeWorkspaceRole(row.Role),
			Status:        row.Status,
			JoinedAt:      row.JoinedAt,
		})
	}
	for i := range summaries {
		summaries[i].WorkspaceAssignments = byUserID[summaries[i].ID]
	}
	return nil
}

func loadUserByID(ctx context.Context, db *gorm.DB, userID uint64) (models.User, error) {
	var user models.User
	if err := db.WithContext(ctx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, ServiceError{Code: ErrorCodeNotFound, Message: "user not found"}
		}
		return models.User{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load user"}
	}
	return user, nil
}

func ensureUniqueUserEmail(ctx context.Context, db *gorm.DB, userID uint64, email string) error {
	var count int64
	if err := db.WithContext(ctx).Model(&models.User{}).
		Where("id <> ? AND LOWER(email) = LOWER(?)", userID, email).
		Count(&count).Error; err != nil {
		return ServiceError{Code: ErrorCodeInternalError, Message: "failed to validate email"}
	}
	if count > 0 {
		return ServiceError{Code: ErrorCodeConflict, Message: "email already exists"}
	}
	return nil
}

func ensureUniqueUserPhone(ctx context.Context, db *gorm.DB, userID uint64, phone string) error {
	if strings.TrimSpace(phone) == "" {
		return nil
	}
	var count int64
	if err := db.WithContext(ctx).Model(&models.User{}).
		Where("id <> ? AND phone = ?", userID, phone).
		Count(&count).Error; err != nil {
		return ServiceError{Code: ErrorCodeInternalError, Message: "failed to validate phone"}
	}
	if count > 0 {
		return ServiceError{Code: ErrorCodeConflict, Message: "phone already exists"}
	}
	return nil
}

func normalizeUserManagementSystemRole(role string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return "admin", true
	case "user":
		return "user", true
	default:
		return "", false
	}
}

func appendUserManagementAudit(ctx context.Context, db *gorm.DB, actor ActorContext, workspaceID uint64, action string, targetID uint64, before any, after any, metadata any) error {
	return AppendAuditLog(ctx, db, AuditInput{
		WorkspaceID:      &workspaceID,
		ActorUserID:      actor.UserID,
		ActorRole:        strings.TrimSpace(actor.SystemRole),
		ActorWorkspaceID: &workspaceID,
		Action:           action,
		TargetType:       "user",
		TargetID:         targetID,
		Before:           before,
		After:            after,
		Metadata:         metadata,
	})
}

func appendWorkspaceMemberAudit(ctx context.Context, db *gorm.DB, actor ActorContext, actorWorkspaceID uint64, action string, memberID uint64, member models.WorkspaceMember) error {
	workspaceID := member.WorkspaceID
	return AppendAuditLog(ctx, db, AuditInput{
		WorkspaceID:      &workspaceID,
		ActorUserID:      actor.UserID,
		ActorRole:        strings.TrimSpace(actor.SystemRole),
		ActorWorkspaceID: &actorWorkspaceID,
		Action:           action,
		TargetType:       "workspace_member",
		TargetID:         memberID,
		Metadata: map[string]any{
			"user_id":      member.UserID,
			"workspace_id": member.WorkspaceID,
			"role":         models.NormalizeWorkspaceRole(member.Role),
		},
	})
}

func buildWorkspaceMemberSummary(member models.WorkspaceMember) WorkspaceMemberSummary {
	return WorkspaceMemberSummary{
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
		Role:        models.NormalizeWorkspaceRole(member.Role),
		Status:      member.Status,
		JoinedAt:    member.JoinedAt,
	}
}

func normalizePagination(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

func normalizeUserManagementWorkspaceKind(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), models.WorkspaceKindAdmin) {
		return models.WorkspaceKindAdmin
	}
	return models.WorkspaceKindNormal
}

func hasActiveWorkspaceMember(ctx context.Context, db *gorm.DB, workspaceID, userID uint64) bool {
	var count int64
	err := db.WithContext(ctx).Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ? AND status = ?", workspaceID, userID, models.WorkspaceMemberStatusActive).
		Count(&count).Error
	return err == nil && count > 0
}

func loadActiveWorkspaceMember(ctx context.Context, db *gorm.DB, workspaceID, userID uint64) (models.WorkspaceMember, error) {
	var member models.WorkspaceMember
	if err := db.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ? AND status = ?", workspaceID, userID, models.WorkspaceMemberStatusActive).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.WorkspaceMember{}, ServiceError{Code: ErrorCodeNotFound, Message: "workspace member not found"}
		}
		return models.WorkspaceMember{}, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace member"}
	}
	return member, nil
}

func listUserWorkspaceAssignments(ctx context.Context, db *gorm.DB, userID uint64) ([]WorkspaceAssignmentSummary, error) {
	var members []models.WorkspaceMember
	if err := db.WithContext(ctx).Preload("Workspace").
		Where("user_id = ?", userID).
		Order("workspace_id ASC").
		Find(&members).Error; err != nil {
		return nil, ServiceError{Code: ErrorCodeInternalError, Message: "failed to load workspace assignments"}
	}
	assignments := make([]WorkspaceAssignmentSummary, 0, len(members))
	for _, member := range members {
		workspaceName := ""
		workspaceKind := models.WorkspaceKindNormal
		if member.Workspace != nil {
			workspaceName = member.Workspace.Name
			workspaceKind = normalizeUserManagementWorkspaceKind(member.Workspace.Kind)
		}
		assignments = append(assignments, WorkspaceAssignmentSummary{
			WorkspaceID:   member.WorkspaceID,
			WorkspaceName: workspaceName,
			WorkspaceKind: workspaceKind,
			Role:          models.NormalizeWorkspaceRole(member.Role),
			Status:        member.Status,
			JoinedAt:      member.JoinedAt,
		})
	}
	return assignments, nil
}

func buildUserSummary(user models.User) UserSummary {
	return UserSummary{
		ID:                 user.ID,
		Username:           user.Username,
		Email:              user.Email,
		Phone:              user.Phone,
		Nickname:           user.Nickname,
		SystemRole:         user.Role,
		Status:             user.Status,
		MustChangePassword: user.MustChangePassword,
		LastLoginAt:        user.LastLoginAt,
		DisabledAt:         user.DisabledAt,
	}
}
