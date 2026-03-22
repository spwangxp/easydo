import request from './request'

export const NOTIFICATION_CHANNELS = [
  { value: 'in_app', label: '站内通知' },
  { value: 'email', label: '邮件通知' }
]

export const NOTIFICATION_EVENT_GROUPS = [
  {
    value: 'pipeline.run',
    label: '流水线运行',
    description: '流水线不同结果分别控制通知方式',
    supports_resource_scope: true,
    resource_scope_label: '后续可细化到某条流水线',
    events: [
      { value: 'pipeline.run.succeeded', label: '流水线运行成功', description: '流水线执行成功时通知' },
      { value: 'pipeline.run.failed', label: '流水线运行失败', description: '流水线执行失败时通知' },
      { value: 'pipeline.run.cancelled', label: '流水线运行取消', description: '流水线执行取消时通知' }
    ]
  },
  {
    value: 'deployment.request',
    label: '发布申请',
    description: '发布申请各个执行阶段分别控制通知方式',
    supports_resource_scope: false,
    resource_scope_label: '',
    events: [
      { value: 'deployment.request.created', label: '发布申请已创建', description: '创建发布申请时通知' },
      { value: 'deployment.request.queued', label: '发布申请排队中', description: '发布申请进入排队时通知' },
      { value: 'deployment.request.running', label: '发布申请执行中', description: '发布申请开始执行时通知' },
      { value: 'deployment.request.succeeded', label: '发布申请成功', description: '发布申请成功时通知' },
      { value: 'deployment.request.failed', label: '发布申请失败', description: '发布申请失败时通知' },
      { value: 'deployment.request.cancelled', label: '发布申请取消', description: '发布申请取消时通知' }
    ]
  },
  {
    value: 'agent.lifecycle',
    label: '执行器状态',
    description: '执行器状态变化分别控制通知方式',
    supports_resource_scope: true,
    resource_scope_label: '后续可细化到某个执行器',
    events: [
      { value: 'agent.approved', label: '执行器已批准', description: '执行器审批通过时通知' },
      { value: 'agent.rejected', label: '执行器已拒绝', description: '执行器审批拒绝时通知' },
      { value: 'agent.removed', label: '执行器已移除', description: '执行器被移除时通知' },
      { value: 'agent.offline', label: '执行器离线', description: '执行器离线时通知' }
    ]
  },
  {
    value: 'workspace.member',
    label: '工作空间成员',
    description: '成员变更动作分别控制通知方式',
    supports_resource_scope: false,
    resource_scope_label: '',
    events: [
      { value: 'workspace.member.role_updated', label: '成员角色已更新', description: '成员角色调整时通知' },
      { value: 'workspace.member.removed', label: '成员已被移除', description: '成员被移出工作空间时通知' }
    ]
  },
  {
    value: 'workspace.invitation',
    label: '工作空间邀请',
    description: '邀请动作分别控制通知方式',
    supports_resource_scope: false,
    resource_scope_label: '',
    events: [
      { value: 'workspace.invitation.created', label: '收到工作空间邀请', description: '发起邀请时通知被邀请人' },
      { value: 'workspace.invitation.accepted', label: '工作空间邀请被接受', description: '被邀请人接受邀请时通知邀请发起人' }
    ]
  },
  {
    value: 'system.message',
    label: '系统消息',
    description: '系统直接投递的站内消息',
    supports_resource_scope: false,
    resource_scope_label: '',
    events: [
      { value: 'system.message.created', label: '系统消息', description: '系统主动创建站内消息时通知' }
    ]
  }
]

export const NOTIFICATION_EVENTS = NOTIFICATION_EVENT_GROUPS.flatMap(group => {
  return group.events.map(event => ({ ...event, family: group.value, family_label: group.label }))
})

export function getNotificationInbox(params) {
  return request({
    url: '/notifications/inbox',
    method: 'get',
    params
  })
}

export function getNotificationUnreadCount() {
  return request({
    url: '/notifications/inbox/unread-count',
    method: 'get'
  })
}

export function markNotificationRead(id) {
  return request({
    url: `/notifications/inbox/${id}/read`,
    method: 'post'
  })
}

export function markAllNotificationsRead() {
  return request({
    url: '/notifications/inbox/read-all',
    method: 'post'
  })
}

export function listNotificationPreferences(params) {
  return request({
    url: '/notifications/preferences',
    method: 'get',
    params
  })
}

export function upsertNotificationPreference(data) {
  return request({
    url: '/notifications/preferences',
    method: 'put',
    data
  })
}

export function normalizeNotificationWorkspaceId(workspaceId) {
  if (workspaceId === null || workspaceId === undefined || workspaceId === '') {
    return null
  }
  const numericValue = Number(workspaceId)
  return Number.isNaN(numericValue) ? workspaceId : numericValue
}

export function findNotificationPreference(preferences, { eventType, channel, workspaceId = null, resourceType = '', resourceId = null }) {
  const normalizedWorkspaceId = normalizeNotificationWorkspaceId(workspaceId)
  const normalizedResourceId = normalizeNotificationWorkspaceId(resourceId)

  return (preferences || []).find((item) => {
    return item.event_type === eventType
      && item.channel === channel
      && normalizeNotificationWorkspaceId(item.workspace_id) === normalizedWorkspaceId
      && (item.resource_type || '') === resourceType
      && normalizeNotificationWorkspaceId(item.resource_id) === normalizedResourceId
  })
}

export function resolveNotificationPreferenceEnabled(preferences, { eventType, channel, workspaceId = null, fallbackToGlobal = false, defaultEnabled = true, resourceType = '', resourceId = null }) {
  const exactMatch = findNotificationPreference(preferences, { eventType, channel, workspaceId, resourceType, resourceId })
  if (exactMatch) {
    return !!exactMatch.enabled
  }

  if (fallbackToGlobal && normalizeNotificationWorkspaceId(workspaceId) !== null) {
    const globalMatch = findNotificationPreference(preferences, { eventType, channel, workspaceId: null, resourceType, resourceId })
    if (globalMatch) {
      return !!globalMatch.enabled
    }
  }

  return defaultEnabled
}

export function upsertNotificationPreferenceInList(preferences, preference) {
  const currentList = Array.isArray(preferences) ? [...preferences] : []
  const index = currentList.findIndex((item) => {
    return item.event_type === preference.event_type
      && item.channel === preference.channel
      && normalizeNotificationWorkspaceId(item.workspace_id) === normalizeNotificationWorkspaceId(preference.workspace_id)
      && (item.resource_type || '') === (preference.resource_type || '')
      && normalizeNotificationWorkspaceId(item.resource_id) === normalizeNotificationWorkspaceId(preference.resource_id)
  })

  if (index >= 0) {
    currentList[index] = { ...currentList[index], ...preference }
    return currentList
  }

  currentList.push(preference)
  return currentList
}
