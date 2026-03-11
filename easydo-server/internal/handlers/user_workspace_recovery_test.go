package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
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
