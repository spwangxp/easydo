package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
