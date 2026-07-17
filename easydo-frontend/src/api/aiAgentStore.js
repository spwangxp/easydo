import request from './request'

export function listAgentProfiles(params) {
  return request({
    url: '/store/ai-agents/profiles',
    method: 'get',
    params
  })
}

export function createAgentProfile(data) {
  return request({
    url: '/store/ai-agents/profiles',
    method: 'post',
    data
  })
}

export function getAgentProfile(id) {
  return request({
    url: `/store/ai-agents/profiles/${id}`,
    method: 'get'
  })
}

export function updateAgentProfile(id, data) {
  return request({
    url: `/store/ai-agents/profiles/${id}`,
    method: 'put',
    data
  })
}

export function deleteAgentProfile(id) {
  return request({
    url: `/store/ai-agents/profiles/${id}`,
    method: 'delete'
  })
}

export function publishAgentProfile(id, data) {
  return request({
    url: `/store/ai-agents/profiles/${id}/publish`,
    method: 'post',
    data
  })
}

export function validateAgentProfile(id) {
  return request({
    url: `/store/ai-agents/profiles/${id}/validate`,
    method: 'post',
    data: {}
  })
}

export function listAgentProfileVersions(id) {
  return request({
    url: `/store/ai-agents/profiles/${id}/versions`,
    method: 'get'
  })
}

export function getAgentProfileDependencies(id) {
  return request({
    url: `/store/ai-agents/profiles/${id}/dependencies`,
    method: 'get'
  })
}

export function listAgentResources() {
  return request({
    url: '/store/ai-agents/resources',
    method: 'get'
  })
}

export function listAgentResourceVersions(id) {
  return request({
    url: `/store/ai-agents/resources/${id}/versions`,
    method: 'get'
  })
}

export function getAgentResourceDependencies(id) {
  return request({
    url: `/store/ai-agents/resources/${id}/dependencies`,
    method: 'get'
  })
}

export function createAgentResource(data) {
  return request({
    url: '/store/ai-agents/resources',
    method: 'post',
    data
  })
}

export function updateAgentResource(id, data) {
  return request({
    url: `/store/ai-agents/resources/${id}`,
    method: 'put',
    data
  })
}

export function scanAgentResource(id, data = {}) {
  return request({
    url: `/store/ai-agents/resources/${id}/scan`,
    method: 'post',
    data
  })
}

export function probeMcpResource(data = {}) {
  return request({
    url: '/store/ai-agents/resources/mcp/probe',
    method: 'post',
    data
  })
}

export function deleteAgentResource(id) {
  return request({
    url: `/store/ai-agents/resources/${id}`,
    method: 'delete'
  })
}

export function getAgentRuntimeOperationsSummary() {
  return request({
    url: '/store/ai-agents/operations/summary',
    method: 'get'
  })
}
