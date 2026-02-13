import request from './request'

export function getSecretList(params) {
  return request({
    url: '/secrets',
    method: 'get',
    params
  })
}

export function getSecret(id) {
  return request({
    url: `/secrets/${id}`,
    method: 'get'
  })
}

export function createSecret(data) {
  return request({
    url: '/secrets',
    method: 'post',
    data
  })
}

export function updateSecret(id, data) {
  return request({
    url: `/secrets/${id}`,
    method: 'put',
    data
  })
}

export function deleteSecret(id) {
  return request({
    url: `/secrets/${id}`,
    method: 'delete'
  })
}

export function getSecretValue(id) {
  return request({
    url: `/secrets/${id}/value`,
    method: 'get'
  })
}

export function getSecretTypes() {
  return request({
    url: '/secrets/types',
    method: 'get'
  })
}

export function generateSSHKey(data) {
  return request({
    url: '/secrets/ssh/generate',
    method: 'post',
    data
  })
}

export function verifySecret(id) {
  return request({
    url: `/secrets/${id}/verify`,
    method: 'post'
  })
}

export function rotateSecret(id, data) {
  return request({
    url: `/secrets/${id}/rotate`,
    method: 'post',
    data
  })
}

export function getStatistics() {
  return request({
    url: '/secrets/statistics',
    method: 'get'
  })
}

export function batchDeleteSecrets(ids) {
  return request({
    url: '/secrets/batch-delete',
    method: 'post',
    data: ids
  })
}
