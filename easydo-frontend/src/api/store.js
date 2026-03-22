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
  return request({
    url: `/store/templates/${id}/versions`,
    method: 'post',
    data
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
