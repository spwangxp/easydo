import request from './request'

// =============================================================================
// 凭据管理 API
// =============================================================================

/**
 * 获取凭据列表
 * @param {Object} params - 查询参数
 * @param {number} params.page - 页码
 * @param {number} params.size - 每页数量
 * @param {string} params.type - 凭据类型筛选
 * @param {string} params.category - 分类筛选
 * @param {string} params.scope - 范围筛选
 * @param {string} params.status - 状态筛选
 * @param {string} params.keyword - 关键词搜索
 */
export function getCredentialList(params) {
  return request({
    url: '/v1/credentials',
    method: 'get',
    params
  })
}

/**
 * 创建凭据
 * @param {Object} data - 凭据数据
 * @param {string} data.name - 凭据名称
 * @param {string} data.type - 凭据类型
 * @param {string} data.category - 分类
 * @param {Object} data.secret_data - 敏感数据（加密存储）
 * @param {string} data.description - 描述
 * @param {string} data.scope - 使用范围
 * @param {number} data.project_id - 项目ID
 * @param {number} data.expires_at - 过期时间戳
 */
export function createCredential(data) {
  return request({
    url: '/v1/credentials',
    method: 'post',
    data
  })
}

/**
 * 获取凭据详情
 * @param {number} id - 凭据ID
 */
export function getCredential(id) {
  return request({
    url: `/v1/credentials/${id}`,
    method: 'get'
  })
}

/**
 * 获取凭据敏感数据（用于编辑回填）
 * @param {number} id - 凭据ID
 */
export function getCredentialSecretData(id) {
  return request({
    url: `/v1/credentials/${id}/secret-data`,
    method: 'get'
  })
}

/**
 * 更新凭据
 * @param {number} id - 凭据ID
 * @param {Object} data - 更新数据
 */
export function updateCredential(id, data) {
  return request({
    url: `/v1/credentials/${id}`,
    method: 'put',
    data
  })
}

/**
 * 删除凭据
 * @param {number} id - 凭据ID
 */
export function deleteCredential(id) {
  return request({
    url: `/v1/credentials/${id}`,
    method: 'delete'
  })
}

/**
 * 验证凭据有效性
 * @param {number} id - 凭据ID
 */
export function verifyCredential(id) {
  return request({
    url: `/v1/credentials/${id}/verify`,
    method: 'post'
  })
}

/**
 * 获取凭据类型列表
 * @returns {Promise<{code: number, data: Array}>}
 */
export function getCredentialTypes() {
  return request({
    url: '/v1/credentials/types',
    method: 'get'
  })
}

/**
 * 获取凭据分类列表
 * @returns {Promise<{code: number, data: Array}>}
 */
export function getCredentialCategories() {
  return request({
    url: '/v1/credentials/categories',
    method: 'get'
  })
}

// =============================================================================
// 凭据轮换和管理 API
// =============================================================================

/**
 * 轮换凭据
 * @param {number} id - 凭据ID
 * @param {Object} data - 轮换数据
 * @param {Object} data.secret_data - 新的敏感数据
 * @param {string} data.reason - 轮换原因
 */
export function rotateCredential(id, data) {
  return request({
    url: `/v1/credentials/${id}/rotate`,
    method: 'post',
    data
  })
}

/**
 * 获取凭据使用统计
 * @param {number} id - 凭据ID
 */
export function getCredentialUsage(id) {
  return request({
    url: `/v1/credentials/${id}/usage`,
    method: 'get'
  })
}

/**
 * 获取凭据影响范围
 * @param {number} id - 凭据ID
 */
export function getCredentialImpact(id) {
  return request({
    url: `/v1/credentials/${id}/impact`,
    method: 'get'
  })
}

/**
 * 批量获取凭据影响范围
 * @param {Array<number>} ids - 凭据ID列表
 */
export function batchCredentialImpact(ids) {
  return request({
    url: '/v1/credentials/impact',
    method: 'post',
    data: { ids }
  })
}

/**
 * 批量验证凭据
 * @param {Array<number>} ids - 凭据ID列表
 */
export function batchVerifyCredentials(ids) {
  return request({
    url: '/v1/credentials/batch/verify',
    method: 'post',
    data: { ids }
  })
}

/**
 * 批量删除凭据
 * @param {Array<number>} ids - 凭据ID列表
 */
export function batchDeleteCredentials(ids) {
  return request({
    url: '/v1/credentials/batch/delete',
    method: 'post',
    data: { ids }
  })
}

/**
 * 导出凭据
 * @param {Object} params - 导出参数
 * @param {string} params.type - 凭据类型
 * @param {string} params.category - 分类
 */
export function exportCredentials(params) {
  return request({
    url: '/v1/credentials/export',
    method: 'get',
    params,
    responseType: 'blob'
  })
}
