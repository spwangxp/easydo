package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

type getUserInfoWorkspaceEntry struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Role string `json:"role"`
}

type getUserInfoResponse struct {
	Code int `json:"code"`
	Data struct {
		Workspaces       []getUserInfoWorkspaceEntry `json:"workspaces"`
		CurrentWorkspace getUserInfoWorkspaceEntry   `json:"current_workspace"`
	} `json:"data"`
}

func TestGetUserInfo_AdminRequestedAdminWorkspaceReturnsAdminCurrentContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	admin := models.User{Username: "admin-admin-context", Role: "admin", Status: "active"}
	if err := admin.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "platform-governance", Slug: "platform-governance", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}

	resp := performGetUserInfoRequest(t, h, admin.ID, "admin", workspace.ID)
	if resp.Data.CurrentWorkspace.ID != workspace.ID {
		t.Fatalf("current workspace id=%d, want=%d", resp.Data.CurrentWorkspace.ID, workspace.ID)
	}
	if resp.Data.CurrentWorkspace.Kind != models.WorkspaceKindAdmin {
		t.Fatalf("current workspace kind=%s, want=%s", resp.Data.CurrentWorkspace.Kind, models.WorkspaceKindAdmin)
	}
}

func TestGetUserInfo_NonAdminReturnsWorkspaceKinds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}

	user := models.User{Username: "workspace-kind-user", Role: "user", Status: "active"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-space", Slug: "team-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: "", CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Model(&models.Workspace{}).Where("id = ?", workspace.ID).Update("kind", "").Error; err != nil {
		t.Fatalf("blank workspace kind failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	resp := performGetUserInfoRequest(t, h, user.ID, "user", workspace.ID)
	if len(resp.Data.Workspaces) != 1 {
		t.Fatalf("workspaces=%d, want=1", len(resp.Data.Workspaces))
	}
	if resp.Data.Workspaces[0].Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%s, want=%s", resp.Data.Workspaces[0].Kind, models.WorkspaceKindNormal)
	}
	if resp.Data.CurrentWorkspace.Kind != models.WorkspaceKindNormal {
		t.Fatalf("current workspace kind=%s, want=%s", resp.Data.CurrentWorkspace.Kind, models.WorkspaceKindNormal)
	}
}

func performGetUserInfoRequest(t *testing.T, h *UserHandler, userID uint64, role string, workspaceID uint64) getUserInfoResponse {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/userinfo", nil)
	c.Set("user_id", userID)
	c.Set("role", role)
	if workspaceID != 0 {
		c.Set("workspace_id", workspaceID)
	}

	h.GetUserInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp getUserInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 200 {
		t.Fatalf("resp.code=%d body=%s", resp.Code, w.Body.String())
	}
	return resp
}
