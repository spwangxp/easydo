import request from './request'

// Agent APIs
export function getAgentList(params) {
  return request({
    url: '/agents',
    method: 'get',
    params
  })
}

export function getAgentDetail(id) {
  return request({
    url: `/agents/${id}`,
    method: 'get'
  })
}

export function createAgent(data) {
  return request({
    url: '/agents/register',
    method: 'post',
    data
  })
}

export function updateAgent(id, data) {
  return request({
    url: `/agents/${id}`,
    method: 'put',
    data
  })
}

export function deleteAgent(id) {
  return request({
    url: `/agents/${id}`,
    method: 'delete'
  })
}

export function getAgentHeartbeats(id, params) {
  return request({
    url: `/agents/${id}/heartbeats`,
    method: 'get',
    params
  })
}

// Task APIs
export function getTaskList(params) {
  return request({
    url: '/tasks',
    method: 'get',
    params
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

export function createTask(data) {
  return request({
    url: '/tasks',
    method: 'post',
    data
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

// Agent Approval APIs
export function getPendingAgents(params) {
  return request({
    url: '/agents/pending',
    method: 'get',
    params
  })
}

export function approveAgent(id, data) {
  return request({
    url: `/agents/${id}/approve`,
    method: 'post',
    data
  })
}

export function rejectAgent(id, data) {
  return request({
    url: `/agents/${id}/reject`,
    method: 'post',
    data
  })
}

export function refreshAgentToken(id) {
  return request({
    url: `/agents/${id}/refresh-token`,
    method: 'post'
  })
}

export function removeAgent(id) {
  return request({
    url: `/agents/${id}/remove`,
    method: 'post'
  })
}

export function getWorkspaceAIAgents() {
  return request({
    url: '/ai/agents',
    method: 'get'
  })
}

export function createWorkspaceAIAgent(data) {
  return request({
    url: '/ai/agents',
    method: 'post',
    data
  })
}

export function updateWorkspaceAIAgent(id, data) {
  return request({
    url: `/ai/agents/${id}`,
    method: 'put',
    data
  })
}

export function deleteWorkspaceAIAgent(id) {
  return request({
    url: `/ai/agents/${id}`,
    method: 'delete'
  })
}

export function getWorkspaceAIRuntimeProfiles() {
  return request({
    url: '/ai/runtime-profiles',
    method: 'get'
  })
}

export function createWorkspaceAIRuntimeProfile(data) {
  return request({
    url: '/ai/runtime-profiles',
    method: 'post',
    data
  })
}

export function updateWorkspaceAIRuntimeProfile(id, data) {
  return request({
    url: `/ai/runtime-profiles/${id}`,
    method: 'put',
    data
  })
}

export function deleteWorkspaceAIRuntimeProfile(id) {
  return request({
    url: `/ai/runtime-profiles/${id}`,
    method: 'delete'
  })
}

export function getWorkspaceAIModelCatalog(params) {
  return request({
    url: '/store/ai-models',
    method: 'get',
    params
  })
}
