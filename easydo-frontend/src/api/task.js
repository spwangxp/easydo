import request from './request'

export function getTaskList(params) {
  return request({
    url: '/tasks',
    method: 'get',
    params
  })
}

export function getTaskDispatchList(params = {}) {
  return request({
    url: '/tasks',
    method: 'get',
    params: {
      include_schedule: 1,
      ...params
    }
  })
}

export function getTaskDetail(id) {
  return request({
    url: `/tasks/${id}`,
    method: 'get'
  })
}

export function getTaskLogs(id, params) {
  return request({
    url: `/tasks/${id}/logs`,
    method: 'get',
    params
  })
}

export function cancelTask(id) {
  return request({
    url: `/tasks/${id}/cancel`,
    method: 'post'
  })
}

export function retryTask(id) {
  return request({
    url: `/tasks/${id}/retry`,
    method: 'post'
  })
}

export function getPendingTasks(agentId) {
  return request({
    url: `/tasks/agent/${agentId}/pending`,
    method: 'get'
  })
}
