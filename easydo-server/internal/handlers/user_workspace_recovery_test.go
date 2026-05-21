package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestEnsurePersonalWorkspaceWithDB_IgnoresInactiveMembership(t *testing.T) {
	db := openHandlerTestDB(t)
	user := &models.User{Username: "recover-user", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	archived := models.Workspace{Name: "archived-only", Slug: "archived-only", Status: models.WorkspaceStatusArchived, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatalf("create archived workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: archived.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	workspace, err := ensurePersonalWorkspaceWithDB(db, user)
	if err != nil {
		t.Fatalf("ensurePersonalWorkspaceWithDB returned error: %v", err)
	}
	if workspace == nil {
		t.Fatalf("expected personal workspace to be created")
	}
	if workspace.Status != models.WorkspaceStatusActive {
		t.Fatalf("workspace status=%s, want active", workspace.Status)
	}
	if workspace.ID == archived.ID {
		t.Fatalf("expected new active personal workspace, got archived workspace id=%d", workspace.ID)
	}
}

func TestEnsurePersonalWorkspaceWithDB_CreatesAlphaNumericWorkspaceNameForUser(t *testing.T) {
	db := openHandlerTestDB(t)
	user := &models.User{Username: "CaseUser9", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace, err := ensurePersonalWorkspaceWithDB(db, user)
	if err != nil {
		t.Fatalf("ensurePersonalWorkspaceWithDB returned error: %v", err)
	}
	if workspace.Name != "CaseUser9Workspace" {
		t.Fatalf("workspace name=%s, want=CaseUser9Workspace", workspace.Name)
	}
	if workspace.Slug != workspace.Name {
		t.Fatalf("workspace slug=%s, want name=%s", workspace.Slug, workspace.Name)
	}
	if workspace.Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%s, want=%s", workspace.Kind, models.WorkspaceKindNormal)
	}
}

func setupHandlerWorkspaceTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	config.Init()
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
		mini.Close()
	})
	return mini
}

func TestEnsurePersonalWorkspaceWithDB_CreatesAdminWorkspaceForAdminUser(t *testing.T) {
	db := openHandlerTestDB(t)
	user := &models.User{Username: "admin", Role: "admin", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	workspace, err := ensurePersonalWorkspaceWithDB(db, user)
	if err != nil {
		t.Fatalf("ensurePersonalWorkspaceWithDB returned error: %v", err)
	}
	if workspace.Name != "AdminWorkspace" {
		t.Fatalf("workspace name=%s, want=AdminWorkspace", workspace.Name)
	}
	if workspace.Slug != workspace.Name {
		t.Fatalf("workspace slug=%s, want name=%s", workspace.Slug, workspace.Name)
	}
	if workspace.Kind != models.WorkspaceKindAdmin {
		t.Fatalf("workspace kind=%s, want=%s", workspace.Kind, models.WorkspaceKindAdmin)
	}
}

func TestEnsurePersonalWorkspaceWithDB_NonAdminIgnoresAdminWorkspaceMembership(t *testing.T) {
	db := openHandlerTestDB(t)
	user := &models.User{Username: "non-admin-admin-member", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	adminWorkspace := models.Workspace{Name: "platform-space", Slug: "platform-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: 999}
	if err := db.Create(&adminWorkspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: adminWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	workspace, err := ensurePersonalWorkspaceWithDB(db, user)
	if err != nil {
		t.Fatalf("ensurePersonalWorkspaceWithDB returned error: %v", err)
	}
	if workspace == nil {
		t.Fatalf("expected personal workspace to be created")
	}
	if workspace.ID == adminWorkspace.ID {
		t.Fatalf("expected non-admin personal workspace instead of admin workspace id=%d", workspace.ID)
	}
	if workspace.Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%s, want=%s", workspace.Kind, models.WorkspaceKindNormal)
	}
}

func TestEnsureAdminWorkspaceWithDB_InvalidatesCachedNonAdminResolutionOnKindPromotion(t *testing.T) {
	setupHandlerWorkspaceTestRedis(t)
	db := openHandlerTestDB(t)

	admin := &models.User{Username: "kind-promotion-admin", Role: "admin", Status: "active"}
	member := &models.User{Username: "kind-promotion-member", Role: "user", Status: "active"}
	for _, user := range []*models.User{admin, member} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "promoted-space", Slug: "promoted-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: admin.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create admin membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: member.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create member membership failed: %v", err)
	}

	resolvedWorkspace, resolvedMember, err := middleware.ResolveUserWorkspace(member.ID, workspace.ID)
	if err != nil {
		t.Fatalf("initial resolve failed: %v", err)
	}
	if resolvedWorkspace == nil || resolvedMember == nil {
		t.Fatalf("expected initial workspace resolution, got workspace=%v member=%v", resolvedWorkspace, resolvedMember)
	}

	if err := ensureAdminWorkspaceWithDB(db, admin); err != nil {
		t.Fatalf("ensureAdminWorkspaceWithDB returned error: %v", err)
	}

	resolvedWorkspace, resolvedMember, err = middleware.ResolveUserWorkspace(member.ID, workspace.ID)
	if err != nil {
		t.Fatalf("resolve after kind promotion failed: %v", err)
	}
	if resolvedWorkspace != nil || resolvedMember != nil {
		t.Fatalf("expected promoted admin workspace to become inaccessible immediately, got workspace=%v member=%v", resolvedWorkspace, resolvedMember)
	}
	if err := middleware.BumpWorkspaceAuthVersion(context.Background(), workspace.ID); err != nil {
		t.Fatalf("bump workspace auth version failed: %v", err)
	}
}

func TestGetUserInfo_NonAdminFiltersAdminWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	user := models.User{Username: "workspace-admin-filter-user", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	normal := models.Workspace{Name: "normal-space", Slug: "normal-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	adminWorkspace := models.Workspace{Name: "admin-space", Slug: "admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: 998}
	if err := db.Create(&adminWorkspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}
	if err := db.Create(&normal).Error; err != nil {
		t.Fatalf("create normal workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: adminWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create admin membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: normal.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create normal membership failed: %v", err)
	}

	resp := performGetUserInfoRequest(t, h, user.ID, "user", 0)
	if len(resp.Data.Workspaces) != 1 {
		t.Fatalf("expected only normal workspace in response, got=%d", len(resp.Data.Workspaces))
	}
	if resp.Data.Workspaces[0].ID != normal.ID {
		t.Fatalf("workspace id=%d, want=%d", resp.Data.Workspaces[0].ID, normal.ID)
	}
	if resp.Data.Workspaces[0].Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%s, want=%s", resp.Data.Workspaces[0].Kind, models.WorkspaceKindNormal)
	}
	if resp.Data.CurrentWorkspace.ID != normal.ID {
		t.Fatalf("current workspace id=%d, want=%d", resp.Data.CurrentWorkspace.ID, normal.ID)
	}
}

func TestGetUserInfo_NonAdminFiltersInactiveWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	user := models.User{Username: "workspace-filter-user", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	active := models.Workspace{Name: "active-space", Slug: "active-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	archived := models.Workspace{Name: "archived-space", Slug: "archived-space", Status: models.WorkspaceStatusArchived, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("create active workspace failed: %v", err)
	}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatalf("create archived workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: archived.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create archived membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: active.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create active membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/userinfo", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")

	h.GetUserInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Workspaces []struct {
				ID   uint64 `json:"id"`
				Name string `json:"name"`
				Role string `json:"role"`
			} `json:"workspaces"`
			CurrentWorkspace struct {
				ID   uint64 `json:"id"`
				Role string `json:"role"`
			} `json:"current_workspace"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.Workspaces) != 1 {
		t.Fatalf("expected only active workspace in response, got=%d body=%s", len(resp.Data.Workspaces), w.Body.String())
	}
	if resp.Data.Workspaces[0].ID != active.ID {
		t.Fatalf("workspace id=%d, want=%d", resp.Data.Workspaces[0].ID, active.ID)
	}
	if resp.Data.CurrentWorkspace.ID != active.ID {
		t.Fatalf("current workspace id=%d, want=%d", resp.Data.CurrentWorkspace.ID, active.ID)
	}
}
