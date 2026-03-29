import request from './request'

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

export function getLocalLlmCatalog(params) {
  return request({
    url: '/store/llm-models',
    method: 'get',
    params
  })
}

export function importLocalLlmModel(data) {
  return request({
    url: '/store/llm-models/import',
    method: 'post',
    data
  })
}
