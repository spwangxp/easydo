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
