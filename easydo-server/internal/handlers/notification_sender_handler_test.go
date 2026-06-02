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
	"gorm.io/gorm"
)

func seedNotificationSenderWorkspace(t *testing.T, db *gorm.DB, owner models.User, slug string) models.Workspace {
	t.Helper()
	workspace := models.Workspace{Name: slug, Slug: slug, Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create owner member failed: %v", err)
	}
	return workspace
}

func setNotificationSenderContext(c *gin.Context, user models.User, workspace models.Workspace, workspaceRole string) {
	c.Set("user_id", user.ID)
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set("workspace_id", workspace.ID)
	c.Set("workspace_kind", workspace.Kind)
	c.Set("workspace_role", workspaceRole)
}

func TestNotificationSenderHandlerSavesPlatformConfigForAdminWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}
	admin, adminWorkspace := seedUserManagementHandlerPlatformContext(t, db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/notification-senders/platform", strings.NewReader(`{
		"enabled": true,
		"from_name": "EasyDo Platform",
		"from_address": "platform@example.com",
		"smtp_host": "smtp.platform.example.com",
		"smtp_port": 587,
		"smtp_username": "platform@example.com",
		"smtp_password": "smtp-secret",
		"smtp_tls_mode": "starttls"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	setNotificationSenderContext(c, admin, adminWorkspace, models.WorkspaceRoleOwner)

	h.SavePlatformNotificationSender(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "smtp-secret") {
		t.Fatalf("response leaked smtp password: %s", w.Body.String())
	}
	var resp struct {
		Data struct {
			Scope                  string `json:"scope"`
			WorkspaceID            uint64 `json:"workspace_id"`
			FromAddress            string `json:"from_address"`
			SMTPPasswordConfigured bool   `json:"smtp_password_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Scope != models.NotificationSenderScopePlatform || resp.Data.WorkspaceID != 0 || resp.Data.FromAddress != "platform@example.com" || !resp.Data.SMTPPasswordConfigured {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	var auditCount int64
	if err := db.Model(&models.AuditLog{}).Where("action = ?", "notification_sender.save").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit logs failed: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count=%d, want 1", auditCount)
	}
}

func TestNotificationSenderHandlerSavesWorkspaceConfigForOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}
	owner := models.User{Username: "sender-owner", Email: "sender-owner@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := seedNotificationSenderWorkspace(t, db, owner, "sender-owner-workspace")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/notification-sender", strings.NewReader(`{
		"enabled": true,
		"from_name": "Workspace Mail",
		"from_address": "workspace@example.com",
		"smtp_host": "smtp.workspace.example.com",
		"smtp_port": 2525,
		"smtp_password": "workspace-secret"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	setNotificationSenderContext(c, owner, workspace, models.WorkspaceRoleOwner)

	h.SaveWorkspaceNotificationSender(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.NotificationSenderConfig
	if err := db.Where("scope = ? AND workspace_id = ?", models.NotificationSenderScopeWorkspace, workspace.ID).First(&saved).Error; err != nil {
		t.Fatalf("load saved workspace sender failed: %v", err)
	}
	if saved.FromAddress != "workspace@example.com" || saved.SMTPPasswordEncrypted == "" || strings.Contains(saved.SMTPPasswordEncrypted, "workspace-secret") {
		t.Fatalf("unexpected saved sender: %+v", saved)
	}
}

func TestNotificationSenderHandlerRejectsWorkspaceMaintainer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}
	owner := models.User{Username: "sender-owner-for-maintainer", Email: "sender-owner-for-maintainer@example.com", Role: "user", Status: "active"}
	maintainer := models.User{Username: "sender-maintainer", Email: "sender-maintainer@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	if err := db.Create(&maintainer).Error; err != nil {
		t.Fatalf("create maintainer failed: %v", err)
	}
	workspace := seedNotificationSenderWorkspace(t, db, owner, "sender-maintainer-workspace")
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: maintainer.ID, Role: models.WorkspaceRoleMaintainer, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create maintainer member failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/notification-sender", strings.NewReader(`{"enabled":true,"from_address":"workspace@example.com","smtp_host":"smtp.workspace.example.com","smtp_port":25}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	setNotificationSenderContext(c, maintainer, workspace, models.WorkspaceRoleMaintainer)

	h.SaveWorkspaceNotificationSender(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", w.Code, w.Body.String())
	}
}

func TestNotificationSenderHandlerGetsEffectiveConfigForCurrentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}
	owner := models.User{Username: "sender-effective-owner", Email: "sender-effective-owner@example.com", Role: "user", Status: "active"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := seedNotificationSenderWorkspace(t, db, owner, "sender-effective-workspace")
	encryptedPassword, err := models.EncryptCredentialPayload("workspace-secret")
	if err != nil {
		t.Fatalf("encrypt password failed: %v", err)
	}
	if err := db.Create(&models.NotificationSenderConfig{
		Scope:                 models.NotificationSenderScopeWorkspace,
		WorkspaceID:           workspace.ID,
		Enabled:               true,
		FromName:              "Workspace Mail",
		FromAddress:           "workspace@example.com",
		SMTPHost:              "smtp.workspace.example.com",
		SMTPPort:              2525,
		SMTPPasswordEncrypted: encryptedPassword,
		SMTPTLSMode:           "plain",
		UpdatedBy:             owner.ID,
	}).Error; err != nil {
		t.Fatalf("create sender config failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/notification-senders/effective", nil)
	setNotificationSenderContext(c, owner, workspace, models.WorkspaceRoleOwner)

	h.GetEffectiveNotificationSender(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "workspace-secret") {
		t.Fatalf("response leaked smtp password: %s", w.Body.String())
	}
	var resp struct {
		Data struct {
			Scope                  string `json:"scope"`
			WorkspaceID            uint64 `json:"workspace_id"`
			FromAddress            string `json:"from_address"`
			SMTPPasswordConfigured bool   `json:"smtp_password_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Data.Scope != models.NotificationSenderScopeWorkspace || resp.Data.WorkspaceID != workspace.ID || resp.Data.FromAddress != "workspace@example.com" || !resp.Data.SMTPPasswordConfigured {
		t.Fatalf("unexpected effective response: %s", w.Body.String())
	}
}
