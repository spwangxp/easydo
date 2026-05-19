package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRemoveLastOwnerDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "last-owner-remove", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "last-owner-remove-space", Slug: "last-owner-remove-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/workspaces/1/members/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}, {Key: "member_id", Value: strconv.FormatUint(member.ID, 10)}}
	c.Set("user_id", owner.ID)
	c.Set("role", "user")

	h.RemoveMember(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.WorkspaceMember{}).Where("id = ?", member.ID).Count(&count).Error; err != nil {
		t.Fatalf("count owner membership failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("owner membership count=%d, want=1", count)
	}
}

func TestDemoteLastOwnerDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "last-owner-demote", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "last-owner-demote-space", Slug: "last-owner-demote-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/workspaces/1/members/1", strings.NewReader(`{"role":"developer"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}, {Key: "member_id", Value: strconv.FormatUint(member.ID, 10)}}
	c.Set("user_id", owner.ID)
	c.Set("role", "user")

	h.UpdateMember(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var reloaded models.WorkspaceMember
	if err := db.First(&reloaded, member.ID).Error; err != nil {
		t.Fatalf("reload owner membership failed: %v", err)
	}
	if reloaded.Role != models.WorkspaceRoleOwner {
		t.Fatalf("owner role=%s, want=%s", reloaded.Role, models.WorkspaceRoleOwner)
	}
}

func TestPlatformAdminRemoveLastOwnerDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	admin := models.User{Username: "platform-admin-last-owner", Role: "admin", Status: "active"}
	owner := models.User{Username: "workspace-owner-last-owner", Role: "user", Status: "active"}
	for _, user := range []*models.User{&admin, &owner} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "platform-admin-last-owner-space", Slug: "platform-admin-last-owner-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/workspaces/1/members/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}, {Key: "member_id", Value: strconv.FormatUint(member.ID, 10)}}
	c.Set("user_id", admin.ID)
	c.Set("role", "admin")

	h.RemoveMember(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.WorkspaceMember{}).Where("id = ?", member.ID).Count(&count).Error; err != nil {
		t.Fatalf("count owner membership failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("owner membership count=%d, want=1", count)
	}
}
