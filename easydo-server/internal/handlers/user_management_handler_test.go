package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedUserManagementHandlerPlatformContext(t *testing.T, db *gorm.DB) (models.User, models.Workspace) {
	t.Helper()
	admin := models.User{Username: "handler-um-admin", Email: "handler-um-admin@example.com", Role: "admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin failed: %v", err)
	}
	workspace := models.Workspace{Name: "handler-um-admin-space", Slug: "handler-um-admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create admin workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: admin.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: admin.ID}).Error; err != nil {
		t.Fatalf("create admin workspace membership failed: %v", err)
	}
	return admin, workspace
}

func TestUserManagementHandlerListUsersUsesServiceFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin, adminWorkspace := seedUserManagementHandlerPlatformContext(t, db)
	for _, user := range []models.User{
		{Username: "handler-um-alpha", Email: "alpha@example.com", Role: "user", Status: "active", Nickname: "Alpha"},
		{Username: "handler-um-beta", Email: "beta@example.com", Role: "user", Status: "active", Nickname: "Beta"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/users?q=alpha&page=1&page_size=20", nil)
	c.Set("user_id", admin.ID)
	c.Set("role", admin.Role)
	c.Set("workspace_id", adminWorkspace.ID)
	c.Set("workspace_kind", models.WorkspaceKindAdmin)
	c.Set("workspace_role", models.WorkspaceRoleOwner)

	h.GetUserList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if strings.Contains(body, `"password"`) {
		t.Fatalf("response leaked password field: %s", w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Total int `json:"total"`
			List  []struct {
				Username string `json:"username"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 || resp.Data.List[0].Username != "handler-um-alpha" {
		t.Fatalf("unexpected filtered response: %s", w.Body.String())
	}
}

func TestUserManagementHandlerDisableUserRevokesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupUserAuthTestEnv(t, 4*time.Hour, 10*time.Minute)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin, adminWorkspace := seedUserManagementHandlerPlatformContext(t, db)
	target := models.User{Username: "handler-um-disable", Email: "handler-um-disable@example.com", Role: "user", Status: "active"}
	if err := target.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	token, _, err := middleware.IssueTokenSession(context.Background(), &target)
	if err != nil {
		t.Fatalf("issue target token failed: %v", err)
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse target token failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/users/"+strconv.FormatUint(target.ID, 10)+"/disable", strings.NewReader(`{"reason":"left company"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(target.ID, 10)}}
	c.Set("user_id", admin.ID)
	c.Set("role", admin.Role)
	c.Set("workspace_id", adminWorkspace.ID)
	c.Set("workspace_kind", models.WorkspaceKindAdmin)
	c.Set("workspace_role", models.WorkspaceRoleOwner)

	h.DisableManagedUser(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if err := middleware.ValidateTokenSession(context.Background(), claims); err == nil {
		t.Fatal("expected disabled user session to be revoked")
	}
	var reloaded models.User
	if err := db.First(&reloaded, target.ID).Error; err != nil {
		t.Fatalf("reload target failed: %v", err)
	}
	if reloaded.Status != "disabled" || reloaded.DisabledAt == 0 {
		t.Fatalf("reloaded target=%+v, want disabled with timestamp", reloaded)
	}
	var auditCount int64
	if err := db.Model(&models.AuditLog{}).Where("action = ? AND target_id = ?", "user.disable", target.ID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count=%d, want 1", auditCount)
	}
}

func TestUserManagementHandlerAssignsAndRemovesWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	admin, adminWorkspace := seedUserManagementHandlerPlatformContext(t, db)
	target := models.User{Username: "handler-um-assign-target", Email: "handler-um-assign-target@example.com", Role: "user", Status: "active"}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	workspace := models.Workspace{Name: "handler-um-assign-space", Slug: "handler-um-assign-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	assignW := httptest.NewRecorder()
	assignC, _ := gin.CreateTestContext(assignW)
	assignC.Request = httptest.NewRequest(http.MethodPost, "/api/users/"+strconv.FormatUint(target.ID, 10)+"/workspaces", strings.NewReader(`{"workspace_id":`+strconv.FormatUint(workspace.ID, 10)+`,"role":"developer"}`))
	assignC.Request.Header.Set("Content-Type", "application/json")
	assignC.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(target.ID, 10)}}
	assignC.Set("user_id", admin.ID)
	assignC.Set("role", admin.Role)
	assignC.Set("workspace_id", adminWorkspace.ID)
	assignC.Set("workspace_kind", models.WorkspaceKindAdmin)
	assignC.Set("workspace_role", models.WorkspaceRoleOwner)

	h.AddManagedUserToWorkspace(assignC)

	if assignW.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", assignW.Code, assignW.Body.String())
	}
	var member models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, target.ID).First(&member).Error; err != nil {
		t.Fatalf("load assigned member failed: %v", err)
	}
	if member.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("assigned role=%s, want developer", member.Role)
	}

	removeW := httptest.NewRecorder()
	removeC, _ := gin.CreateTestContext(removeW)
	removeC.Request = httptest.NewRequest(http.MethodDelete, "/api/users/"+strconv.FormatUint(target.ID, 10)+"/workspaces/"+strconv.FormatUint(workspace.ID, 10), nil)
	removeC.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(target.ID, 10)}, {Key: "workspace_id", Value: strconv.FormatUint(workspace.ID, 10)}}
	removeC.Set("user_id", admin.ID)
	removeC.Set("role", admin.Role)
	removeC.Set("workspace_id", adminWorkspace.ID)
	removeC.Set("workspace_kind", models.WorkspaceKindAdmin)
	removeC.Set("workspace_role", models.WorkspaceRoleOwner)

	h.RemoveManagedUserFromWorkspace(removeC)

	if removeW.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removeW.Code, removeW.Body.String())
	}
	var count int64
	if err := db.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ?", workspace.ID, target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count removed member failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("member count=%d, want 0", count)
	}
}

func TestWorkspaceHandlerOwnerAddsExistingUserMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "handler-ws-owner", Email: "handler-ws-owner@example.com", Role: "user", Status: "active"}
	target := models.User{Username: "handler-ws-target", Email: "handler-ws-target@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	workspace := models.Workspace{Name: "handler-ws-space", Slug: "handler-ws-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create owner member failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/members", strings.NewReader(`{"user_id":`+strconv.FormatUint(target.ID, 10)+`,"role":"developer"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", owner.ID)
	c.Set("role", owner.Role)
	c.Set("workspace_id", workspace.ID)
	c.Set("workspace_kind", models.WorkspaceKindNormal)
	c.Set("workspace_role", models.WorkspaceRoleOwner)

	h.AddExistingMember(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var member models.WorkspaceMember
	if err := db.Where("workspace_id = ? AND user_id = ?", workspace.ID, target.ID).First(&member).Error; err != nil {
		t.Fatalf("load added member failed: %v", err)
	}
	if member.Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("member role=%s, want developer", member.Role)
	}
}

func TestWorkspaceHandlerSearchesExistingMemberCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}
	owner := models.User{Username: "handler-ws-candidate-owner", Email: "handler-ws-candidate-owner@example.com", Role: "user", Status: "active"}
	candidate := models.User{Username: "candidate-alpha", Email: "candidate-alpha@example.com", Phone: "13900000001", Role: "user", Status: "active", Nickname: "候选用户"}
	emailMatch := models.User{Username: "other-user", Email: "find-by-mail-alpha@example.com", Phone: "13900000002", Role: "user", Status: "active"}
	phoneMatch := models.User{Username: "phone-user", Email: "phone-user@example.com", Phone: "13900000003", Role: "user", Status: "active"}
	existing := models.User{Username: "existing-alpha", Email: "existing-alpha@example.com", Phone: "13900000004", Role: "user", Status: "active"}
	disabled := models.User{Username: "disabled-alpha", Email: "disabled-alpha@example.com", Role: "user", Status: "disabled"}
	platformAdmin := models.User{Username: "admin-alpha", Email: "admin-alpha@example.com", Role: "admin", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	for _, user := range []*models.User{&candidate, &emailMatch, &phoneMatch, &existing, &disabled, &platformAdmin} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user %s failed: %v", user.Username, err)
		}
	}
	workspace := models.Workspace{Name: "handler-ws-candidate-space", Slug: "handler-ws-candidate-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	for _, member := range []models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
		{WorkspaceID: workspace.ID, UserID: existing.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
	} {
		if err := db.Create(&member).Error; err != nil {
			t.Fatalf("create workspace member failed: %v", err)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/members/candidates?q=alpha", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", owner.ID)
	c.Set("role", owner.Role)
	c.Set("workspace_id", workspace.ID)
	c.Set("workspace_kind", models.WorkspaceKindNormal)
	c.Set("workspace_role", models.WorkspaceRoleOwner)

	h.SearchMemberCandidates(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Total int `json:"total"`
			List  []struct {
				ID       uint64 `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
				Phone    string `json:"phone"`
				Status   string `json:"status"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	usernames := make([]string, 0, len(resp.Data.List))
	for _, item := range resp.Data.List {
		usernames = append(usernames, item.Username)
	}
	if resp.Data.Total != 2 || !slices.Contains(usernames, candidate.Username) || !slices.Contains(usernames, emailMatch.Username) {
		t.Fatalf("alpha candidates total=%d usernames=%v body=%s", resp.Data.Total, usernames, w.Body.String())
	}
	if slices.Contains(usernames, existing.Username) || slices.Contains(usernames, disabled.Username) || slices.Contains(usernames, platformAdmin.Username) {
		t.Fatalf("candidates included existing, disabled, or platform admin users: %v", usernames)
	}

	phoneW := httptest.NewRecorder()
	phoneC, _ := gin.CreateTestContext(phoneW)
	phoneC.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/members/candidates?q=0003", nil)
	phoneC.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	phoneC.Set("user_id", owner.ID)
	phoneC.Set("role", owner.Role)
	phoneC.Set("workspace_id", workspace.ID)
	phoneC.Set("workspace_kind", models.WorkspaceKindNormal)
	phoneC.Set("workspace_role", models.WorkspaceRoleOwner)

	h.SearchMemberCandidates(phoneC)

	if phoneW.Code != http.StatusOK {
		t.Fatalf("phone status=%d body=%s", phoneW.Code, phoneW.Body.String())
	}
	if !strings.Contains(phoneW.Body.String(), phoneMatch.Username) {
		t.Fatalf("phone search did not include %s: %s", phoneMatch.Username, phoneW.Body.String())
	}
}
