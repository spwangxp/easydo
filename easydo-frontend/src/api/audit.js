import request from './request'

export function getAuditLogs(params) {
  return request({
    url: '/audit-logs',
    method: 'get',
    params
  })
}

export function getWorkspaceAuditLogs(id, params) {
  return request({
    url: `/workspaces/${id}/audit-logs`,
    method: 'get',
    params
  })
}
