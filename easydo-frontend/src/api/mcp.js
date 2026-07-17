import request from './request'

export function getMcpConfig() {
  return request({
    url: '/mcp/config',
    method: 'get'
  })
}
