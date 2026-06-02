import request from './request'

export function login(data) {
  return request({
    url: '/auth/login',
    method: 'post',
    data
  })
}

export function logout() {
  return request({
    url: '/auth/logout',
    method: 'post'
  })
}

export function refreshAuthToken() {
  return request({
    url: '/auth/refresh',
    method: 'post'
  })
}

export function getUserInfo() {
  return request({
    url: '/auth/userinfo',
    method: 'get'
  })
}

export function updatePassword(data) {
  return request({
    url: '/auth/password',
    method: 'put',
    data
  })
}

export function getUserList(params) {
  return request({
    url: '/users',
    method: 'get',
    params
  })
}

export function createUser(data) {
  return request({
    url: '/users',
    method: 'post',
    data
  })
}

export function getUserDetail(id) {
  return request({
    url: `/users/${id}`,
    method: 'get'
  })
}

export function getUserWorkspaces(id) {
  return request({
    url: `/users/${id}/workspaces`,
    method: 'get'
  })
}

export function addUserWorkspace(id, data) {
  return request({
    url: `/users/${id}/workspaces`,
    method: 'post',
    data
  })
}

export function removeUserWorkspace(id, workspaceId) {
  return request({
    url: `/users/${id}/workspaces/${workspaceId}`,
    method: 'delete'
  })
}

export function updateUser(id, data) {
  return request({
    url: `/users/${id}`,
    method: 'patch',
    data
  })
}

export function disableUser(id, data = {}) {
  return request({
    url: `/users/${id}/disable`,
    method: 'post',
    data
  })
}

export function enableUser(id) {
  return request({
    url: `/users/${id}/enable`,
    method: 'post'
  })
}

export function resetUserPassword(id, data) {
  return request({
    url: `/users/${id}/reset-password`,
    method: 'post',
    data
  })
}

export function updateUserSystemRole(id, data) {
  return request({
    url: `/users/${id}/system-role`,
    method: 'patch',
    data
  })
}
