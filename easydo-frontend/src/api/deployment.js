import request from './request'

export function getDeploymentRequestList(params) {
  return request({
    url: '/deployments/requests',
    method: 'get',
    params
  })
}

export function getDeploymentRequestDetail(id) {
  return request({
    url: `/deployments/requests/${id}`,
    method: 'get'
  })
}

export function createDeploymentRequest(data) {
  return request({
    url: '/deployments/requests',
    method: 'post',
    data
  })
}
