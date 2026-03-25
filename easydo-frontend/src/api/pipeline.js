import request from './request'

export function getPipelineList(params) {
  return request({
    url: '/pipelines',
    method: 'get',
    params
  })
}

export function getPipelineDetail(id) {
  return request({
    url: `/pipelines/${id}`,
    method: 'get'
  })
}

export function getPipelineTaskTypes() {
  return request({
    url: '/pipelines/task-types',
    method: 'get'
  })
}

export function createPipeline(data) {
  return request({
    url: '/pipelines',
    method: 'post',
    data
  })
}

export function updatePipeline(id, data) {
  return request({
    url: `/pipelines/${id}`,
    method: 'put',
    data
  })
}

export function deletePipeline(id) {
  return request({
    url: `/pipelines/${id}`,
    method: 'delete'
  })
}

export function runPipeline(id) {
  return request({
    url: `/pipelines/${id}/run`,
    method: 'post'
  })
}

export function getPipelineHistory(id, params) {
  return request({
    url: `/pipelines/${id}/history`,
    method: 'get',
    params
  })
}

export function toggleFavorite(id) {
  return request({
    url: `/pipelines/${id}/favorite`,
    method: 'post'
  })
}

export function getPipelineRuns(id, params) {
  return request({
    url: `/pipelines/${id}/runs`,
    method: 'get',
    params
  })
}

export function getPipelineStatistics(id, params) {
  return request({
    url: `/pipelines/${id}/statistics`,
    method: 'get',
    params
  })
}

export function getPipelineTriggers(id) {
  return request({
    url: `/pipelines/${id}/triggers`,
    method: 'get'
  })
}

export function updatePipelineTriggers(id, data) {
  return request({
    url: `/pipelines/${id}/triggers`,
    method: 'put',
    data
  })
}

export function getPipelineTestReports(id, params) {
  return request({
    url: `/pipelines/${id}/test-reports`,
    method: 'get',
    params
  })
}

export function getRunTasks(id, runId) {
  return request({
    url: `/pipelines/${id}/runs/${runId}/tasks`,
    method: 'get'
  })
}

export function getRunLogs(id, runId, params) {
  return request({
    url: `/pipelines/${id}/runs/${runId}/logs`,
    method: 'get',
    params
  })
}

export function cancelPipelineRun(id, runId) {
  return request({
    url: `/pipelines/${id}/runs/${runId}/cancel`,
    method: 'post'
  })
}
