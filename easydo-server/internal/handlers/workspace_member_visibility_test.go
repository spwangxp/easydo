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
				ID   uint64 `json:"id"`
				Kind string `json:"kind"`
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

func TestGetWorkspaceList_HidesAdminWorkspacesFromNonAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-admin-hidden-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	normalWorkspace := models.Workspace{Name: "visibleSpace", Slug: "visibleSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	adminWorkspace := models.Workspace{Name: "platformSpace", Slug: "platformSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: 999}
	for _, workspace := range []*models.Workspace{&adminWorkspace, &normalWorkspace} {
		if err := db.Create(workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: adminWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create admin membership failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: normalWorkspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create normal membership failed: %v", err)
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
				ID   uint64 `json:"id"`
				Kind string `json:"kind"`
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
	if resp.Data.List[0].ID != normalWorkspace.ID {
		t.Fatalf("visible workspace id=%d, want=%d body=%s", resp.Data.List[0].ID, normalWorkspace.ID, w.Body.String())
	}
	if resp.Data.List[0].Kind != models.WorkspaceKindNormal {
		t.Fatalf("visible workspace kind=%s, want=%s body=%s", resp.Data.List[0].Kind, models.WorkspaceKindNormal, w.Body.String())
	}
	if resp.Data.CurrentWorkspaceID != normalWorkspace.ID {
		t.Fatalf("current_workspace_id=%d, want=%d body=%s", resp.Data.CurrentWorkspaceID, normalWorkspace.ID, w.Body.String())
	}
}

func TestGetWorkspaceListWithoutPaginationParamsReturnsAllVisibleWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-list-all-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	for i := 0; i < 25; i++ {
		workspace := models.Workspace{Name: "visible-space-" + strconv.Itoa(i), Slug: "visible-space-" + strconv.Itoa(i), Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
		if err := db.Create(&workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
		member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}
		if err := db.Create(&member).Error; err != nil {
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
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if len(resp.Data.List) != 25 {
		t.Fatalf("visible workspaces=%d, want=25 body=%s", len(resp.Data.List), w.Body.String())
	}
}

func TestGetWorkspaceListRejectsInvalidPaginationParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	for _, rawQuery := range []string{"page=abc", "limit=oops", "page=0", "limit=-1", "limit=101"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces?"+rawQuery, nil)
		c.Set("user_id", uint64(1))
		c.Set("role", "user")

		h.GetWorkspaceList(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("query=%s status=%d body=%s", rawQuery, w.Code, w.Body.String())
		}
	}
}

func TestGetWorkspace_RejectsAdminWorkspaceAccessForNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-admin-reject-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "adminDetailSpace", Slug: "adminDetailSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: 999}
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

func TestWorkspaceGetReturnsSuccessShapeAfterUseCaseRefactor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-shape-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "shapeSpace", Slug: "shapeSpace", Description: "shape description", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")

	h.GetWorkspace(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID           uint64   `json:"id"`
			Name         string   `json:"name"`
			Description  string   `json:"description"`
			Status       string   `json:"status"`
			Visibility   string   `json:"visibility"`
			Kind         string   `json:"kind"`
			Role         string   `json:"role"`
			Capabilities []string `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 200 || resp.Data.ID != workspace.ID || resp.Data.Name != workspace.Name || resp.Data.Description != workspace.Description {
		t.Fatalf("unexpected success response: %+v body=%s", resp, w.Body.String())
	}
	if resp.Data.Role != models.WorkspaceRoleMaintainer {
		t.Fatalf("role=%s, want=%s body=%s", resp.Data.Role, models.WorkspaceRoleMaintainer, w.Body.String())
	}
	if len(resp.Data.Capabilities) == 0 {
		t.Fatalf("expected capabilities body=%s", w.Body.String())
	}
}

func TestWorkspaceGetMapsInternalUseCaseErrorToHTTP500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-db-error-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "dbErrorSpace", Slug: "dbErrorSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/1", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", user.ID)
	c.Set("role", "user")

	h.GetWorkspace(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 500 || resp.Message == "" {
		t.Fatalf("unexpected error response: %+v", resp)
	}
	if strings.Contains(strings.ToLower(resp.Message), "sql") || strings.Contains(strings.ToLower(resp.Message), "database") {
		t.Fatalf("response leaked internal detail: %s", resp.Message)
	}
}

func TestGetWorkspace_ReturnsWorkspaceKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	user := models.User{Username: "workspace-kind-detail-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "detailSpace", Slug: "detailSpace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
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

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID   uint64 `json:"id"`
			Kind string `json:"kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Data.ID != workspace.ID {
		t.Fatalf("workspace id=%d, want=%d body=%s", resp.Data.ID, workspace.ID, w.Body.String())
	}
	if resp.Data.Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%s, want=%s body=%s", resp.Data.Kind, models.WorkspaceKindNormal, w.Body.String())
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
