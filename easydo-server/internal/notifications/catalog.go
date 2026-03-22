package notifications

import "strings"

type EventDefinition struct {
	Family      string `json:"family"`
	EventType   string `json:"event_type"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

var eventDefinitions = []EventDefinition{
	{Family: FamilyWorkspaceInvitation, EventType: EventTypeWorkspaceInvitationCreated, Label: "收到工作空间邀请", Description: "创建工作空间邀请时通知被邀请用户"},
	{Family: FamilyWorkspaceInvitation, EventType: EventTypeWorkspaceInvitationAccepted, Label: "工作空间邀请被接受", Description: "被邀请人接受工作空间邀请时通知邀请发起人"},
	{Family: FamilyWorkspaceMember, EventType: EventTypeWorkspaceMemberRoleUpdated, Label: "工作空间角色已更新", Description: "成员角色调整时通知该成员"},
	{Family: FamilyWorkspaceMember, EventType: EventTypeWorkspaceMemberRemoved, Label: "已从工作空间移除", Description: "成员被移出工作空间时通知该成员"},
	{Family: FamilyAgentLifecycle, EventType: EventTypeAgentApproved, Label: "执行器已批准", Description: "执行器审批通过时通知"},
	{Family: FamilyAgentLifecycle, EventType: EventTypeAgentRejected, Label: "执行器已拒绝", Description: "执行器审批拒绝时通知"},
	{Family: FamilyAgentLifecycle, EventType: EventTypeAgentRemoved, Label: "执行器已移除", Description: "执行器被移除时通知"},
	{Family: FamilyAgentLifecycle, EventType: EventTypeAgentOffline, Label: "执行器离线", Description: "执行器离线时通知"},
	{Family: FamilyPipelineRun, EventType: EventTypePipelineRunSucceeded, Label: "流水线运行成功", Description: "流水线运行成功时通知"},
	{Family: FamilyPipelineRun, EventType: EventTypePipelineRunFailed, Label: "流水线运行失败", Description: "流水线运行失败时通知"},
	{Family: FamilyPipelineRun, EventType: EventTypePipelineRunCancelled, Label: "流水线运行取消", Description: "流水线运行取消时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestCreated, Label: "发布申请已创建", Description: "发布申请创建时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestQueued, Label: "发布申请排队中", Description: "发布申请进入排队时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestRunning, Label: "发布申请执行中", Description: "发布申请开始执行时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestSucceeded, Label: "发布申请成功", Description: "发布申请成功时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestFailed, Label: "发布申请失败", Description: "发布申请失败时通知"},
	{Family: FamilyDeploymentRequest, EventType: EventTypeDeploymentRequestCancelled, Label: "发布申请取消", Description: "发布申请取消时通知"},
	{Family: "system.message", EventType: "system.message.created", Label: "系统消息", Description: "系统直接投递的站内通知"},
}

var familyByEventType = func() map[string]string {
	lookup := make(map[string]string, len(eventDefinitions))
	for _, item := range eventDefinitions {
		lookup[item.EventType] = item.Family
	}
	return lookup
}()

func EventDefinitions() []EventDefinition {
	result := make([]EventDefinition, len(eventDefinitions))
	copy(result, eventDefinitions)
	return result
}

func FamilyForEventType(eventType string) string {
	return familyByEventType[strings.TrimSpace(eventType)]
}

func IsKnownEventType(eventType string) bool {
	_, ok := familyByEventType[strings.TrimSpace(eventType)]
	return ok
}
