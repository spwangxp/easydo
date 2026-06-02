package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

func seedWorkspaceInvitationOwner(t *testing.T) (*WorkspaceHandler, models.User, models.Workspace) {
	t.Helper()
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "invite-batch-owner", Role: "user", Status: "active", Email: "invite-batch-owner@example.com"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set owner password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "invite-batch-ws", Slug: "invite-batch-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create owner member failed: %v", err)
	}
	return h, owner, workspace
}

func performWorkspaceInvitationRequest(t *testing.T, h *WorkspaceHandler, owner models.User, workspace models.Workspace, method string, path string, body any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request failed: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Set("user_id", owner.ID)
	c.Set("username", owner.Username)
	c.Set("role", owner.Role)
	c.Set("workspace_id", workspace.ID)
	handler(c)
	return w
}

func TestCreateInvitationAcceptsBatchEmails(t *testing.T) {
	h, owner, workspace := seedWorkspaceInvitationOwner(t)

	w := performWorkspaceInvitationRequest(t, h, owner, workspace, http.MethodPost, "/api/workspaces/1/invitations", map[string]any{
		"emails": []string{"BatchA@example.com", "batch-b@example.com"},
		"role":   models.WorkspaceRoleDeveloper,
	}, h.CreateInvitation)
	if w.Code != http.StatusOK {
		t.Fatalf("create batch invitations status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID        uint64 `json:"id"`
				Email     string `json:"email"`
				Token     string `json:"token"`
				ExpiresAt int64  `json:"expires_at"`
			} `json:"list"`
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Data.Total != 2 || len(resp.Data.List) != 2 {
		t.Fatalf("batch response total=%d list=%d", resp.Data.Total, len(resp.Data.List))
	}
	if resp.Data.List[0].Email != "batcha@example.com" || resp.Data.List[1].Email != "batch-b@example.com" {
		t.Fatalf("emails not normalized in response: %+v", resp.Data.List)
	}
	if resp.Data.List[0].Token == "" || resp.Data.List[1].Token == "" || resp.Data.List[0].Token == resp.Data.List[1].Token {
		t.Fatalf("expected unique invite tokens: %+v", resp.Data.List)
	}

	var count int64
	if err := h.DB.Model(&models.WorkspaceInvitation{}).Where("workspace_id = ?", workspace.ID).Count(&count).Error; err != nil {
		t.Fatalf("count invitations failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("invitation count=%d, want 2", count)
	}
}

func TestRegenerateInvitationRefreshesPendingToken(t *testing.T) {
	h, owner, workspace := seedWorkspaceInvitationOwner(t)
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{
		WorkspaceID: workspace.ID,
		Email:       "regenerate@example.com",
		Role:        models.WorkspaceRoleViewer,
		TokenHash:   tokenHash,
		Status:      models.WorkspaceInvitationStatusPending,
		InvitedBy:   owner.ID,
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}
	if err := h.DB.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/1/invitations/1/regenerate", nil)
	c.Params = gin.Params{
		{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)},
		{Key: "invite_id", Value: strconv.FormatUint(invitation.ID, 10)},
	}
	c.Set("user_id", owner.ID)
	c.Set("username", owner.Username)
	c.Set("role", owner.Role)
	c.Set("workspace_id", workspace.ID)
	h.RegenerateInvitation(c)
	if w.Code != http.StatusOK {
		t.Fatalf("regenerate invitation status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			ID        uint64 `json:"id"`
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Data.ID != invitation.ID || resp.Data.Token == "" || resp.Data.Token == token {
		t.Fatalf("unexpected regenerate payload: %+v", resp.Data)
	}

	var reloaded models.WorkspaceInvitation
	if err := h.DB.First(&reloaded, invitation.ID).Error; err != nil {
		t.Fatalf("reload invitation failed: %v", err)
	}
	nextHash := sha256.Sum256([]byte(resp.Data.Token))
	if reloaded.TokenHash != hex.EncodeToString(nextHash[:]) {
		t.Fatalf("token hash was not refreshed")
	}
	if reloaded.ExpiresAt <= invitation.ExpiresAt {
		t.Fatalf("expires_at=%d, want greater than %d", reloaded.ExpiresAt, invitation.ExpiresAt)
	}
}
