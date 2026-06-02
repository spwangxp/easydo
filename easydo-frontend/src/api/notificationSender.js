import request from './request'

export function getEffectiveNotificationSender() {
  return request({
    url: '/notification-senders/effective',
    method: 'get'
  })
}

export function savePlatformNotificationSender(data) {
  return request({
    url: '/notification-senders/platform',
    method: 'put',
    data
  })
}

export function saveWorkspaceNotificationSender(id, data) {
  return request({
    url: `/workspaces/${id}/notification-sender`,
    method: 'put',
    data
  })
}

export function testNotificationSender(data) {
  return request({
    url: '/notification-senders/test',
    method: 'post',
    data
  })
}
