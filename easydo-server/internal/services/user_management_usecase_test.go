package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func setupUserManagementAuthTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", "user-management-test-secret")
	t.Setenv("AUTH_TOKEN_TTL", (4 * time.Hour).String())
	t.Setenv("AUTH_REFRESH_INTERVAL", (10 * time.Minute).String())
	config.Init()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		_ = utils.RedisClient.Close()
		utils.RedisClient = previousRedis
		mini.Close()
	})
}

func openUserManagementMutationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openActorTestDB(t)
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("auto migrate audit log failed: %v", err)
	}
	return db
}

func seedUserManagementUser(t *testing.T, db *gorm.DB, username, role string) models.User {
	t.Helper()
	user := models.User{
		Username: username,
		Email:    username + "@example.com",
		Role:     role,
		Status:   "active",
		Nickname: username,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %s failed: %v", username, err)
	}
	return user
}

func seedUserManagementWorkspace(t *testing.T, db *gorm.DB, name, kind string, creatorID uint64) models.Workspace {
	t.Helper()
	workspace := models.Workspace{
		Name:       name,
		Slug:       name,
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		Kind:       kind,
		CreatedBy:  creatorID,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace %s failed: %v", name, err)
	}
	return workspace
}

func seedUserManagementMember(t *testing.T, db *gorm.DB, workspaceID, userID uint64, role string) models.WorkspaceMember {
	t.Helper()
	member := models.WorkspaceMember{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   userID,
		JoinedAt:    100,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	return member
}

func assertServiceErrorCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var svcErr ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error=%v, want ServiceError", err)
	}
	if svcErr.Code != code {
		t.Fatalf("error code=%q, want %q", svcErr.Code, code)
	}
}

func TestUserManagementListUsersRequiresPlatformGovernance(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-list-admin", "admin")
	owner := seedUserManagementUser(t, db, "um-list-owner", "user")
	target := seedUserManagementUser(t, db, "um-list-target", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-list-admin-space", models.WorkspaceKindAdmin, admin.ID)
	normalWorkspace := seedUserManagementWorkspace(t, db, "um-list-normal-space", models.WorkspaceKindNormal, owner.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, normalWorkspace.ID, owner.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, normalWorkspace.ID, target.ID, models.WorkspaceRoleDeveloper)

	result, err := usecase.ListUsers(context.Background(), ListUsersRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		Page:        1,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("total=%d, want 3", result.Total)
	}
	usernames := make([]string, 0, len(result.List))
	for _, item := range result.List {
		usernames = append(usernames, item.Username)
	}
	serialized, err := json.Marshal(result.List)
	if err != nil {
		t.Fatalf("marshal user list failed: %v", err)
	}
	if strings.Contains(strings.ToLower(string(serialized)), `"password"`) {
		t.Fatalf("user list leaked password field: %s", string(serialized))
	}
	for _, username := range []string{admin.Username, owner.Username, target.Username} {
		if !slices.Contains(usernames, username) {
			t.Fatalf("list usernames=%v, missing %s", usernames, username)
		}
	}
	var targetSummary UserSummary
	for _, item := range result.List {
		if item.ID == target.ID {
			targetSummary = item
			break
		}
	}
	if len(targetSummary.WorkspaceAssignments) != 1 {
		t.Fatalf("target assignments=%+v, want one workspace assignment", targetSummary.WorkspaceAssignments)
	}
	if targetSummary.WorkspaceAssignments[0].WorkspaceID != normalWorkspace.ID || targetSummary.WorkspaceAssignments[0].WorkspaceName != normalWorkspace.Name {
		t.Fatalf("target assignment=%+v, want normal workspace", targetSummary.WorkspaceAssignments[0])
	}

	filtered, err := usecase.ListUsers(context.Background(), ListUsersRequest{
		Actor:             ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:       adminWorkspace.ID,
		WorkspaceFilterID: normalWorkspace.ID,
		Page:              1,
		Limit:             20,
	})
	if err != nil {
		t.Fatalf("ListUsers with workspace filter returned error: %v", err)
	}
	filteredNames := make([]string, 0, len(filtered.List))
	for _, item := range filtered.List {
		filteredNames = append(filteredNames, item.Username)
	}
	if filtered.Total != 2 || !slices.Contains(filteredNames, owner.Username) || !slices.Contains(filteredNames, target.Username) || slices.Contains(filteredNames, admin.Username) {
		t.Fatalf("workspace filtered users total=%d names=%v", filtered.Total, filteredNames)
	}

	_, err = usecase.ListUsers(context.Background(), ListUsersRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: normalWorkspace.ID,
		Page:        1,
		Limit:       20,
	})
	assertServiceErrorCode(t, err, ErrorCodeForbidden)

	_, err = usecase.ListUsers(context.Background(), ListUsersRequest{
		Actor:       ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID: normalWorkspace.ID,
		Page:        1,
		Limit:       20,
	})
	assertServiceErrorCode(t, err, ErrorCodeForbidden)
}

func TestUserManagementGetUserDetailScopesWorkspaceOwnerToCurrentMember(t *testing.T) {
	db := openActorTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-detail-admin", "admin")
	owner := seedUserManagementUser(t, db, "um-detail-owner", "user")
	target := seedUserManagementUser(t, db, "um-detail-target", "user")
	other := seedUserManagementUser(t, db, "um-detail-other", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-detail-admin-space", models.WorkspaceKindAdmin, admin.ID)
	workspaceA := seedUserManagementWorkspace(t, db, "um-detail-a", models.WorkspaceKindNormal, owner.ID)
	workspaceB := seedUserManagementWorkspace(t, db, "um-detail-b", models.WorkspaceKindNormal, other.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspaceA.ID, owner.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspaceA.ID, target.ID, models.WorkspaceRoleDeveloper)
	seedUserManagementMember(t, db, workspaceB.ID, target.ID, models.WorkspaceRoleViewer)
	seedUserManagementMember(t, db, workspaceB.ID, other.ID, models.WorkspaceRoleOwner)

	adminDetail, err := usecase.GetUser(context.Background(), GetUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
	})
	if err != nil {
		t.Fatalf("admin GetUser returned error: %v", err)
	}
	if len(adminDetail.WorkspaceAssignments) != 2 {
		t.Fatalf("workspace assignments=%+v, want two assignments", adminDetail.WorkspaceAssignments)
	}

	ownerDetail, err := usecase.GetUser(context.Background(), GetUserRequest{
		Actor:       ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID: workspaceA.ID,
		UserID:      target.ID,
	})
	if err != nil {
		t.Fatalf("owner GetUser returned error: %v", err)
	}
	if ownerDetail.CurrentWorkspaceMember == nil {
		t.Fatalf("expected current workspace member detail, got %+v", ownerDetail)
	}
	if ownerDetail.CurrentWorkspaceMember.WorkspaceID != workspaceA.ID || ownerDetail.CurrentWorkspaceMember.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("current member=%+v, want workspace A developer", ownerDetail.CurrentWorkspaceMember)
	}
	if len(ownerDetail.WorkspaceAssignments) != 0 {
		t.Fatalf("workspace owner should not see global assignments, got %+v", ownerDetail.WorkspaceAssignments)
	}

	_, err = usecase.GetUser(context.Background(), GetUserRequest{
		Actor:       ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID: workspaceA.ID,
		UserID:      other.ID,
	})
	assertServiceErrorCode(t, err, ErrorCodeNotFound)
}

func TestUserManagementUpdateUserValidatesEmailAndPhoneUniqueness(t *testing.T) {
	db := openUserManagementMutationTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-update-admin", "admin")
	target := seedUserManagementUser(t, db, "um-update-target", "user")
	duplicate := seedUserManagementUser(t, db, "um-update-duplicate", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-update-admin-space", models.WorkspaceKindAdmin, admin.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	if err := db.Model(&duplicate).Updates(map[string]any{"email": "duplicate@example.com", "phone": "13800000000"}).Error; err != nil {
		t.Fatalf("update duplicate seed failed: %v", err)
	}

	_, err := usecase.UpdateUser(context.Background(), UpdateUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		Email:       "",
	})
	assertServiceErrorCode(t, err, ErrorCodeInvalidArgument)

	_, err = usecase.UpdateUser(context.Background(), UpdateUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		Email:       "duplicate@example.com",
	})
	assertServiceErrorCode(t, err, ErrorCodeConflict)

	_, err = usecase.UpdateUser(context.Background(), UpdateUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		Email:       "target-updated@example.com",
		Phone:       "13800000000",
	})
	assertServiceErrorCode(t, err, ErrorCodeConflict)

	updated, err := usecase.UpdateUser(context.Background(), UpdateUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		Email:       " target-updated@example.com ",
		Phone:       "",
		Nickname:    "Updated Target",
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if updated.Email != "target-updated@example.com" || updated.Phone != "" || updated.Nickname != "Updated Target" {
		t.Fatalf("updated summary=%+v", updated)
	}
	var auditCount int64
	if err := db.Model(&models.AuditLog{}).Where("action = ? AND target_id = ?", "user.update", target.ID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count=%d, want 1", auditCount)
	}
}

func TestUserManagementDisableUserRevokesSessionsAndEnableRestoresAccount(t *testing.T) {
	setupUserManagementAuthTestEnv(t)
	db := openUserManagementMutationTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-disable-admin", "admin")
	target := seedUserManagementUser(t, db, "um-disable-target", "user")
	if err := target.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set target password failed: %v", err)
	}
	if err := db.Save(&target).Error; err != nil {
		t.Fatalf("save target password failed: %v", err)
	}
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-disable-admin-space", models.WorkspaceKindAdmin, admin.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)

	token, _, err := middleware.IssueTokenSession(context.Background(), &target)
	if err != nil {
		t.Fatalf("issue token session failed: %v", err)
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	if err := middleware.ValidateTokenSession(context.Background(), claims); err != nil {
		t.Fatalf("session should be valid before disable: %v", err)
	}

	disabled, err := usecase.DisableUser(context.Background(), DisableUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		Reason:      "left company",
	})
	if err != nil {
		t.Fatalf("DisableUser returned error: %v", err)
	}
	if disabled.Status != "disabled" || disabled.DisabledAt == 0 {
		t.Fatalf("disabled summary=%+v", disabled)
	}
	if err := middleware.ValidateTokenSession(context.Background(), claims); err == nil {
		t.Fatal("expected disabled user session to be revoked")
	}

	enabled, err := usecase.EnableUser(context.Background(), EnableUserRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
	})
	if err != nil {
		t.Fatalf("EnableUser returned error: %v", err)
	}
	if enabled.Status != "active" || enabled.DisabledAt != 0 {
		t.Fatalf("enabled summary=%+v", enabled)
	}
	var auditCount int64
	if err := db.Model(&models.AuditLog{}).Where("target_id = ? AND action IN ?", target.ID, []string{"user.disable", "user.enable"}).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("audit count=%d, want 2", auditCount)
	}
}

func TestUserManagementResetPasswordAndSystemRoleRequirePlatformGovernance(t *testing.T) {
	db := openUserManagementMutationTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-reset-admin", "admin")
	owner := seedUserManagementUser(t, db, "um-reset-owner", "user")
	target := seedUserManagementUser(t, db, "um-reset-target", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-reset-admin-space", models.WorkspaceKindAdmin, admin.ID)
	normalWorkspace := seedUserManagementWorkspace(t, db, "um-reset-normal-space", models.WorkspaceKindNormal, owner.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, normalWorkspace.ID, owner.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, normalWorkspace.ID, target.ID, models.WorkspaceRoleDeveloper)

	_, err := usecase.ResetPassword(context.Background(), ResetPasswordRequest{
		Actor:       ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID: normalWorkspace.ID,
		UserID:      target.ID,
		NewPassword: "NewPass123!",
	})
	assertServiceErrorCode(t, err, ErrorCodeForbidden)

	reset, err := usecase.ResetPassword(context.Background(), ResetPasswordRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		NewPassword: "NewPass123!",
	})
	if err != nil {
		t.Fatalf("ResetPassword returned error: %v", err)
	}
	if !reset.MustChangePassword {
		t.Fatalf("reset summary=%+v, want must_change_password", reset)
	}
	var loaded models.User
	if err := db.First(&loaded, target.ID).Error; err != nil {
		t.Fatalf("load target failed: %v", err)
	}
	if !loaded.CheckPassword("NewPass123!") {
		t.Fatal("new password does not verify")
	}

	_, err = usecase.UpdateSystemRole(context.Background(), UpdateSystemRoleRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		SystemRole:  "superadmin",
	})
	assertServiceErrorCode(t, err, ErrorCodeInvalidArgument)

	roleUpdated, err := usecase.UpdateSystemRole(context.Background(), UpdateSystemRoleRequest{
		Actor:       ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID: adminWorkspace.ID,
		UserID:      target.ID,
		SystemRole:  "admin",
	})
	if err != nil {
		t.Fatalf("UpdateSystemRole returned error: %v", err)
	}
	if roleUpdated.SystemRole != "admin" {
		t.Fatalf("system role=%s, want admin", roleUpdated.SystemRole)
	}
}

func TestUserManagementWorkspaceAssignmentScopesAndConflicts(t *testing.T) {
	db := openUserManagementMutationTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-assign-admin", "admin")
	owner := seedUserManagementUser(t, db, "um-assign-owner", "user")
	target := seedUserManagementUser(t, db, "um-assign-target", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-assign-admin-space", models.WorkspaceKindAdmin, admin.ID)
	workspaceA := seedUserManagementWorkspace(t, db, "um-assign-a", models.WorkspaceKindNormal, owner.ID)
	workspaceB := seedUserManagementWorkspace(t, db, "um-assign-b", models.WorkspaceKindNormal, owner.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspaceA.ID, owner.ID, models.WorkspaceRoleOwner)

	assigned, err := usecase.AddUserToWorkspace(context.Background(), AddUserToWorkspaceRequest{
		Actor:             ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:       adminWorkspace.ID,
		UserID:            target.ID,
		TargetWorkspaceID: workspaceB.ID,
		Role:              models.WorkspaceRoleDeveloper,
	})
	if err != nil {
		t.Fatalf("admin AddUserToWorkspace returned error: %v", err)
	}
	if assigned.WorkspaceID != workspaceB.ID || assigned.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("assigned=%+v, want workspace B developer", assigned)
	}

	_, err = usecase.AddUserToWorkspace(context.Background(), AddUserToWorkspaceRequest{
		Actor:             ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:       adminWorkspace.ID,
		UserID:            target.ID,
		TargetWorkspaceID: workspaceB.ID,
		Role:              models.WorkspaceRoleViewer,
	})
	assertServiceErrorCode(t, err, ErrorCodeConflict)

	_, err = usecase.AddUserToWorkspace(context.Background(), AddUserToWorkspaceRequest{
		Actor:             ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID:       workspaceA.ID,
		UserID:            target.ID,
		TargetWorkspaceID: workspaceB.ID,
		Role:              models.WorkspaceRoleViewer,
	})
	assertServiceErrorCode(t, err, ErrorCodeForbidden)

	ownerAssigned, err := usecase.AddUserToWorkspace(context.Background(), AddUserToWorkspaceRequest{
		Actor:       ActorContext{UserID: owner.ID, Username: owner.Username, SystemRole: owner.Role},
		WorkspaceID: workspaceA.ID,
		UserID:      target.ID,
		Role:        models.WorkspaceRoleViewer,
	})
	if err != nil {
		t.Fatalf("owner AddUserToWorkspace returned error: %v", err)
	}
	if ownerAssigned.WorkspaceID != workspaceA.ID || ownerAssigned.Role != models.WorkspaceRoleViewer {
		t.Fatalf("owner assigned=%+v, want current workspace viewer", ownerAssigned)
	}
}

func TestUserManagementWorkspaceAssignmentRemoveGuardsLastOwner(t *testing.T) {
	db := openUserManagementMutationTestDB(t)
	usecase := &UserManagementUseCase{DB: db}

	admin := seedUserManagementUser(t, db, "um-remove-admin", "admin")
	owner := seedUserManagementUser(t, db, "um-remove-owner", "user")
	developer := seedUserManagementUser(t, db, "um-remove-developer", "user")
	adminWorkspace := seedUserManagementWorkspace(t, db, "um-remove-admin-space", models.WorkspaceKindAdmin, admin.ID)
	workspace := seedUserManagementWorkspace(t, db, "um-remove-normal-space", models.WorkspaceKindNormal, owner.ID)
	seedUserManagementMember(t, db, adminWorkspace.ID, admin.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspace.ID, owner.ID, models.WorkspaceRoleOwner)
	seedUserManagementMember(t, db, workspace.ID, developer.ID, models.WorkspaceRoleDeveloper)

	_, err := usecase.RemoveUserFromWorkspace(context.Background(), RemoveUserFromWorkspaceRequest{
		Actor:             ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:       adminWorkspace.ID,
		UserID:            owner.ID,
		TargetWorkspaceID: workspace.ID,
	})
	assertServiceErrorCode(t, err, ErrorCodeForbidden)

	removed, err := usecase.RemoveUserFromWorkspace(context.Background(), RemoveUserFromWorkspaceRequest{
		Actor:             ActorContext{UserID: admin.ID, Username: admin.Username, SystemRole: admin.Role},
		WorkspaceID:       adminWorkspace.ID,
		UserID:            developer.ID,
		TargetWorkspaceID: workspace.ID,
	})
	if err != nil {
		t.Fatalf("RemoveUserFromWorkspace returned error: %v", err)
	}
	if removed.WorkspaceID != workspace.ID || removed.UserID != developer.ID {
		t.Fatalf("removed=%+v, want developer from workspace", removed)
	}
	var memberCount int64
	if err := db.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ?", workspace.ID, developer.ID).Count(&memberCount).Error; err != nil {
		t.Fatalf("count removed member failed: %v", err)
	}
	if memberCount != 0 {
		t.Fatalf("member count=%d, want 0", memberCount)
	}
}
