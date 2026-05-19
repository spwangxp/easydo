package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAcceptInvitationRevalidatesAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "inviter-owner", Role: "user", Status: "active", Email: "inviter-owner@example.com"}
	invitee := models.User{Username: "invitee-user", Role: "user", Status: "active", Email: "invitee-user@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-revalidate-space", Slug: "invite-revalidate-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	ownerMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&ownerMember).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}
	_, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleDeveloper, TokenHash: tokenHash, Status: models.WorkspaceInvitationStatusPending, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}
	if err := db.Model(&ownerMember).Update("role", models.WorkspaceRoleDeveloper).Error; err != nil {
		t.Fatalf("downgrade inviter failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "token", Value: strconv.FormatUint(invitation.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/invitations/"+strconv.FormatUint(invitation.ID, 10)+"/accept", nil)
	c.Set("user_id", invitee.ID)

	h.AcceptInvitation(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertInvitationStillPendingWithoutMembership(t, db, invitation.ID, workspace.ID, invitee.ID)
}

func TestAcceptInvitationRejectsDisabledInviter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "disabled-inviter-owner", Role: "user", Status: "active", Email: "disabled-inviter-owner@example.com"}
	invitee := models.User{Username: "disabled-invitee-user", Role: "user", Status: "active", Email: "disabled-invitee-user@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-disabled-space", Slug: "invite-disabled-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	ownerMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&ownerMember).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}
	_, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleDeveloper, TokenHash: tokenHash, Status: models.WorkspaceInvitationStatusPending, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}
	if err := db.Model(&owner).Update("status", "disabled").Error; err != nil {
		t.Fatalf("disable inviter failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "token", Value: strconv.FormatUint(invitation.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/invitations/"+strconv.FormatUint(invitation.ID, 10)+"/accept", nil)
	c.Set("user_id", invitee.ID)

	h.AcceptInvitation(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	assertInvitationStillPendingWithoutMembership(t, db, invitation.ID, workspace.ID, invitee.ID)
}

func assertInvitationStillPendingWithoutMembership(t *testing.T, db *gorm.DB, invitationID, workspaceID, inviteeID uint64) {
	t.Helper()

	var reloadedInvitation models.WorkspaceInvitation
	if err := db.First(&reloadedInvitation, invitationID).Error; err != nil {
		t.Fatalf("reload invitation failed: %v", err)
	}
	if reloadedInvitation.Status != models.WorkspaceInvitationStatusPending {
		t.Fatalf("invitation status=%s, want=%s", reloadedInvitation.Status, models.WorkspaceInvitationStatusPending)
	}
	var memberCount int64
	if err := db.Model(&models.WorkspaceMember{}).Where("workspace_id = ? AND user_id = ?", workspaceID, inviteeID).Count(&memberCount).Error; err != nil {
		t.Fatalf("count invitee membership failed: %v", err)
	}
	if memberCount != 0 {
		t.Fatalf("invitee membership count=%d, want=0", memberCount)
	}
}
