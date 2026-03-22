package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
)

func TestNotificationHandlerInboxReadFlow(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}

	owner := models.User{Username: "notify-owner", Role: "user", Status: "active", Email: "owner@example.com"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set owner password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	workspace := models.Workspace{Name: "notify-ws", Slug: "notify-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	result, err := EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID: workspace.ID,
		Family:      NotificationFamilyPipelineRun,
		EventType:   NotificationEventTypePipelineRunSucceeded,
		Title:       "流水线运行成功",
		Content:     "run finished successfully",
		ActorUserID: owner.ID,
		UserRecipients: []uint64{
			owner.ID,
		},
		IdempotencyKey: "notification-test-inbox-read-flow",
		Channels:       []string{models.NotificationChannelInApp},
	})
	if err != nil {
		t.Fatalf("EmitNotificationEvent failed: %v", err)
	}
	if len(result.InboxMessages) != 1 {
		t.Fatalf("inbox message count=%d, want 1", len(result.InboxMessages))
	}

	listW := httptest.NewRecorder()
	listC, _ := gin.CreateTestContext(listW)
	listC.Request = httptest.NewRequest(http.MethodGet, "/api/notifications/inbox?page=1&page_size=20", nil)
	listC.Set("user_id", owner.ID)
	listC.Set("workspace_id", workspace.ID)
	h.GetInbox(listC)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Code int `json:"code"`
		Data struct {
			List  []models.InboxMessage `json:"list"`
			Total int64                 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response failed: %v", err)
	}
	if listResp.Data.Total != 1 || len(listResp.Data.List) != 1 {
		t.Fatalf("unexpected list payload: %+v", listResp.Data)
	}

	countW := httptest.NewRecorder()
	countC, _ := gin.CreateTestContext(countW)
	countC.Request = httptest.NewRequest(http.MethodGet, "/api/notifications/inbox/unread-count", nil)
	countC.Set("user_id", owner.ID)
	countC.Set("workspace_id", workspace.ID)
	h.GetUnreadInboxCount(countC)
	if countW.Code != http.StatusOK {
		t.Fatalf("unread count status=%d body=%s", countW.Code, countW.Body.String())
	}
	var countResp struct {
		Data struct {
			UnreadCount int64 `json:"unread_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(countW.Body.Bytes(), &countResp); err != nil {
		t.Fatalf("unmarshal unread count response failed: %v", err)
	}
	if countResp.Data.UnreadCount != 1 {
		t.Fatalf("unread_count=%d, want 1", countResp.Data.UnreadCount)
	}

	messageID := strconv.FormatUint(result.InboxMessages[0].ID, 10)
	markW := httptest.NewRecorder()
	markC, _ := gin.CreateTestContext(markW)
	markC.Params = gin.Params{{Key: "id", Value: messageID}}
	markC.Request = httptest.NewRequest(http.MethodPost, "/api/notifications/inbox/"+messageID+"/read", nil)
	markC.Set("user_id", owner.ID)
	markC.Set("workspace_id", workspace.ID)
	h.MarkInboxMessageRead(markC)
	if markW.Code != http.StatusOK {
		t.Fatalf("mark read status=%d body=%s", markW.Code, markW.Body.String())
	}

	var inbox models.InboxMessage
	if err := db.First(&inbox, result.InboxMessages[0].ID).Error; err != nil {
		t.Fatalf("reload inbox message failed: %v", err)
	}
	if !inbox.IsRead {
		t.Fatal("expected inbox message to be marked read")
	}

	markAllW := httptest.NewRecorder()
	markAllC, _ := gin.CreateTestContext(markAllW)
	markAllC.Request = httptest.NewRequest(http.MethodPost, "/api/notifications/inbox/read-all", nil)
	markAllC.Set("user_id", owner.ID)
	markAllC.Set("workspace_id", workspace.ID)
	h.MarkAllInboxMessagesRead(markAllC)
	if markAllW.Code != http.StatusOK {
		t.Fatalf("mark all read status=%d body=%s", markAllW.Code, markAllW.Body.String())
	}
}

func TestNotificationHandlerInboxReturnsRecipientMessagesAcrossWorkspacesByDefault(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}

	owner := models.User{Username: "notify-owner-cross-ws", Role: "user", Status: "active", Email: "owner-cross-ws@example.com"}
	recipient := models.User{Username: "notify-recipient-cross-ws", Role: "user", Status: "active", Email: "recipient-cross-ws@example.com"}
	for _, user := range []*models.User{&owner, &recipient} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}

	personalWorkspace := models.Workspace{Name: "recipient-home", Slug: "recipient-home", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: recipient.ID}
	targetWorkspace := models.Workspace{Name: "target-workspace", Slug: "target-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	for _, workspace := range []*models.Workspace{&personalWorkspace, &targetWorkspace} {
		if err := db.Create(workspace).Error; err != nil {
			t.Fatalf("create workspace failed: %v", err)
		}
	}
	for _, member := range []models.WorkspaceMember{
		{WorkspaceID: personalWorkspace.ID, UserID: recipient.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: recipient.ID},
		{WorkspaceID: targetWorkspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
	} {
		member := member
		if err := db.Create(&member).Error; err != nil {
			t.Fatalf("create workspace member failed: %v", err)
		}
	}

	result, err := EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      targetWorkspace.ID,
		Family:           NotificationFamilyWorkspaceInvitation,
		EventType:        NotificationEventTypeWorkspaceInvitationCreated,
		ResourceType:     models.NotificationResourceTypeWorkspaceInvite,
		ResourceID:       9001,
		Title:            "工作空间邀请",
		Content:          "你收到新的工作空间邀请",
		UserRecipients:   []uint64{recipient.ID},
		PermissionPolicy: "workspace_invitation",
		IdempotencyKey:   "notification-test-cross-workspace-inbox",
		Channels:         []string{models.NotificationChannelInApp},
	})
	if err != nil {
		t.Fatalf("EmitNotificationEvent failed: %v", err)
	}
	if len(result.InboxMessages) != 1 {
		t.Fatalf("inbox message count=%d, want 1", len(result.InboxMessages))
	}

	listW := httptest.NewRecorder()
	listC, _ := gin.CreateTestContext(listW)
	listC.Request = httptest.NewRequest(http.MethodGet, "/api/notifications/inbox?page=1&page_size=20", nil)
	listC.Set("user_id", recipient.ID)
	listC.Set("workspace_id", personalWorkspace.ID)
	h.GetInbox(listC)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listW.Code, listW.Body.String())
	}
	var listResp struct {
		Code int `json:"code"`
		Data struct {
			List  []models.InboxMessage `json:"list"`
			Total int64                 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response failed: %v", err)
	}
	if listResp.Data.Total != 1 || len(listResp.Data.List) != 1 {
		t.Fatalf("unexpected list payload: %+v", listResp.Data)
	}
	if listResp.Data.List[0].WorkspaceID != targetWorkspace.ID {
		t.Fatalf("message workspace_id=%d, want %d", listResp.Data.List[0].WorkspaceID, targetWorkspace.ID)
	}

	countW := httptest.NewRecorder()
	countC, _ := gin.CreateTestContext(countW)
	countC.Request = httptest.NewRequest(http.MethodGet, "/api/notifications/inbox/unread-count", nil)
	countC.Set("user_id", recipient.ID)
	countC.Set("workspace_id", personalWorkspace.ID)
	h.GetUnreadInboxCount(countC)
	if countW.Code != http.StatusOK {
		t.Fatalf("unread count status=%d body=%s", countW.Code, countW.Body.String())
	}
	var countResp struct {
		Data struct {
			UnreadCount int64 `json:"unread_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(countW.Body.Bytes(), &countResp); err != nil {
		t.Fatalf("unmarshal unread count response failed: %v", err)
	}
	if countResp.Data.UnreadCount != 1 {
		t.Fatalf("unread_count=%d, want 1", countResp.Data.UnreadCount)
	}
}

func TestNotificationHandlerPreferenceResolutionSupportsEventTypeOverrides(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &NotificationHandler{DB: db}

	user := models.User{Username: "notify-pref-user", Role: "user", Status: "active", Email: "pref@example.com"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set user password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "pref-ws", Slug: "pref-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}

	for _, payload := range []map[string]interface{}{
		{
			"family":     NotificationFamilyPipelineRun,
			"event_type": NotificationEventTypePipelineRunSucceeded,
			"channel":    models.NotificationChannelEmail,
			"enabled":    false,
		},
		{
			"workspace_id": workspace.ID,
			"family":       NotificationFamilyPipelineRun,
			"event_type":   NotificationEventTypePipelineRunFailed,
			"channel":      models.NotificationChannelEmail,
			"enabled":      true,
		},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body, _ := json.Marshal(payload)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/notifications/preferences", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", user.ID)
		c.Set("workspace_id", workspace.ID)
		h.UpsertPreference(c)
		if w.Code != http.StatusOK {
			t.Fatalf("upsert preference status=%d body=%s", w.Code, w.Body.String())
		}
	}

	succeededResult, err := EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:    workspace.ID,
		Family:         NotificationFamilyPipelineRun,
		EventType:      NotificationEventTypePipelineRunSucceeded,
		ResourceType:   models.NotificationResourceTypePipelineRun,
		ResourceID:     42,
		Title:          "流水线成功",
		Content:        "pipeline succeeded",
		UserRecipients: []uint64{user.ID},
		IdempotencyKey: "notification-test-preference-resolution-succeeded",
		Channels:       []string{models.NotificationChannelEmail},
	})
	if err != nil {
		t.Fatalf("EmitNotificationEvent for succeeded failed: %v", err)
	}
	if len(succeededResult.Deliveries) != 1 {
		t.Fatalf("succeeded delivery count=%d, want 1", len(succeededResult.Deliveries))
	}
	if succeededResult.Deliveries[0].Status != models.NotificationDeliveryStatusSuppressed {
		t.Fatalf("succeeded delivery status=%s, want %s", succeededResult.Deliveries[0].Status, models.NotificationDeliveryStatusSuppressed)
	}

	failedResult, err := EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:    workspace.ID,
		Family:         NotificationFamilyPipelineRun,
		EventType:      NotificationEventTypePipelineRunFailed,
		ResourceType:   models.NotificationResourceTypePipelineRun,
		ResourceID:     43,
		Title:          "流水线失败",
		Content:        "pipeline failed",
		UserRecipients: []uint64{user.ID},
		IdempotencyKey: "notification-test-preference-resolution-failed",
		Channels:       []string{models.NotificationChannelEmail},
	})
	if err != nil {
		t.Fatalf("EmitNotificationEvent for failed failed: %v", err)
	}
	if len(failedResult.Deliveries) != 1 {
		t.Fatalf("failed delivery count=%d, want 1", len(failedResult.Deliveries))
	}
	if failedResult.Deliveries[0].Status == models.NotificationDeliveryStatusSuppressed {
		t.Fatalf("failed delivery status=%s, want enabled non-suppressed delivery", failedResult.Deliveries[0].Status)
	}

	listW := httptest.NewRecorder()
	listC, _ := gin.CreateTestContext(listW)
	listC.Request = httptest.NewRequest(http.MethodGet, "/api/notifications/preferences", nil)
	listC.Set("user_id", user.ID)
	listC.Set("workspace_id", workspace.ID)
	h.ListPreferences(listC)
	if listW.Code != http.StatusOK {
		t.Fatalf("list preferences status=%d body=%s", listW.Code, listW.Body.String())
	}
}

func TestEmitNotificationEventFanOutHonorsPermissionBoundsAndIdempotency(t *testing.T) {
	db := openHandlerTestDB(t)

	owner := models.User{Username: "notify-owner-fanout", Role: "user", Status: "active", Email: "owner-fanout@example.com"}
	other := models.User{Username: "notify-other-fanout", Role: "user", Status: "active", Email: "other-fanout@example.com"}
	outsider := models.User{Username: "notify-outsider-fanout", Role: "user", Status: "active", Email: "outsider-fanout@example.com"}
	for _, user := range []*models.User{&owner, &other, &outsider} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "fanout-ws", Slug: "fanout-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	for _, member := range []models.WorkspaceMember{
		{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
		{WorkspaceID: workspace.ID, UserID: other.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID},
	} {
		member := member
		if err := db.Create(&member).Error; err != nil {
			t.Fatalf("create workspace member failed: %v", err)
		}
	}

	input := NotificationEventInput{
		WorkspaceID: workspace.ID,
		Family:      NotificationFamilyAgentLifecycle,
		EventType:   NotificationEventTypeAgentOffline,
		Title:       "执行器离线",
		Content:     "agent became offline",
		UserRecipients: []uint64{
			owner.ID,
			other.ID,
			outsider.ID,
		},
		IdempotencyKey: "notification-test-fanout-idempotency",
		Channels:       []string{models.NotificationChannelInApp},
	}
	first, err := EmitNotificationEvent(db, input)
	if err != nil {
		t.Fatalf("first EmitNotificationEvent failed: %v", err)
	}
	second, err := EmitNotificationEvent(db, input)
	if err != nil {
		t.Fatalf("second EmitNotificationEvent failed: %v", err)
	}
	if first.Event.ID != second.Event.ID {
		t.Fatalf("idempotent event id mismatch: first=%d second=%d", first.Event.ID, second.Event.ID)
	}

	var eventCount int64
	if err := db.Model(&models.NotificationEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatalf("count notification events failed: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("notification event count=%d, want 1", eventCount)
	}

	var inboxCount int64
	if err := db.Model(&models.InboxMessage{}).Count(&inboxCount).Error; err != nil {
		t.Fatalf("count inbox messages failed: %v", err)
	}
	if inboxCount != 2 {
		t.Fatalf("inbox message count=%d, want 2 for active workspace members only", inboxCount)
	}
}

func TestWorkspaceCreateInvitationEmitsCanonicalNotification(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "workspace-invite-owner", Role: "user", Status: "active", Email: "owner-invite@example.com"}
	invitee := models.User{Username: "workspace-invite-target", Role: "user", Status: "active", Email: "target-invite@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-ws", Slug: "invite-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace owner membership failed: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"email": invitee.Email, "role": models.WorkspaceRoleDeveloper})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/invitations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", owner.ID)
	c.Set("role", "user")
	h.CreateInvitation(c)
	if w.Code != http.StatusOK {
		t.Fatalf("create invitation status=%d body=%s", w.Code, w.Body.String())
	}

	var eventCount int64
	if err := db.Model(&models.NotificationEvent{}).Where("family = ? AND event_type = ?", NotificationFamilyWorkspaceInvitation, NotificationEventTypeWorkspaceInvitationCreated).Count(&eventCount).Error; err != nil {
		t.Fatalf("count invitation notification events failed: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("workspace invitation notification event count=%d, want 1", eventCount)
	}
	var inviteeInboxCount int64
	if err := db.Model(&models.InboxMessage{}).Where("recipient_id = ? AND type = ?", invitee.ID, NotificationFamilyWorkspaceInvitation).Count(&inviteeInboxCount).Error; err != nil {
		t.Fatalf("count invitee inbox messages failed: %v", err)
	}
	if inviteeInboxCount != 1 {
		t.Fatalf("invitee inbox message count=%d, want 1", inviteeInboxCount)
	}
}

func TestWorkspaceUpdateMemberEmitsRoleUpdatedNotification(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "workspace-role-owner", Role: "user", Status: "active", Email: "workspace-role-owner@example.com"}
	memberUser := models.User{Username: "workspace-role-member", Role: "user", Status: "active", Email: "workspace-role-member@example.com"}
	for _, user := range []*models.User{&owner, &memberUser} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "workspace-role-ws", Slug: "workspace-role-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	ownerMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	targetMember := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: memberUser.ID, Role: models.WorkspaceRoleViewer, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	for _, member := range []*models.WorkspaceMember{&ownerMember, &targetMember} {
		if err := db.Create(member).Error; err != nil {
			t.Fatalf("create workspace member failed: %v", err)
		}
	}

	body, _ := json.Marshal(map[string]interface{}{"role": models.WorkspaceRoleDeveloper})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(workspace.ID, 10)}, {Key: "member_id", Value: strconv.FormatUint(targetMember.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+strconv.FormatUint(workspace.ID, 10)+"/members/"+strconv.FormatUint(targetMember.ID, 10), bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", owner.ID)
	h.UpdateMember(c)
	if w.Code != http.StatusOK {
		t.Fatalf("update member status=%d body=%s", w.Code, w.Body.String())
	}

	var inboxCount int64
	if err := db.Model(&models.InboxMessage{}).Where("recipient_id = ? AND type = ? AND event_type = ?", memberUser.ID, NotificationFamilyWorkspaceMember, NotificationEventTypeWorkspaceMemberRoleUpdated).Count(&inboxCount).Error; err != nil {
		t.Fatalf("count role updated inbox messages failed: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("role updated inbox count=%d, want 1", inboxCount)
	}
}

func TestWorkspaceNotificationProducersEmitAcceptedAndRemovedEvents(t *testing.T) {
	db := openHandlerTestDB(t)

	owner := models.User{Username: "workspace-producer-owner", Role: "user", Status: "active", Email: "workspace-producer-owner@example.com"}
	invitee := models.User{Username: "workspace-producer-invitee", Role: "user", Status: "active", Email: "workspace-producer-invitee@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "workspace-producer-ws", Slug: "workspace-producer-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleDeveloper, TokenHash: "workspace-producer-token", Status: models.WorkspaceInvitationStatusAccepted, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: invitee.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	emitWorkspaceInvitationAcceptedNotification(db, &workspace, &invitation, &invitee)
	emitWorkspaceMemberRemovedNotification(db, workspace.ID, &member, owner.ID)

	for _, check := range []struct {
		eventType string
		userID    uint64
	}{
		{NotificationEventTypeWorkspaceInvitationAccepted, owner.ID},
		{NotificationEventTypeWorkspaceMemberRemoved, invitee.ID},
	} {
		var eventCount int64
		if err := db.Model(&models.NotificationEvent{}).Where("event_type = ?", check.eventType).Count(&eventCount).Error; err != nil {
			t.Fatalf("count %s events failed: %v", check.eventType, err)
		}
		if eventCount != 1 {
			t.Fatalf("event count for %s = %d, want 1", check.eventType, eventCount)
		}
		var inboxCount int64
		if err := db.Model(&models.InboxMessage{}).Where("recipient_id = ? AND event_type = ?", check.userID, check.eventType).Count(&inboxCount).Error; err != nil {
			t.Fatalf("count inbox for %s failed: %v", check.eventType, err)
		}
		if inboxCount != 1 {
			t.Fatalf("inbox count for %s = %d, want 1", check.eventType, inboxCount)
		}
	}
}

func TestAgentAndTerminalNotificationProducersEmitExpectedEventTypes(t *testing.T) {
	db := openHandlerTestDB(t)

	owner := models.User{Username: "agent-producer-owner", Role: "user", Status: "active", Email: "agent-producer-owner@example.com"}
	if err := owner.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "agent-producer-ws", Slug: "agent-producer-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: owner.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	agent := models.Agent{Name: "agent-producer", WorkspaceID: workspace.ID, ScopeType: models.AgentScopeWorkspace, Status: models.AgentStatusOnline, RegistrationStatus: models.AgentRegistrationStatusApproved}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: 1, BuildNumber: 7, Status: models.PipelineRunStatusFailed, TriggerUserID: owner.ID, TriggerUser: owner.Username, ErrorMsg: "boom"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}

	emitAgentLifecycleNotification(db, &agent, NotificationEventTypeAgentApproved, owner.ID, "执行器已批准", "approved")
	emitAgentLifecycleNotification(db, &agent, NotificationEventTypeAgentRejected, owner.ID, "执行器已拒绝", "rejected")
	emitAgentLifecycleNotification(db, &agent, NotificationEventTypeAgentRemoved, owner.ID, "执行器已移除", "removed")
	emitPipelineRunTerminalNotification(db, &run, NotificationEventTypePipelineRunFailed)
	run.Status = models.PipelineRunStatusCancelled
	emitPipelineRunTerminalNotification(db, &run, NotificationEventTypePipelineRunCancelled)

	for _, eventType := range []string{
		NotificationEventTypeAgentApproved,
		NotificationEventTypeAgentRejected,
		NotificationEventTypeAgentRemoved,
		NotificationEventTypePipelineRunFailed,
		NotificationEventTypePipelineRunCancelled,
	} {
		var eventCount int64
		if err := db.Model(&models.NotificationEvent{}).Where("event_type = ?", eventType).Count(&eventCount).Error; err != nil {
			t.Fatalf("count %s events failed: %v", eventType, err)
		}
		if eventCount != 1 {
			t.Fatalf("event count for %s = %d, want 1", eventType, eventCount)
		}
	}
}

func TestDeploymentAndSystemNotificationProducersEmitExpectedEventTypes(t *testing.T) {
	db := openHandlerTestDB(t)

	user := models.User{Username: "deployment-producer-user", Role: "user", Status: "active", Email: "deployment-producer-user@example.com"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "deployment-producer-ws", Slug: "deployment-producer-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	request := models.DeploymentRequest{WorkspaceID: workspace.ID, RequestedBy: user.ID, Status: models.DeploymentRequestStatusQueued}
	if err := db.Create(&request).Error; err != nil {
		t.Fatalf("create deployment request failed: %v", err)
	}

	emitDeploymentRequestNotification(db, &request, NotificationEventTypeDeploymentRequestCreated, "created", "created")
	emitDeploymentRequestNotification(db, &request, NotificationEventTypeDeploymentRequestQueued, "queued", "queued")
	emitDeploymentRequestNotification(db, &request, NotificationEventTypeDeploymentRequestRunning, "running", "running")
	emitDeploymentRequestNotification(db, &request, NotificationEventTypeDeploymentRequestFailed, "failed", "failed")
	emitDeploymentRequestNotification(db, &request, NotificationEventTypeDeploymentRequestCancelled, "cancelled", "cancelled")
	if err := emitSystemInboxNotification(db, workspace.ID, user.ID, "系统消息", "system content", map[string]interface{}{"kind": "demo"}, "system-inbox-notification-test"); err != nil {
		t.Fatalf("emit system inbox notification failed: %v", err)
	}

	for _, eventType := range []string{
		NotificationEventTypeDeploymentRequestCreated,
		NotificationEventTypeDeploymentRequestQueued,
		NotificationEventTypeDeploymentRequestRunning,
		NotificationEventTypeDeploymentRequestFailed,
		NotificationEventTypeDeploymentRequestCancelled,
		notificationFamilySystemMessage + ".created",
	} {
		var eventCount int64
		if err := db.Model(&models.NotificationEvent{}).Where("event_type = ?", eventType).Count(&eventCount).Error; err != nil {
			t.Fatalf("count %s events failed: %v", eventType, err)
		}
		if eventCount != 1 {
			t.Fatalf("event count for %s = %d, want 1", eventType, eventCount)
		}
	}
}

func TestWorkspaceAcceptInvitationSupportsInvitationIDLocator(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "workspace-invite-owner-id", Role: "user", Status: "active", Email: "owner-id@example.com"}
	invitee := models.User{Username: "workspace-invite-target-id", Role: "user", Status: "active", Email: "target-id@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-id-ws", Slug: "invite-id-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	_ = token
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleViewer, TokenHash: tokenHash, Status: models.WorkspaceInvitationStatusPending, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "token", Value: strconv.FormatUint(invitation.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/invitations/"+strconv.FormatUint(invitation.ID, 10)+"/accept", nil)
	c.Set("user_id", invitee.ID)
	h.AcceptInvitation(c)
	if w.Code != http.StatusOK {
		t.Fatalf("accept invitation by id status=%d body=%s", w.Code, w.Body.String())
	}

	var reloaded models.WorkspaceInvitation
	if err := db.First(&reloaded, invitation.ID).Error; err != nil {
		t.Fatalf("reload invitation failed: %v", err)
	}
	if reloaded.Status != models.WorkspaceInvitationStatusAccepted {
		t.Fatalf("invitation status=%s, want %s", reloaded.Status, models.WorkspaceInvitationStatusAccepted)
	}
}

func TestWorkspaceAcceptInvitationSupportsLegacyTokenLocator(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "workspace-invite-owner-token", Role: "user", Status: "active", Email: "owner-token@example.com"}
	invitee := models.User{Username: "workspace-invite-target-token", Role: "user", Status: "active", Email: "target-token@example.com"}
	for _, user := range []*models.User{&owner, &invitee} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-token-ws", Slug: "invite-token-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleViewer, TokenHash: tokenHash, Status: models.WorkspaceInvitationStatusPending, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "token", Value: token}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/invitations/"+token+"/accept", nil)
	c.Set("user_id", invitee.ID)
	h.AcceptInvitation(c)
	if w.Code != http.StatusOK {
		t.Fatalf("accept invitation by legacy token status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWorkspaceAcceptInvitationRejectsMismatchedUserForIDLocator(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &WorkspaceHandler{DB: db}

	owner := models.User{Username: "workspace-invite-owner-mismatch", Role: "user", Status: "active", Email: "owner-mismatch@example.com"}
	invitee := models.User{Username: "workspace-invite-target-mismatch", Role: "user", Status: "active", Email: "target-mismatch@example.com"}
	outsider := models.User{Username: "workspace-invite-outsider-mismatch", Role: "user", Status: "active", Email: "outsider-mismatch@example.com"}
	for _, user := range []*models.User{&owner, &invitee, &outsider} {
		if err := user.SetPassword("1qaz2WSX"); err != nil {
			t.Fatalf("set password failed: %v", err)
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}
	workspace := models.Workspace{Name: "invite-mismatch-ws", Slug: "invite-mismatch-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: owner.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	token, tokenHash, err := generateInviteToken()
	if err != nil {
		t.Fatalf("generate invite token failed: %v", err)
	}
	_ = token
	invitation := models.WorkspaceInvitation{WorkspaceID: workspace.ID, Email: invitee.Email, InvitedUserID: &invitee.ID, Role: models.WorkspaceRoleViewer, TokenHash: tokenHash, Status: models.WorkspaceInvitationStatusPending, InvitedBy: owner.ID, ExpiresAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&invitation).Error; err != nil {
		t.Fatalf("create invitation failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "token", Value: strconv.FormatUint(invitation.ID, 10)}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspaces/invitations/"+strconv.FormatUint(invitation.ID, 10)+"/accept", nil)
	c.Set("user_id", outsider.ID)
	h.AcceptInvitation(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("mismatched user accept status=%d body=%s", w.Code, w.Body.String())
	}

	var reloaded models.WorkspaceInvitation
	if err := db.First(&reloaded, invitation.ID).Error; err != nil {
		t.Fatalf("reload invitation failed: %v", err)
	}
	if reloaded.Status != models.WorkspaceInvitationStatusPending {
		t.Fatalf("invitation status=%s, want %s", reloaded.Status, models.WorkspaceInvitationStatusPending)
	}
}

func TestUpdateRunStatusEmitsPipelineAndDeploymentNotifications(t *testing.T) {
	db := openHandlerTestDB(t)
	h := &PipelineHandler{DB: db}

	user := models.User{Username: "pipeline-notify-user", Role: "user", Status: "active", Email: "pipeline-notify@example.com"}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set user password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "pipeline-notify-ws", Slug: "pipeline-notify-ws", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	if err := db.Create(&models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleOwner, Status: models.WorkspaceMemberStatusActive, InvitedBy: user.ID}).Error; err != nil {
		t.Fatalf("create workspace member failed: %v", err)
	}
	pipeline := models.Pipeline{Name: "notify-pipeline", WorkspaceID: workspace.ID, OwnerID: user.ID, Environment: "development"}
	if err := db.Create(&pipeline).Error; err != nil {
		t.Fatalf("create pipeline failed: %v", err)
	}
	run := models.PipelineRun{WorkspaceID: workspace.ID, PipelineID: pipeline.ID, BuildNumber: 1, Status: models.PipelineRunStatusRunning, TriggerUser: user.Username, TriggerUserID: user.ID, StartTime: time.Now().Add(-time.Minute).Unix()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create pipeline run failed: %v", err)
	}
	request := models.DeploymentRequest{WorkspaceID: workspace.ID, TemplateID: 1, TemplateVersionID: 1, TemplateType: models.StoreTemplateTypeApp, TargetResourceID: 1, TargetResourceType: models.ResourceTypeVM, Status: models.DeploymentRequestStatusRunning, PipelineID: pipeline.ID, PipelineRunID: run.ID, RequestedBy: user.ID}
	if err := db.Create(&request).Error; err != nil {
		t.Fatalf("create deployment request failed: %v", err)
	}
	record := models.DeploymentRecord{WorkspaceID: workspace.ID, RequestID: request.ID, PipelineRunID: run.ID, Status: models.DeploymentRequestStatusRunning}
	if err := db.Create(&record).Error; err != nil {
		t.Fatalf("create deployment record failed: %v", err)
	}

	h.updateRunStatus(run.ID, models.PipelineRunStatusSuccess, "")

	var pipelineEventCount int64
	if err := db.Model(&models.NotificationEvent{}).Where("family = ? AND event_type = ?", NotificationFamilyPipelineRun, NotificationEventTypePipelineRunSucceeded).Count(&pipelineEventCount).Error; err != nil {
		t.Fatalf("count pipeline notification events failed: %v", err)
	}
	if pipelineEventCount != 1 {
		t.Fatalf("pipeline notification event count=%d, want 1", pipelineEventCount)
	}
	var deploymentEventCount int64
	if err := db.Model(&models.NotificationEvent{}).Where("family = ? AND event_type = ?", NotificationFamilyDeploymentRequest, NotificationEventTypeDeploymentRequestSucceeded).Count(&deploymentEventCount).Error; err != nil {
		t.Fatalf("count deployment notification events failed: %v", err)
	}
	if deploymentEventCount != 1 {
		t.Fatalf("deployment notification event count=%d, want 1", deploymentEventCount)
	}
	var reloadedRequest models.DeploymentRequest
	if err := db.First(&reloadedRequest, request.ID).Error; err != nil {
		t.Fatalf("reload deployment request failed: %v", err)
	}
	if reloadedRequest.Status != models.DeploymentRequestStatusSuccess {
		t.Fatalf("deployment request status=%s, want %s", reloadedRequest.Status, models.DeploymentRequestStatusSuccess)
	}
}
