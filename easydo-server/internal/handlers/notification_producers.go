package handlers

import (
	"fmt"
	"strings"

	"easydo-server/internal/models"
	"gorm.io/gorm"
)

const notificationFamilySystemMessage = "system.message"

func emitWorkspaceInvitationCreatedNotification(db *gorm.DB, workspace *models.Workspace, invitation *models.WorkspaceInvitation, actorUserID uint64) {
	if db == nil || workspace == nil || invitation == nil || invitation.ID == 0 {
		return
	}
	input := NotificationEventInput{
		WorkspaceID:      workspace.ID,
		Family:           NotificationFamilyWorkspaceInvitation,
		EventType:        NotificationEventTypeWorkspaceInvitationCreated,
		ResourceType:     models.NotificationResourceTypeWorkspaceInvite,
		ResourceID:       invitation.ID,
		ActorUserID:      actorUserID,
		ActorType:        "user",
		Title:            "工作空间邀请",
		Content:          fmt.Sprintf("你收到工作空间 %q 的加入邀请", workspace.Name),
		IdempotencyKey:   fmt.Sprintf("workspace-invitation-created:%d", invitation.ID),
		PermissionPolicy: "workspace_invitation",
		Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
		Metadata: map[string]interface{}{
			"workspace_id":    workspace.ID,
			"invitation_id":   invitation.ID,
			"workspace_name":  workspace.Name,
			"invitation_role": invitation.Role,
		},
	}
	if invitation.InvitedUserID != nil {
		input.UserRecipients = []uint64{*invitation.InvitedUserID}
	} else {
		input.EmailRecipients = []string{invitation.Email}
	}
	_, _ = EmitNotificationEvent(db, input)
}

func emitWorkspaceInvitationAcceptedNotification(db *gorm.DB, workspace *models.Workspace, invitation *models.WorkspaceInvitation, acceptedUser *models.User) {
	if db == nil || workspace == nil || invitation == nil || acceptedUser == nil {
		return
	}
	_, _ = EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      workspace.ID,
		Family:           NotificationFamilyWorkspaceInvitation,
		EventType:        NotificationEventTypeWorkspaceInvitationAccepted,
		ResourceType:     models.NotificationResourceTypeWorkspaceInvite,
		ResourceID:       invitation.ID,
		ActorUserID:      acceptedUser.ID,
		ActorType:        "user",
		Title:            "工作空间邀请已接受",
		Content:          fmt.Sprintf("%s 已加入工作空间 %q", acceptedUser.Username, workspace.Name),
		IdempotencyKey:   fmt.Sprintf("workspace-invitation-accepted:%d", invitation.ID),
		PermissionPolicy: "workspace_member",
		Channels:         []string{models.NotificationChannelInApp},
		UserRecipients:   []uint64{invitation.InvitedBy},
		Metadata: map[string]interface{}{
			"workspace_id":   workspace.ID,
			"invitation_id":  invitation.ID,
			"accepted_user":  acceptedUser.ID,
			"accepted_email": acceptedUser.Email,
		},
	})
}

func emitWorkspaceMemberRoleUpdatedNotification(db *gorm.DB, workspaceID uint64, member *models.WorkspaceMember, actorUserID uint64) {
	if db == nil || member == nil || member.UserID == 0 {
		return
	}
	_, _ = EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      workspaceID,
		Family:           NotificationFamilyWorkspaceMember,
		EventType:        NotificationEventTypeWorkspaceMemberRoleUpdated,
		ResourceType:     models.NotificationResourceTypeWorkspaceMember,
		ResourceID:       member.ID,
		ActorUserID:      actorUserID,
		ActorType:        "user",
		Title:            "工作空间角色已更新",
		Content:          fmt.Sprintf("你在工作空间中的角色已更新为 %s", member.Role),
		IdempotencyKey:   fmt.Sprintf("workspace-member-role-updated:%d:%s", member.ID, member.Role),
		PermissionPolicy: "workspace_member",
		Channels:         []string{models.NotificationChannelInApp},
		UserRecipients:   []uint64{member.UserID},
	})
}

func emitWorkspaceMemberRemovedNotification(db *gorm.DB, workspaceID uint64, member *models.WorkspaceMember, actorUserID uint64) {
	if db == nil || member == nil || member.UserID == 0 {
		return
	}
	_, _ = EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      workspaceID,
		Family:           NotificationFamilyWorkspaceMember,
		EventType:        NotificationEventTypeWorkspaceMemberRemoved,
		ResourceType:     models.NotificationResourceTypeWorkspaceMember,
		ResourceID:       member.ID,
		ActorUserID:      actorUserID,
		ActorType:        "user",
		Title:            "已从工作空间移除",
		Content:          "你已被移出当前工作空间",
		IdempotencyKey:   fmt.Sprintf("workspace-member-removed:%d", member.ID),
		PermissionPolicy: "workspace_invitation",
		Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
		UserRecipients:   []uint64{member.UserID},
	})
}

func emitAgentLifecycleNotification(db *gorm.DB, agent *models.Agent, eventType string, actorUserID uint64, title string, content string) {
	if db == nil || agent == nil || agent.ID == 0 {
		return
	}
	input := NotificationEventInput{
		WorkspaceID:    agent.WorkspaceID,
		Family:         NotificationFamilyAgentLifecycle,
		EventType:      eventType,
		ResourceType:   models.NotificationResourceTypeAgent,
		ResourceID:     agent.ID,
		Title:          title,
		Content:        content,
		IdempotencyKey: fmt.Sprintf("agent-lifecycle:%s:%d", eventType, agent.ID),
		Channels:       []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
		Metadata: map[string]interface{}{
			"agent_id":   agent.ID,
			"agent_name": agent.Name,
		},
	}
	if actorUserID > 0 {
		input.ActorUserID = actorUserID
		input.ActorType = "user"
	}
	if agent.WorkspaceID > 0 {
		input.PermissionPolicy = "workspace_member"
		input.UserRecipients = activeWorkspaceMemberUserIDs(db, agent.WorkspaceID)
	} else {
		input.PermissionPolicy = "platform_admin"
		input.UserRecipients = platformAdminUserIDs(db)
	}
	_, _ = EmitNotificationEvent(db, input)
}

func emitPipelineRunTerminalNotification(db *gorm.DB, run *models.PipelineRun, eventType string) {
	if db == nil || run == nil || run.ID == 0 || run.TriggerUserID == 0 {
		return
	}
	title := "流水线运行状态更新"
	content := fmt.Sprintf("流水线运行 #%d 状态变更为 %s", run.BuildNumber, run.Status)
	metadata := map[string]interface{}{
		"pipeline_run_id": run.ID,
		"pipeline_id":     run.PipelineID,
		"build_number":    run.BuildNumber,
		"trigger_type":    run.TriggerType,
		"status":          run.Status,
		"error_msg":       run.ErrorMsg,
	}
	_, _ = EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      run.WorkspaceID,
		Family:           NotificationFamilyPipelineRun,
		EventType:        eventType,
		ResourceType:     models.NotificationResourceTypePipelineRun,
		ResourceID:       run.ID,
		ActorUserID:      run.TriggerUserID,
		ActorType:        "user",
		Title:            title,
		Content:          content,
		IdempotencyKey:   fmt.Sprintf("pipeline-run-terminal:%d:%s", run.ID, run.Status),
		PermissionPolicy: "workspace_member",
		Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
		UserRecipients:   []uint64{run.TriggerUserID},
		Metadata:         metadata,
	})
}

func emitDeploymentRequestNotification(db *gorm.DB, request *models.DeploymentRequest, eventType string, title string, content string) {
	if db == nil || request == nil || request.ID == 0 || request.RequestedBy == 0 {
		return
	}
	metadata := map[string]interface{}{
		"deployment_request_id": request.ID,
		"pipeline_run_id":       request.PipelineRunID,
		"pipeline_id":           request.PipelineID,
		"status":                request.Status,
	}
	if request.PipelineRunID > 0 {
		var run models.PipelineRun
		if err := db.Select("id", "pipeline_id", "build_number", "status", "trigger_type").First(&run, request.PipelineRunID).Error; err == nil {
			metadata["pipeline_id"] = run.PipelineID
			metadata["build_number"] = run.BuildNumber
			metadata["run_status"] = run.Status
			metadata["trigger_type"] = run.TriggerType
		}
	}
	_, _ = EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      request.WorkspaceID,
		Family:           NotificationFamilyDeploymentRequest,
		EventType:        eventType,
		ResourceType:     models.NotificationResourceTypeDeploymentRequest,
		ResourceID:       request.ID,
		ActorUserID:      request.RequestedBy,
		ActorType:        "user",
		Title:            title,
		Content:          content,
		IdempotencyKey:   fmt.Sprintf("deployment-request:%d:%s", request.ID, eventType),
		PermissionPolicy: "workspace_member",
		Channels:         []string{models.NotificationChannelInApp, models.NotificationChannelEmail},
		UserRecipients:   []uint64{request.RequestedBy},
		Metadata:         metadata,
	})
}

func emitSystemInboxNotification(db *gorm.DB, workspaceID uint64, recipientUserID uint64, title string, content string, metadata map[string]interface{}, idempotencyKey string) error {
	_, err := EmitNotificationEvent(db, NotificationEventInput{
		WorkspaceID:      workspaceID,
		Family:           notificationFamilySystemMessage,
		EventType:        notificationFamilySystemMessage + ".created",
		ActorType:        "system",
		Title:            title,
		Content:          content,
		Metadata:         metadata,
		IdempotencyKey:   idempotencyKey,
		PermissionPolicy: "workspace_member",
		Channels:         []string{models.NotificationChannelInApp},
		UserRecipients:   []uint64{recipientUserID},
	})
	return err
}

func platformAdminUserIDs(db *gorm.DB) []uint64 {
	var users []models.User
	if db == nil {
		return nil
	}
	db.Where("role = ?", "admin").Find(&users)
	ids := make([]uint64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	return ids
}

func activeWorkspaceMemberUserIDs(db *gorm.DB, workspaceID uint64) []uint64 {
	if db == nil || workspaceID == 0 {
		return nil
	}
	var members []models.WorkspaceMember
	db.Where("workspace_id = ? AND status = ?", workspaceID, models.WorkspaceMemberStatusActive).Find(&members)
	ids := make([]uint64, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	return ids
}

func deploymentRequestEventType(status models.DeploymentRequestStatus) string {
	switch status {
	case models.DeploymentRequestStatusQueued:
		return NotificationEventTypeDeploymentRequestQueued
	case models.DeploymentRequestStatusRunning:
		return NotificationEventTypeDeploymentRequestRunning
	case models.DeploymentRequestStatusSuccess:
		return NotificationEventTypeDeploymentRequestSucceeded
	case models.DeploymentRequestStatusFailed:
		return NotificationEventTypeDeploymentRequestFailed
	case models.DeploymentRequestStatusCancelled:
		return NotificationEventTypeDeploymentRequestCancelled
	default:
		return ""
	}
}

func deploymentRequestEventTitle(status models.DeploymentRequestStatus) string {
	return "部署请求状态更新"
}

func deploymentRequestEventContent(status models.DeploymentRequestStatus) string {
	trimmed := strings.TrimSpace(string(status))
	if trimmed == "" {
		trimmed = "unknown"
	}
	return fmt.Sprintf("部署请求状态变更为 %s", trimmed)
}
