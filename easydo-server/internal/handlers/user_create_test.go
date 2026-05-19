package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func performCreateUserRequest(t *testing.T, h *UserHandler, actorID uint64, actorRole string, workspaceID uint64, workspaceRole string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if workspaceID > 0 {
		c.Request.Header.Set(middleware.WorkspaceHeaderKey, strconv.FormatUint(workspaceID, 10))
	}
	c.Set("user_id", actorID)
	c.Set("role", actorRole)
	if workspaceID > 0 {
		c.Set("workspace_id", workspaceID)
	}
	if workspaceRole != "" {
		c.Set("workspace_role", workspaceRole)
	}
	h.CreateUser(c)
	return w
}

func TestOwnerCreateUserBindsCurrentWorkspaceOnly(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	owner := models.User{Username: "owner-create-user", Role: "user", Status: "active"}
	_ = owner.SetPassword("1qaz2WSX")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-current", Slug: "team-current", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	otherWorkspace := models.Workspace{Name: "team-other", Slug: "team-other", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&otherWorkspace).Error; err != nil {
		t.Fatalf("create other workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, owner.ID, "user", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "blocked-other-workspace-owner",
		"password":       "1qaz2WSX",
		"workspace_id":   otherWorkspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for other workspace, got %d body=%s", w.Code, w.Body.String())
	}

	w = performCreateUserRequest(t, h, owner.ID, "user", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "created-by-owner",
		"password":       "1qaz2WSX",
		"email":          "created-by-owner@example.com",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "created-by-owner").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	if created.Role != "user" {
		t.Fatalf("system role=%s, want=user", created.Role)
	}
	var workspaceMember models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, created.ID).First(&workspaceMember).Error; err != nil {
		t.Fatalf("workspace membership missing: %v", err)
	}
	if workspaceMember.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("workspace role=%s, want=%s", workspaceMember.Role, models.WorkspaceRoleDeveloper)
	}
	var otherMembershipCount int64
	db.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ?", otherWorkspace.ID, created.ID).Count(&otherMembershipCount)
	if otherMembershipCount != 0 {
		t.Fatalf("other workspace membership count=%d, want=0", otherMembershipCount)
	}
}

func TestOwnerCreateUserCannotGrantPlatformAdmin(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	owner := models.User{Username: "owner-no-platform-admin", Role: "user", Status: "active"}
	_ = owner.SetPassword("1qaz2WSX")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "owner-normal", Slug: "owner-normal", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, owner.ID, "user", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "blocked-owner-platform-admin",
		"password":       "1qaz2WSX",
		"system_role":    "admin",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for system admin role, got %d body=%s", w.Code, w.Body.String())
	}

	var count int64
	db.Model(&models.User{}).Where("username = ?", "blocked-owner-platform-admin").Count(&count)
	if count != 0 {
		t.Fatalf("created user count=%d, want=0", count)
	}
}

func TestAdminCreatePlatformUserInAdminWorkspace(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin-create", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "platform-governance", Slug: "platform-governance", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":    "created-platform-user",
		"password":    "1qaz2WSX",
		"email":       "created-platform-user@example.com",
		"nickname":    "Created Platform User",
		"system_role": "admin",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "created-platform-user").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	if created.Role != "admin" {
		t.Fatalf("system role=%s, want=admin", created.Role)
	}
	var membershipCount int64
	db.Model(&models.WorkspaceMember{}).Where("user_id = ?", created.ID).Count(&membershipCount)
	if membershipCount != 1 {
		t.Fatalf("membership count=%d, want=1 personal workspace only", membershipCount)
	}
}

func TestCreateUserAsPlatformAdminInNormalWorkspaceBindsCurrentWorkspaceOnly(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin-normal-workspace", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "shared-normal", Slug: "shared-normal", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "created-by-admin-in-normal",
		"password":       "1qaz2WSX",
		"email":          "created-by-admin-in-normal@example.com",
		"workspace_id":   workspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "created-by-admin-in-normal").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	var workspaceMember models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, created.ID).First(&workspaceMember).Error; err != nil {
		t.Fatalf("load workspace member failed: %v", err)
	}
	if workspaceMember.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("workspace role=%s, want=%s", workspaceMember.Role, models.WorkspaceRoleDeveloper)
	}
}

func TestCreateUserAsPlatformAdminInNormalWorkspaceRejectsOtherWorkspaceBinding(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin-other-workspace", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "shared-current", Slug: "shared-current", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	otherWorkspace := models.Workspace{Name: "shared-other", Slug: "shared-other", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&otherWorkspace).Error; err != nil {
		t.Fatalf("create other workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "blocked-admin-other-workspace",
		"password":       "1qaz2WSX",
		"workspace_id":   otherWorkspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for other workspace, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsPlatformAdminInNormalWorkspaceCannotGrantPlatformAdmin(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin-normal-no-platform-admin", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "shared-normal-no-platform-admin", Slug: "shared-normal-no-platform-admin", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "blocked-admin-platform-normal",
		"password":       "1qaz2WSX",
		"system_role":    "admin",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for platform admin role in normal workspace, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserRejectsMaintainerActor(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "maintainer-actor", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "maintainer-team", Slug: "maintainer-team", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]any{
		"username":       "forbidden-by-maintainer",
		"password":       "1qaz2WSX",
		"workspace_role": "viewer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserRejectsDeveloperActor(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "dev-actor", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "dev-team", Slug: "dev-team", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, map[string]any{
		"username":       "forbidden-by-developer",
		"password":       "1qaz2WSX",
		"workspace_role": "viewer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsWorkspaceOwnerRequiresExplicitWorkspaceHeader(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	owner := models.User{Username: "owner-explicit-header", Role: "user", Status: "active"}
	_ = owner.SetPassword("1qaz2WSX")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspaceA := models.Workspace{Name: "team-a", Slug: "team-a-explicit", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	workspaceB := models.Workspace{Name: "team-b", Slug: "team-b-explicit", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspace A failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspace B failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspaceA.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace A membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspaceB.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace B membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, owner.ID, "user", 0, "", map[string]any{
		"username":       "missing-current-workspace",
		"password":       "1qaz2WSX",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without explicit workspace header, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsWorkspaceOwnerRejectsInvalidSystemRoleInput(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	owner := models.User{Username: "owner-invalid-system-role", Role: "user", Status: "active"}
	_ = owner.SetPassword("1qaz2WSX")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "invalid-system-role", Slug: "invalid-system-role", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, owner.ID, "user", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "invalid-system-role-target",
		"password":       "1qaz2WSX",
		"system_role":    "root",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid system role, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsPlatformAdminRejectsInvalidWorkspaceRoleInput(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "admin-invalid-workspace-role", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "invalid-workspace-role", Slug: "invalid-workspace-role", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", workspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "invalid-workspace-role-target",
		"password":       "1qaz2WSX",
		"workspace_role": "supervisor",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid workspace role, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsPlatformAdminInAdminWorkspaceRejectsWorkspaceBinding(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "admin-reject-workspace-binding", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	adminWorkspace := models.Workspace{Name: "admin-governance", Slug: "admin-governance", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	normalWorkspace := models.Workspace{Name: "normal-target", Slug: "normal-target", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&adminWorkspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}
	if err := db.Create(&normalWorkspace).Error; err != nil {
		t.Fatalf("create normal workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", adminWorkspace.ID, models.WorkspaceRoleOwner, map[string]any{
		"username":       "mixed-scope-create-user",
		"password":       "1qaz2WSX",
		"workspace_id":   normalWorkspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mixed platform/workspace scope, got %d body=%s", w.Code, w.Body.String())
	}
}
