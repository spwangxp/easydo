import request from './request'

export function getStatsOverview(params = {}) {
  return request({
    url: '/stats/overview',
    method: 'get',
    params
  })
}

export function getStatsTrend(params = {}) {
  return request({
    url: '/stats/trend',
    method: 'get',
    params
  })
}

export function getTopPipelines(params = {}) {
  return request({
    url: '/stats/top-pipelines',
    method: 'get',
    params
  })
}
