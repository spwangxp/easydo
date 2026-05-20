import request from './request'
import {
  createWorkspaceAIAgent,
  createWorkspaceAIRuntimeProfile,
  deleteWorkspaceAIAgent,
  deleteWorkspaceAIRuntimeProfile,
  getWorkspaceAIAgents,
  getWorkspaceAIModelCatalog,
  getWorkspaceAIRuntimeProfiles,
  updateWorkspaceAIAgent,
  updateWorkspaceAIRuntimeProfile
} from './agent'

export function getTemplateList(params) {
  return request({
    url: '/store/templates',
    method: 'get',
    params
  })
}

export function getTemplateDetail(id) {
  return request({
    url: `/store/templates/${id}`,
    method: 'get'
  })
}

export function createTemplate(data) {
  return request({
    url: '/store/templates',
    method: 'post',
    data
  })
}

export function updateTemplate(id, data) {
  return request({
    url: `/store/templates/${id}`,
    method: 'put',
    data
  })
}

export function deleteTemplate(id) {
  return request({
    url: `/store/templates/${id}`,
    method: 'delete'
  })
}

export function getTemplateVersions(id) {
  return request({
    url: `/store/templates/${id}/versions`,
    method: 'get'
  })
}

export function createTemplateVersion(id, data) {
  if (data instanceof FormData) {
    return request({
      url: `/store/templates/${id}/versions`,
      method: 'post',
      data,
      headers: {
        'Content-Type': 'multipart/form-data'
      }
    })
  }
  return request({
    url: `/store/templates/${id}/versions`,
    method: 'post',
    data
  })
}

export function updateTemplateVersion(templateId, versionId, data) {
  if (data instanceof FormData) {
    return request({
      url: `/store/templates/${templateId}/versions/${versionId}`,
      method: 'put',
      data,
      headers: {
        'Content-Type': 'multipart/form-data'
      }
    })
  }
  return request({
    url: `/store/templates/${templateId}/versions/${versionId}`,
    method: 'put',
    data
  })
}

export function deleteTemplateVersion(templateId, versionId) {
  return request({
    url: `/store/templates/${templateId}/versions/${versionId}`,
    method: 'delete'
  })
}

export function previewTemplateVersion(templateId, versionId, data) {
  return request({
    url: `/store/templates/${templateId}/versions/${versionId}/preview`,
    method: 'post',
    data
  })
}

export function resolveTemplateChartSource(templateId, data) {
  return request({
    url: `/store/templates/${templateId}/chart/resolve`,
    method: 'post',
    data
  })
}

export function uploadTemplateVersionChart(templateId, versionId, file) {
  const formData = new FormData()
  formData.append('file', file)
  return request({
    url: `/store/templates/${templateId}/versions/${versionId}/chart/upload`,
    method: 'post',
    data: formData,
    headers: {
      'Content-Type': 'multipart/form-data'
    }
  })
}

export function getAIModelCatalog(params) {
  return getWorkspaceAIModelCatalog(params)
}

export function importAIModel(data) {
  return request({
		url: '/store/ai-models/import',
		method: 'post',
		data
	})
}

export function getAIProviders() {
	return request({
		url: '/store/ai-providers',
		method: 'get'
	})
}

export function createAIProvider(data) {
	return request({
		url: '/store/ai-providers',
		method: 'post',
		data
	})
}

export function updateAIProvider(id, data) {
	return request({
		url: `/store/ai-providers/${id}`,
		method: 'put',
		data
	})
}

export function deleteAIProvider(id) {
	return request({
		url: `/store/ai-providers/${id}`,
		method: 'delete'
	})
}

export function getAIModelBindings(providerId) {
	return request({
		url: `/store/ai-providers/${providerId}/model-bindings`,
		method: 'get'
	})
}

export function createAIModelBinding(providerId, data) {
	return request({
		url: `/store/ai-providers/${providerId}/model-bindings`,
		method: 'post',
		data
	})
}

export function updateAIModelBinding(providerId, bindingId, data) {
	return request({
		url: `/store/ai-providers/${providerId}/model-bindings/${bindingId}`,
		method: 'put',
		data
	})
}

export function deleteAIModelBinding(providerId, bindingId) {
	return request({
		url: `/store/ai-providers/${providerId}/model-bindings/${bindingId}`,
		method: 'delete'
	})
}

export function getAIAgents() {
	return getWorkspaceAIAgents()
}

export function createAIAgent(data) {
	return createWorkspaceAIAgent(data)
}

export function updateAIAgent(id, data) {
	return updateWorkspaceAIAgent(id, data)
}

export function deleteAIAgent(id) {
	return deleteWorkspaceAIAgent(id)
}

export function getAIRuntimeProfiles() {
	return getWorkspaceAIRuntimeProfiles()
}

export function createAIRuntimeProfile(data) {
	return createWorkspaceAIRuntimeProfile(data)
}

export function updateAIRuntimeProfile(id, data) {
	return updateWorkspaceAIRuntimeProfile(id, data)
}

export function deleteAIRuntimeProfile(id) {
	return deleteWorkspaceAIRuntimeProfile(id)
}

export function listAIModels(params) {
	return getAIModelCatalog(params)
}

export function importLocalAIModel(data) {
	return importAIModel(data)
}

export function listAIProviders() {
	return getAIProviders()
}

export function listAIAgents() {
	return getAIAgents()
}

export async function listAIRuntimeProfiles() {
	return getAIRuntimeProfiles()
}
