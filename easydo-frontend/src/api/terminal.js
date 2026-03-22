import request from './request'

export function createResourceTerminalSession(resourceId, data) {
  return request({
    url: `/resources/${resourceId}/terminal-sessions`,
    method: 'post',
    data
  })
}

export function listResourceTerminalSessions(resourceId) {
  return request({
    url: `/resources/${resourceId}/terminal-sessions`,
    method: 'get'
  })
}

export function getResourceTerminalSession(resourceId, sessionId) {
  return request({
    url: `/resources/${resourceId}/terminal-sessions/${sessionId}`,
    method: 'get'
  })
}

export function closeResourceTerminalSession(resourceId, sessionId, data) {
  return request({
    url: `/resources/${resourceId}/terminal-sessions/${sessionId}/close`,
    method: 'post',
    data
  })
}
