package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestListMembers_HidesPlatformAdminsFromPlatformUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	viewer := models.User{Username: "viewer-user", Role: "user", Status: "active"}
	admin := models.User{Username: "platform-admin", Role: "admin", Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-space", Slug: "team-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: viewer.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: viewer.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: viewer.ID}).Error; err != nil {
		t.Fatalf("create viewer membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: admin.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: admin.ID}).Error; err != nil {
		t.Fatalf("create admin membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/1/members", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", viewer.ID)
	c.Set("role", "user")

	h.ListMembers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Username   string `json:"username"`
				SystemRole string `json:"system_role"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("expected only non-admin member, got=%d body=%s", len(resp.Data.List), w.Body.String())
	}
	if resp.Data.List[0].Username != viewer.Username {
		t.Fatalf("visible username=%s, want=%s", resp.Data.List[0].Username, viewer.Username)
	}
}

func TestListMembers_PlatformAdminCanStillSeePlatformAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	viewer := models.User{Username: "viewer-user", Role: "user", Status: "active"}
	admin := models.User{Username: "platform-admin", Role: "admin", Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-space", Slug: "team-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: viewer.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: viewer.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: viewer.ID}).Error; err != nil {
		t.Fatalf("create viewer membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: admin.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: admin.ID}).Error; err != nil {
		t.Fatalf("create admin membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/1/members", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", admin.ID)
	c.Set("role", "admin")

	h.ListMembers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Username   string `json:"username"`
				SystemRole string `json:"system_role"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.List) != 2 {
		t.Fatalf("expected both members visible to admin, got=%d body=%s", len(resp.Data.List), w.Body.String())
	}
	var sawPlatformAdmin bool
	for _, member := range resp.Data.List {
		if member.SystemRole == "admin" {
			sawPlatformAdmin = true
		}
	}
	if !sawPlatformAdmin {
		t.Fatalf("expected platform admin row visible to platform admin, body=%s", w.Body.String())
	}
}

func TestUpdateMember_RequiresWorkspaceGovernance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	maintainer := models.User{Username: "maintainer-user", Role: "user", Status: "active"}
	target := models.User{Username: "target-user", Role: "user", Status: "active"}
	for _, user := range []*models.User{&maintainer, &target} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "governed-space", Slug: "governed-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: maintainer.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	maintainerMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: maintainer.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: maintainer.ID}
	targetMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: target.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: maintainer.ID}
	for _, member := range []*models.WorkspaceMember{&maintainerMember, &targetMember} {
		if err := db.Create(member).Error; err != nil {
			t.Fatalf("create member failed: %v", err)
		}
	}

	body := strings.NewReader(`{"role":"developer"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/workspaces/1/members/1", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}, {Key: "member_id", Value: strconv.FormatUint(targetMember.ID, 10)}}
	c.Set("user_id", maintainer.ID)
	c.Set("role", "user")

	h.UpdateMember(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var reloaded models.WorkspaceMember
	if err := db.First(&reloaded, targetMember.ID).Error; err != nil {
		t.Fatalf("reload target membership failed: %v", err)
	}
	if reloaded.Role != models.WorkspaceRoleViewer {
		t.Fatalf("target role=%s, want=%s", reloaded.Role, models.WorkspaceRoleViewer)
	}
}

func TestGetWorkspaceList_HidesArchivedWorkspacesFromMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-list-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	activeWorkspace := models.Workspace{Name: "activeSpace", Slug: "activeSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	archivedWorkspace := models.Workspace{Name: "archivedSpace", Slug: "archivedSpace", Status: models.WorkspaceStatusArchived, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	for _, workspace := range []*models.Workspace{&activeWorkspace, &archivedWorkspace} {
		if err := db.Create(workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
	}
	for _, workspaceID := range []uint64{activeWorkspace.ID, archivedWorkspace.ID} {
		if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspaceID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
			t.Fatalf("create membership failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	c.Set("user_id", user.ID)
	c.Set("role", "user")

	h.GetWorkspaceList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID uint64 `json:"id"`
			} `json:"list"`
			CurrentWorkspaceID uint64 `json:"current_workspace_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.List) != 1 {
		t.Fatalf("visible workspaces=%d, want=1 body=%s", len(resp.Data.List), w.Body.String())
	}
	if resp.Data.List[0].ID != activeWorkspace.ID {
		t.Fatalf("visible workspace id=%d, want=%d body=%s", resp.Data.List[0].ID, activeWorkspace.ID, w.Body.String())
	}
	if resp.Data.CurrentWorkspaceID != activeWorkspace.ID {
		t.Fatalf("current_workspace_id=%d, want=%d body=%s", resp.Data.CurrentWorkspaceID, activeWorkspace.ID, w.Body.String())
	}
}

func TestGetWorkspace_RejectsArchivedWorkspaceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-detail-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "archivedDetailSpace", Slug: "archivedDetailSpace", Status: models.WorkspaceStatusArchived, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")

	h.GetWorkspace(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
