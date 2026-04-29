import { normalizeGpuDevices } from './aiVramEstimate.js'

const ALLOWED_STATUSES = new Set(['idle', 'loading', 'ready', 'error', 'unsupported'])

const normalizeObjectField = (value) => (value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {})

const normalizeBaseInfoStatus = (resource = {}) => String(resource.baseInfoStatus || resource.base_info_status || '').trim()

const normalizeBaseInfoCollectedAt = (resource = {}) => Number(resource.baseInfoCollectedAt || resource.base_info_collected_at || 0) || 0

const normalizeBaseInfoLastError = (resource = {}) => String(resource.baseInfoLastError || resource.base_info_last_error || '')

const resolveBaseInfoSnapshot = (resource = {}) => normalizeObjectField(resource.baseInfo || resource.base_info)

const resolveGpuDeviceCandidates = (resource = {}, baseInfo = {}) => {
  const candidates = [
    baseInfo?.machine?.gpu?.devices,
    resource.gpuDevices,
    resource.gpu_devices,
    resource.machine?.gpu?.devices,
    resource.gpu?.devices
  ]

  return candidates.find((devices) => Array.isArray(devices)) || []
}

const hasUsableGpuDevices = (gpuDevices = []) => gpuDevices.some((device) => Number(device?.memoryBytes || 0) > 0)

/**
 * @param {object} overrides
 * @returns {{status:'idle'|'loading'|'ready'|'error'|'unsupported',requestedAt:number,refreshToken:string,error:string,gpuDevices:Array<unknown>,baseInfoStatus:string,baseInfoCollectedAt:number,baseInfoSnapshot:object}}
 */
export const createGpuInfoCacheEntry = (overrides = {}) => ({
  status: 'idle',
  requestedAt: 0,
  refreshToken: '',
  error: '',
  gpuDevices: [],
  baseInfoStatus: '',
  baseInfoCollectedAt: 0,
  baseInfoSnapshot: normalizeObjectField(overrides.baseInfoSnapshot),
  ...overrides,
  baseInfoSnapshot: normalizeObjectField(overrides.baseInfoSnapshot)
})

/**
 * @param {object} entry
 * @returns {boolean}
 */
export const shouldRefreshResourceGpuInfo = (entry) => {
  if (!entry) return true
  return entry.status !== 'loading' && entry.status !== 'ready'
}

/**
 * @param {object} resource
 * @returns {{status:'idle'|'loading'|'ready'|'error'|'unsupported',requestedAt:number,refreshToken:string,error:string,gpuDevices:Array<unknown>,baseInfoStatus:string,baseInfoCollectedAt:number,baseInfoSnapshot:object}}
 */
export const normalizeResourceGpuInfo = (resource = {}) => {
  const baseInfoSnapshot = resolveBaseInfoSnapshot(resource)
  const baseInfoStatus = normalizeBaseInfoStatus(resource)
  const baseInfoCollectedAt = normalizeBaseInfoCollectedAt(resource)
  const error = normalizeBaseInfoLastError(resource)
  const gpuDevices = normalizeGpuDevices(resolveGpuDeviceCandidates(resource, baseInfoSnapshot))

  if (baseInfoStatus === 'failed') {
    return createGpuInfoCacheEntry({
      status: 'error',
      error,
      gpuDevices,
      baseInfoStatus,
      baseInfoCollectedAt,
      baseInfoSnapshot
    })
  }

  if (hasUsableGpuDevices(gpuDevices)) {
    return createGpuInfoCacheEntry({
      status: 'ready',
      error: '',
      gpuDevices,
      baseInfoStatus,
      baseInfoCollectedAt,
      baseInfoSnapshot
    })
  }

  if (baseInfoStatus === 'pending' || baseInfoStatus === 'running') {
    return createGpuInfoCacheEntry({
      status: 'loading',
      error: '',
      gpuDevices: [],
      baseInfoStatus,
      baseInfoCollectedAt,
      baseInfoSnapshot
    })
  }

  return createGpuInfoCacheEntry({
    status: 'unsupported',
    error,
    gpuDevices: [],
    baseInfoStatus,
    baseInfoCollectedAt,
    baseInfoSnapshot
  })
}

/**
 * @param {object} cacheEntry
 * @param {object} update
 * @returns {object}
 */
export const createGpuRefreshToken = () => `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`

/**
 * @param {object} entry
 * @param {string} refreshToken
 * @param {number} requestedAt
 * @returns {{started:boolean,entry:object}}
 */
export const beginGpuInfoRefresh = (entry, refreshToken, requestedAt = Date.now()) => {
  const currentEntry = createGpuInfoCacheEntry(entry || {})

  if (!shouldRefreshResourceGpuInfo(currentEntry)) {
    return {
      started: false,
      entry: currentEntry
    }
  }

  return {
    started: true,
    entry: createGpuInfoCacheEntry({
      ...currentEntry,
      status: 'loading',
      requestedAt,
      refreshToken: String(refreshToken || createGpuRefreshToken()),
      error: '',
      gpuDevices: []
    })
  }
}

/**
 * @param {object} cacheEntry
 * @param {object} update
 * @returns {object}
 */
export const applyGpuInfoTerminalState = (cacheEntry, update = {}) => {
  const currentEntry = createGpuInfoCacheEntry(cacheEntry || {})
  const nextStatus = String(update.status || '')
  const nextToken = String(update.refreshToken || '')

  if (!currentEntry.refreshToken || !nextToken || currentEntry.refreshToken !== nextToken) {
    return currentEntry
  }

  if (!ALLOWED_STATUSES.has(nextStatus) || nextStatus === 'idle' || nextStatus === 'loading') {
    return currentEntry
  }

  return createGpuInfoCacheEntry({
    ...currentEntry,
    ...update,
    status: nextStatus,
    refreshToken: nextToken
  })
}

/**
 * @param {object} entry
 * @param {string} refreshToken
 * @param {string} message
 * @returns {object}
 */
export const markGpuInfoTimeout = (entry, refreshToken, message = 'GPU 信息采集超时') => applyGpuInfoTerminalState(entry, {
  status: 'error',
  refreshToken,
  error: message
})

/**
 * @param {object} entry
 * @returns {object}
 */
export const invalidateGpuInfoRefresh = (entry) => {
  const currentEntry = createGpuInfoCacheEntry(entry || {})

  if (currentEntry.status === 'loading') {
    return createGpuInfoCacheEntry({
      ...currentEntry,
      status: 'idle',
      requestedAt: 0,
      refreshToken: ''
    })
  }

  return createGpuInfoCacheEntry({
    ...currentEntry,
    refreshToken: ''
  })
}

/**
 * @param {Record<string, object>} cache
 * @param {string|number} resourceId
 * @param {string} refreshToken
 * @param {object} resource
 * @returns {Record<string, object>}
 */
export const resolveGpuInfoTerminalState = (cache, resourceId, refreshToken, resource = {}) => {
  const cacheKey = String(resourceId)
  const currentEntry = createGpuInfoCacheEntry(cache?.[cacheKey] || {})
  const normalizedEntry = normalizeResourceGpuInfo(resource)

  return {
    ...(cache || {}),
    [cacheKey]: applyGpuInfoTerminalState(currentEntry, {
      ...normalizedEntry,
      refreshToken
    })
  }
}
