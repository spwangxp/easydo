import request from './request'

export function getProjectList(params) {
  return request({
    url: '/projects',
    method: 'get',
    params
  })
}

export function getProjectDetail(id) {
  return request({
    url: `/projects/${id}`,
    method: 'get'
  })
}

export function createProject(data) {
  return request({
    url: '/projects',
    method: 'post',
    data
  })
}

export function updateProject(id, data) {
  return request({
    url: `/projects/${id}`,
    method: 'put',
    data
  })
}

export function deleteProject(id) {
  return request({
    url: `/projects/${id}`,
    method: 'delete'
  })
}

export function toggleFavorite(id) {
  return request({
    url: `/projects/${id}/favorite`,
    method: 'post'
  })
}
