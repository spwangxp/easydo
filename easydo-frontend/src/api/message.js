import request from './request'

// Message APIs
export function getMessageList(params) {
  return request({
    url: '/messages',
    method: 'get',
    params
  })
}

export function getUnreadCount() {
  return request({
    url: '/messages/unread-count',
    method: 'get'
  })
}

export function markAsRead(id) {
  return request({
    url: `/messages/${id}/read`,
    method: 'post'
  })
}

export function markAllAsRead() {
  return request({
    url: '/messages/read-all',
    method: 'post'
  })
}
