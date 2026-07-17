import request from './request.js'

export function enqueueRuntimeSessionQueueItem(sessionId, data = {}) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/queue-items`,
    method: 'post',
    data
  })
}

export function listRuntimeSessionQueueItems(sessionId) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/queue-items`,
    method: 'get'
  })
}

export function cancelRuntimeSessionQueueItem(sessionId, queueItemId) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/queue-items/${encodeURIComponent(queueItemId)}`,
    method: 'delete'
  })
}

export function reorderRuntimeSessionQueueItems(sessionId, data = {}) {
  return request({
    url: `/ai/agent-chatbox/sessions/${sessionId}/queue-items/reorder`,
    method: 'post',
    data
  })
}
