package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func seedAuditLog(t *testing.T, db *gorm.DB, workspaceID uint64, actorID uint64, action string, targetID uint64) models.AuditLog {
	t.Helper()
	log := models.AuditLog{
		WorkspaceID: &workspaceID,
		ActorUserID: actorID,
		ActorRole:   "user",
		Action:      action,
		TargetType:  "user",
		TargetID:    targetID,
	}
	if err := db.Create(&log).Error; err != nil {
		t.Fatalf("create audit log failed: %v", err)
	}
	return log
}

func TestAuditLogHandlerPlatformAdminListsGlobalLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AuditLogHandler{DB: db}
	admin, adminWorkspace := seedUserManagementHandlerPlatformContext(t, db)
	workspaceA := models.Workspace{Name: "audit-a", Slug: "audit-a", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	workspaceB := models.Workspace{Name: "audit-b", Slug: "audit-b", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: admin.ID}
	if err := db.Create(&workspaceA).Error; err != nil {
		t.Fatalf("create workspace a failed: %v", err)
	}
	if err := db.Create(&workspaceB).Error; err != nil {
		t.Fatalf("create workspace b failed: %v", err)
	}
	seedAuditLog(t, db, workspaceA.ID, admin.ID, "user.disable", 101)
	seedAuditLog(t, db, workspaceB.ID, admin.ID, "workspace_member.add", 102)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/audit-logs?page=1&page_size=20", nil)
	setNotificationSenderContext(c, admin, adminWorkspace, models.WorkspaceRoleOwner)

	h.ListGlobalAuditLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total int64             `json:"total"`
			List  []models.AuditLog `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Total != 2 || len(resp.Data.List) != 2 {
		t.Fatalf("unexpected global audit response: %s", w.Body.String())
	}
}

func TestAuditLogHandlerWorkspaceOwnerListsCurrentWorkspaceOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AuditLogHandler{DB: db}
	owner := models.User{Username: "audit-owner", Email: "audit-owner@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := seedNotificationSenderWorkspace(t, db, owner, "audit-owner-workspace")
	otherWorkspace := models.Workspace{Name: "audit-other", Slug: "audit-other", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&otherWorkspace).Error; err != nil {
		t.Fatalf("create other workspace failed: %v", err)
	}
	seedAuditLog(t, db, workspace.ID, owner.ID, "user.update", 201)
	seedAuditLog(t, db, otherWorkspace.ID, owner.ID, "user.disable", 202)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/audit-logs", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	setNotificationSenderContext(c, owner, workspace, models.WorkspaceRoleOwner)

	h.ListWorkspaceAuditLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Total int64             `json:"total"`
			List  []models.AuditLog `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Total != 1 || len(resp.Data.List) != 1 || resp.Data.List[0].Action != "user.update" {
		t.Fatalf("unexpected workspace audit response: %s", w.Body.String())
	}
}

func TestAuditLogHandlerRejectsWorkspaceMaintainer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &AuditLogHandler{DB: db}
	owner := models.User{Username: "audit-maint-owner", Email: "audit-maint-owner@example.com", Role: "user", Status: "active"}
	maintainer := models.User{Username: "audit-maintainer", Email: "audit-maintainer@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	if err := db.Create(&maintainer).Error; err != nil {
		t.Fatalf("create maintainer failed: %v", err)
	}
	workspace := seedNotificationSenderWorkspace(t, db, owner, "audit-maintainer-workspace")
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: maintainer.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create maintainer member failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/audit-logs", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	setNotificationSenderContext(c, maintainer, workspace, models.WorkspaceRoleMaintainer)

	h.ListWorkspaceAuditLogs(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", w.Code, w.Body.String())
	}
}
