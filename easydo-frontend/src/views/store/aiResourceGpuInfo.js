import { normalizeGpuDevices } from './aiVramEstimate.js'

const ALLOWED_STATUSES = new Set(['idle', 'loading', 'ready', 'error', 'unsupported'])

const normalizeObjectField = (value) => (value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {})

const normalizeArrayField = (value) => (Array.isArray(value) ? [...value] : [])

const normalizeBaseInfoStatus = (resource = {}) => String(resource.baseInfoStatus || resource.base_info_status || '').trim()

const normalizeBaseInfoCollectedAt = (resource = {}) => Number(resource.baseInfoCollectedAt || resource.base_info_collected_at || 0) || 0

const normalizeBaseInfoLastError = (resource = {}) => String(resource.baseInfoLastError || resource.base_info_last_error || '')

const resolveBaseInfoSnapshot = (resource = {}) => normalizeObjectField(resource.baseInfo || resource.base_info)

const normalizeNamedArrayMap = (items) => Object.fromEntries(normalizeArrayField(items)
  .map((item) => [String(item?.name || item?.key || '').trim(), item])
  .filter(([name]) => Boolean(name)))

const pickMeasureNumber = (measure, keys) => {
  if (!measure) return 0
  for (const key of keys) {
    const value = Number(measure[key])
    if (Number.isFinite(value) && value !== 0) return value
  }
  for (const key of keys) {
    const value = Number(measure[key])
    if (Number.isFinite(value)) return value
  }
  return 0
}

const resolveMeasureMap = (resourceInstance = {}, fieldName, mapName) => {
  if (resourceInstance?.[mapName] && typeof resourceInstance[mapName] === 'object') {
    return resourceInstance[mapName]
  }
  return normalizeNamedArrayMap(resourceInstance?.[fieldName])
}

const resolveFieldMap = (resourceInstance = {}, fieldName, mapName) => {
  if (resourceInstance?.[mapName] && typeof resourceInstance[mapName] === 'object') {
    return resourceInstance[mapName]
  }
  return Object.fromEntries(normalizeArrayField(resourceInstance?.[fieldName])
    .map((item) => [String(item?.name || item?.key || '').trim(), item?.value])
    .filter(([name]) => Boolean(name)))
}

const resolveCanonicalGpuDevices = (baseInfo = {}) => normalizeArrayField(baseInfo.resourceInstances || baseInfo.resource_instances)
  .filter((resourceInstance) => String(resourceInstance?.resourceTypeId || resourceInstance?.resource_type_id || '').trim() === 'gpu')
  .map((resourceInstance, fallbackIndex) => {
    const identityMap = resolveFieldMap(resourceInstance, 'identity', 'identityMap')
    const specMap = resolveFieldMap(resourceInstance, 'spec', 'specMap')
    const capacityMap = resolveMeasureMap(resourceInstance, 'capacity', 'capacityMap')
    const metricsMap = resolveMeasureMap(resourceInstance, 'metrics', 'metricsMap')
    const index = Number.isFinite(Number(identityMap.index)) ? Number(identityMap.index) : fallbackIndex
    return {
      id: resourceInstance.id,
      index,
      uuid: identityMap.uuid || identityMap.deviceUUID || '',
      busId: identityMap.busId || identityMap.bus_id || '',
      vendor: specMap.vendor || '',
      model: specMap.model || specMap.name || '',
      name: specMap.model || specMap.name || `GPU ${index}`,
      memoryBytes: pickMeasureNumber(capacityMap.memoryBytes, ['capacity', 'allocatable', 'total', 'value']),
      memoryBytesAvailable: pickMeasureNumber(capacityMap.memoryBytesAvailable, ['available', 'value', 'allocatable']),
      memoryBytesUsed: pickMeasureNumber(metricsMap.memoryBytesUsed || capacityMap.memoryBytesUsed, ['value', 'used']),
      utilizationGpuPercent: pickMeasureNumber(metricsMap.utilizationGpuPercent, ['value', 'used']),
      temperatureGpuCelsius: pickMeasureNumber(metricsMap.temperatureGpuCelsius, ['value'])
    }
  })

const resolveGpuDeviceCandidates = (resource = {}, baseInfo = {}) => {
  const candidates = [
    resolveCanonicalGpuDevices(baseInfo),
    baseInfo?.machine?.gpu?.devices,
    resource.gpuDevices,
    resource.gpu_devices,
    resource.machine?.gpu?.devices,
    resource.gpu?.devices
  ]

  return candidates.find((devices) => Array.isArray(devices) && devices.length > 0) || []
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
