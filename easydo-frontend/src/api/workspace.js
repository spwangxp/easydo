import request from './request'

export function getWorkspaceList() {
  return request({
    url: '/workspaces',
    method: 'get'
  })
}

export function getWorkspaceDetail(id) {
  return request({
    url: `/workspaces/${id}`,
    method: 'get'
  })
}

export function createWorkspace(data) {
  return request({
    url: '/workspaces',
    method: 'post',
    data
  })
}

export function updateWorkspace(id, data) {
  return request({
    url: `/workspaces/${id}`,
    method: 'patch',
    data
  })
}

export function getWorkspaceMembers(id) {
  return request({
    url: `/workspaces/${id}/members`,
    method: 'get'
  })
}

export function updateWorkspaceMember(id, memberId, data) {
  return request({
    url: `/workspaces/${id}/members/${memberId}`,
    method: 'patch',
    data
  })
}

export function removeWorkspaceMember(id, memberId) {
  return request({
    url: `/workspaces/${id}/members/${memberId}`,
    method: 'delete'
  })
}

export function getWorkspaceInvitations(id) {
  return request({
    url: `/workspaces/${id}/invitations`,
    method: 'get'
  })
}

export function createWorkspaceInvitation(id, data) {
  return request({
    url: `/workspaces/${id}/invitations`,
    method: 'post',
    data
  })
}

export function revokeWorkspaceInvitation(id, inviteId) {
  return request({
    url: `/workspaces/${id}/invitations/${inviteId}`,
    method: 'delete'
  })
}

export function acceptWorkspaceInvitation(token) {
  return request({
    url: `/workspaces/invitations/${token}/accept`,
    method: 'post'
  })
}
