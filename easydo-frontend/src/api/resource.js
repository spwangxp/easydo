import request from './request'

export function getResourceList(params) {
  return request({
    url: '/resources',
    method: 'get',
    params
  })
}

export function getResourceDetail(id) {
  return request({
    url: `/resources/${id}`,
    method: 'get'
  })
}

export function createResource(data) {
  return request({
    url: '/resources',
    method: 'post',
    data
  })
}

export function verifyResourceConnection(data) {
  return request({
    url: '/resources/verify',
    method: 'post',
    data
  })
}

export function refreshResourceBaseInfo(id) {
  return request({
    url: `/resources/${id}/base-info/refresh`,
    method: 'post'
  })
}

export function updateResource(id, data) {
  return request({
    url: `/resources/${id}`,
    method: 'put',
    data
  })
}

export function deleteResource(id) {
  return request({
    url: `/resources/${id}`,
    method: 'delete'
  })
}

export function getResourceCredentialBindings(id) {
  return request({
    url: `/resources/${id}/credentials`,
    method: 'get'
  })
}

export function bindResourceCredential(id, data) {
  return request({
    url: `/resources/${id}/credentials/bind`,
    method: 'post',
    data
  })
}

export function unbindResourceCredential(id, bindingId) {
  return request({
    url: `/resources/${id}/credentials/${bindingId}`,
    method: 'delete'
  })
}

export function getResourceK8sOverview(id) {
  return request({
    url: `/resources/${id}/k8s/overview`,
    method: 'get'
  })
}

export function queryResourceK8sNamespaces(id, data) {
  return request({
    url: `/resources/${id}/k8s/namespaces/query`,
    method: 'post',
    data
  })
}

export function queryResourceK8sResources(id, data) {
  return request({
    url: `/resources/${id}/k8s/resources/query`,
    method: 'post',
    data
  })
}

export function createResourceK8sAction(id, data) {
  return request({
    url: `/resources/${id}/k8s/actions`,
    method: 'post',
    data
  })
}

export function getResourceActionAudits(id, params) {
  return request({
    url: `/resources/${id}/actions`,
    method: 'get',
    params
  })
}
