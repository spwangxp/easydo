package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestGetUserInfo_AdminSeesAllWorkspacesAsOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	admin := models.User{Username: "admin-all-workspaces", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspaceA := models.Workspace{Name: "workspace-a", Slug: "workspace-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 999}
	workspaceB := models.Workspace{Name: "workspace-b", Slug: "workspace-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: 998}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspace A failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspace B failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/userinfo", nil)
	c.Set("user_id", admin.ID)
	c.Set("role", "admin")
	c.Set("workspace_id", workspaceB.ID)

	h.GetUserInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"slug"`)) {
		t.Fatalf("userinfo response should not expose slug: %s", w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Workspaces []struct {
				ID           uint64   `json:"id"`
				Name         string   `json:"name"`
				Kind         string   `json:"kind"`
				Role         string   `json:"role"`
				Capabilities []string `json:"capabilities"`
			} `json:"workspaces"`
			CurrentWorkspace struct {
				ID   uint64 `json:"id"`
				Kind string `json:"kind"`
				Role string `json:"role"`
			} `json:"current_workspace"`
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("resp.code=%d body=%s", resp.Code, w.Body.String())
	}
	if len(resp.Data.Workspaces) < 3 {
		t.Fatalf("expected admin to see all workspaces including personal workspace, got=%d body=%s", len(resp.Data.Workspaces), w.Body.String())
	}
	var sawA, sawB bool
	for _, workspace := range resp.Data.Workspaces {
		if workspace.Role != models.WorkspaceRoleOwner {
			t.Fatalf("workspace %s role=%s, want owner", workspace.Name, workspace.Role)
		}
		if workspace.Kind == "" {
			t.Fatalf("workspace %s kind should not be empty", workspace.Name)
		}
		if workspace.ID == workspaceA.ID {
			sawA = true
			if workspace.Kind != models.WorkspaceKindNormal {
				t.Fatalf("workspace A kind=%s, want=%s", workspace.Kind, models.WorkspaceKindNormal)
			}
		}
		if workspace.ID == workspaceB.ID {
			sawB = true
			if workspace.Kind != models.WorkspaceKindNormal {
				t.Fatalf("workspace B kind=%s, want=%s", workspace.Kind, models.WorkspaceKindNormal)
			}
		}
	}
	if !sawA || !sawB {
		t.Fatalf("admin workspaces missing seeded targets: sawA=%v sawB=%v body=%s", sawA, sawB, w.Body.String())
	}
	if resp.Data.CurrentWorkspace.ID != workspaceB.ID {
		t.Fatalf("current workspace=%d, want=%d", resp.Data.CurrentWorkspace.ID, workspaceB.ID)
	}
	if resp.Data.CurrentWorkspace.Kind != models.WorkspaceKindNormal {
		t.Fatalf("current workspace kind=%s, want=%s", resp.Data.CurrentWorkspace.Kind, models.WorkspaceKindNormal)
	}
	if resp.Data.CurrentWorkspace.Role != models.WorkspaceRoleOwner {
		t.Fatalf("current workspace role=%s, want owner", resp.Data.CurrentWorkspace.Role)
	}
	permSet := make(map[string]bool, len(resp.Data.Permissions))
	for _, permission := range resp.Data.Permissions {
		permSet[permission] = true
	}
	if !permSet["workspace.delete"] || !permSet["agent.approve"] {
		t.Fatalf("expected owner permissions for admin current workspace, got=%v", resp.Data.Permissions)
	}
}

func TestGetUserInfo_AdminDefaultsToAdminWorkspaceWhenAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	admin := models.User{Username: "admin-default-admin-workspace", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	normalWorkspace := models.Workspace{Name: "team-space", Slug: "team-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&normalWorkspace).Error; err != nil {
		t.Fatalf("create normal workspace failed: %v", err)
	}
	adminWorkspace := models.Workspace{Name: "platform-governance", Slug: "platform-governance", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&adminWorkspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}

	resp := performGetUserInfoRequest(t, h, admin.ID, "admin", 0)
	if resp.Data.CurrentWorkspace.ID != adminWorkspace.ID {
		t.Fatalf("current workspace id=%d, want admin workspace id=%d", resp.Data.CurrentWorkspace.ID, adminWorkspace.ID)
	}
	if resp.Data.CurrentWorkspace.Kind != models.WorkspaceKindAdmin {
		t.Fatalf("current workspace kind=%s, want=%s", resp.Data.CurrentWorkspace.Kind, models.WorkspaceKindAdmin)
	}
}

func TestGetUserInfo_AdminPromotesOwnedNormalWorkspaceToAdminWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	admin := models.User{Username: "admin-promote-owned-workspace", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}

	workspace := models.Workspace{Name: "admin Workspace", Slug: "admin-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create normal admin-owned workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: admin.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	resp := performGetUserInfoRequest(t, h, admin.ID, "admin", 0)
	if resp.Data.CurrentWorkspace.ID != workspace.ID {
		t.Fatalf("current workspace id=%d, want=%d", resp.Data.CurrentWorkspace.ID, workspace.ID)
	}
	if resp.Data.CurrentWorkspace.Kind != models.WorkspaceKindAdmin {
		t.Fatalf("current workspace kind=%s, want=%s", resp.Data.CurrentWorkspace.Kind, models.WorkspaceKindAdmin)
	}

	var stored models.Workspace
	if err := db.First(&stored, workspace.ID).Error; err != nil {
		t.Fatalf("reload workspace failed: %v", err)
	}
	if stored.Kind != models.WorkspaceKindAdmin {
		t.Fatalf("stored workspace kind=%s, want=%s", stored.Kind, models.WorkspaceKindAdmin)
	}
}
