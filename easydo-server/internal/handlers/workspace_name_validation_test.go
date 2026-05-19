package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func performCreateWorkspaceRequest(t *testing.T, h *WorkspaceHandler, actorID uint64, actorRole string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", actorID)
	c.Set("role", actorRole)
	h.CreateWorkspace(c)
	return w
}

func performUpdateWorkspaceRequest(t *testing.T, h *WorkspaceHandler, actorID uint64, actorRole string, workspaceID uint64, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspaceID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+strconv.FormatUint(workspaceID, 10), bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", actorID)
	c.Set("role", actorRole)
	h.UpdateWorkspace(c)
	return w
}

func TestCreateWorkspace_RejectsNameWithSpace(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "workspace-owner", Role: "admin", Status: "active"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}

	w := performCreateWorkspaceRequest(t, h, owner.ID, owner.Role, map[string]any{
		"name": "Team One",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("工作区名称只允许英文字母和数字")) {
		t.Fatalf("unexpected body=%s", w.Body.String())
	}
}

func TestCreateWorkspace_RejectsNameWithLeadingWhitespace(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "workspace-owner-leading-space", Role: "admin", Status: "active"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}

	w := performCreateWorkspaceRequest(t, h, owner.ID, owner.Role, map[string]any{
		"name": " TeamOne9",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("工作区名称只允许英文字母和数字")) {
		t.Fatalf("unexpected body=%s", w.Body.String())
	}
}

func TestCreateWorkspace_CreatesAlphaNumericNameWithoutSlugInput(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "workspaceowner", Role: "admin", Status: "active"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}

	w := performCreateWorkspaceRequest(t, h, owner.ID, owner.Role, map[string]any{
		"name": "TeamOne9",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"slug"`)) {
		t.Fatalf("create response should not expose slug: %s", w.Body.String())
	}

	var workspace models.Workspace
	if err := db.Where("name = ?", "TeamOne9").First(&workspace).Error; err != nil {
		t.Fatalf("load workspace failed: %v", err)
	}
	if workspace.Name != "TeamOne9" {
		t.Fatalf("workspace name=%s, want=TeamOne9", workspace.Name)
	}
	if workspace.Slug != workspace.Name {
		t.Fatalf("workspace slug=%s, want name=%s", workspace.Slug, workspace.Name)
	}
}

func TestUpdateWorkspace_RenamesSlugWithNameAndRejectsWhitespace(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "workspace-editor", Role: "admin", Status: "active"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{
		Name:       "TeamOne9",
		Slug:       "TeamOne9",
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		CreatedBy:  owner.ID,
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Role:        models.WorkspaceRoleOwner,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   owner.ID,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	bad := performUpdateWorkspaceRequest(t, h, owner.ID, owner.Role, workspace.ID, map[string]any{
		"name": "Team One9",
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", bad.Code, bad.Body.String())
	}

	good := performUpdateWorkspaceRequest(t, h, owner.ID, owner.Role, workspace.ID, map[string]any{
		"name": "TeamTwo9",
	})
	if good.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", good.Code, good.Body.String())
	}

	var updated models.Workspace
	if err := db.First(&updated, workspace.ID).Error; err != nil {
		t.Fatalf("load updated workspace failed: %v", err)
	}
	if updated.Name != "TeamTwo9" {
		t.Fatalf("workspace name=%s, want=TeamTwo9", updated.Name)
	}
	if updated.Slug != updated.Name {
		t.Fatalf("workspace slug=%s, want name=%s", updated.Slug, updated.Name)
	}
}
