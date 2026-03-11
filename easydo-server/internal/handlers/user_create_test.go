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

func performCreateUserRequest(t *testing.T, h *UserHandler, actorID uint64, actorRole string, workspaceID uint64, workspaceRole string, payload map[string]interface{}) *httptest.ResponseRecorder {
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

func TestCreateUserAsPlatformAdminWithWorkspaceBinding(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "shared", Slug: "shared-admin", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", 0, "", map[string]interface{}{
		"username":       "created-by-admin",
		"password":       "1qaz2WSX",
		"email":          "created-admin@example.com",
		"nickname":       "Created By Admin",
		"system_role":    "user",
		"workspace_id":   workspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "created-by-admin").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	var workspaceMember models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, created.ID).First(&workspaceMember).Error; err != nil {
		t.Fatalf("load workspace member failed: %v", err)
	}
	if workspaceMember.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("workspace role=%s, want developer", workspaceMember.Role)
	}
	var personalWorkspace models.Workspace
	if err := db.Where("created_by = ?", created.ID).First(&personalWorkspace).Error; err != nil {
		t.Fatalf("personal workspace not created: %v", err)
	}
}

func TestCreateUserAsPlatformAdminWithoutAdditionalWorkspaceBinding(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin := models.User{Username: "platform-admin-no-binding", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set admin password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", 0, "", map[string]interface{}{
		"username":    "admin-created-no-binding",
		"password":    "1qaz2WSX",
		"email":       "admin-created-no-binding@example.com",
		"nickname":    "No Binding",
		"system_role": "user",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "admin-created-no-binding").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	var extraMembershipCount int64
	db.Model(&models.WorkspaceMember{}).Where("user_id = ?", created.ID).Count(&extraMembershipCount)
	if extraMembershipCount != 1 {
		t.Fatalf("membership count=%d, want=1 personal workspace only", extraMembershipCount)
	}
}

func TestCreateUserAsWorkspaceMaintainerBindsCurrentWorkspaceOnly(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "maintainer-user", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "team", Slug: "team-1", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]interface{}{
		"username":       "created-by-maintainer",
		"password":       "1qaz2WSX",
		"email":          "created-by-maintainer@example.com",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var created models.User
	if err := db.Where("username = ?", "created-by-maintainer").First(&created).Error; err != nil {
		t.Fatalf("load created user failed: %v", err)
	}
	var workspaceMember models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, created.ID).First(&workspaceMember).Error; err != nil {
		t.Fatalf("workspace membership missing: %v", err)
	}
	if created.Role != "user" {
		t.Fatalf("system role=%s, want user", created.Role)
	}
}

func TestCreateUserAsMaintainerRejectsOtherWorkspaceAndHighRoles(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "maintainer-blocked", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-current", Slug: "team-current", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	otherWorkspace := models.Workspace{Name: "team-other", Slug: "team-other", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&otherWorkspace).Error; err != nil {
		t.Fatalf("create other workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]interface{}{
		"username":       "blocked-other-workspace",
		"password":       "1qaz2WSX",
		"workspace_id":   otherWorkspace.ID,
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for other workspace, got %d body=%s", w.Code, w.Body.String())
	}

	w = performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]interface{}{
		"username":       "blocked-maintainer-role",
		"password":       "1qaz2WSX",
		"workspace_role": "maintainer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for maintainer role, got %d body=%s", w.Code, w.Body.String())
	}

	w = performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]interface{}{
		"username":       "blocked-system-admin",
		"password":       "1qaz2WSX",
		"system_role":    "admin",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for system admin role, got %d body=%s", w.Code, w.Body.String())
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
	workspace := models.Workspace{Name: "dev-team", Slug: "dev-team", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleDeveloper, map[string]interface{}{
		"username":       "forbidden-by-developer",
		"password":       "1qaz2WSX",
		"workspace_role": "viewer",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsMaintainerRequiresExplicitWorkspaceHeader(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "maintainer-explicit-header", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspaceA := models.Workspace{Name: "team-a", Slug: "team-a-explicit", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	workspaceB := models.Workspace{Name: "team-b", Slug: "team-b-explicit", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspace A failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspace B failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspaceA.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: actor.ID}).Error; err != nil {
		t.Fatalf("create workspace A membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspaceB.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: actor.ID}).Error; err != nil {
		t.Fatalf("create workspace B membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", 0, "", map[string]interface{}{
		"username":       "missing-current-workspace",
		"password":       "1qaz2WSX",
		"workspace_role": "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without explicit workspace header, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateUserAsMaintainerRejectsInvalidSystemRoleInput(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	actor := models.User{Username: "maintainer-invalid-system-role", Role: "user", Status: "active"}
	_ = actor.SetPassword("1qaz2WSX")
	if err := db.Create(&actor).Error; err != nil {
		t.Fatalf("create actor failed: %v", err)
	}
	workspace := models.Workspace{Name: "invalid-system-role", Slug: "invalid-system-role", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: actor.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: actor.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: actor.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := performCreateUserRequest(t, h, actor.ID, "user", workspace.ID, models.WorkspaceRoleMaintainer, map[string]interface{}{
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
	workspace := models.Workspace{Name: "invalid-workspace-role", Slug: "invalid-workspace-role", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	w := performCreateUserRequest(t, h, admin.ID, "admin", 0, "", map[string]interface{}{
		"username":       "invalid-workspace-role-target",
		"password":       "1qaz2WSX",
		"workspace_id":   workspace.ID,
		"workspace_role": "supervisor",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid workspace role, got %d body=%s", w.Code, w.Body.String())
	}
}
