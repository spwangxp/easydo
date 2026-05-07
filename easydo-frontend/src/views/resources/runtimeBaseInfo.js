/**
 * @param {unknown} value
 * @returns {Record<string, any>}
 */
function normalizeObject(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {}
}

/**
 * @param {unknown} value
 * @returns {Array<any>}
 */
function normalizeArray(value) {
  return Array.isArray(value) ? [...value] : []
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function normalizeString(value) {
  return typeof value === 'string' ? value : value == null ? '' : String(value)
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function normalizeNumber(value) {
  const number = Number(value)
  return Number.isFinite(number) ? number : 0
}

/**
 * @param {Array<string>} values
 * @returns {Array<string>}
 */
function uniqueStrings(values) {
  return [...new Set(values.map(item => normalizeString(item)).filter(Boolean))]
}

/**
 * @param {Array<any>} fields
 * @returns {Array<{name: string, value: any}>}
 */
function normalizeFields(fields) {
  return normalizeArray(fields)
    .map(item => ({
      name: normalizeString(item?.name),
      value: item?.value
    }))
    .filter(item => item.name)
}

/**
 * @param {Array<{name: string, value: any}>} fields
 * @returns {Record<string, any>}
 */
function buildFieldMap(fields) {
  return Object.fromEntries(fields.map(field => [field.name, field.value]))
}

/**
 * @param {Array<any>} measures
 * @returns {Array<Record<string, any>>}
 */
function normalizeMeasures(measures) {
  return normalizeArray(measures)
    .map(item => ({
      name: normalizeString(item?.name),
      value: item?.value,
      used: item?.used,
      total: item?.total,
      free: item?.free,
      allocatable: item?.allocatable,
      capacity: item?.capacity,
      available: item?.available,
      sourceReported: Boolean(item?.sourceReported)
    }))
    .filter(item => item.name)
}

/**
 * @param {Array<Record<string, any>>} measures
 * @returns {Record<string, Record<string, any>>}
 */
function buildMeasureMap(measures) {
  return Object.fromEntries(measures.map(measure => [measure.name, measure]))
}

/**
 * @param {Record<string, any>} measure
 * @param {Array<string>} keys
 * @returns {number}
 */
function pickMeasureNumber(measure, keys) {
  if (!measure) return 0
  for (const key of keys) {
    const value = normalizeNumber(measure[key])
    if (value) return value
  }
  return 0
}

/**
 * @param {Record<string, any>} item
 * @returns {string}
 */
function getItemId(item) {
  return normalizeString(item?.id)
}

/**
 * @param {Record<string, any>} resourceType
 * @returns {boolean}
 */
function isGpuResourceType(resourceType = {}) {
  return normalizeString(resourceType?.id) === 'gpu'
}

/**
 * @param {Record<string, any>} entity
 * @returns {boolean}
 */
function isNodeEntity(entity = {}) {
  return normalizeString(entity?.kind) === 'node'
}

/**
 * @param {Record<string, any>} entity
 * @returns {boolean}
 */
function isHostEntity(entity = {}) {
  return normalizeString(entity?.kind) === 'host'
}

/**
 * @param {Record<string, any>} resourceInstance
 * @returns {boolean}
 */
function isPoolResource(resourceInstance = {}) {
  return normalizeString(resourceInstance?.identityMap?.pool) !== ''
}

/**
 * @param {Array<Record<string, any>>} resourceInstances
 * @param {string} resourceTypeId
 * @param {string} poolName
 * @returns {Record<string, any> | null}
 */
function findPoolResource(resourceInstances, resourceTypeId, poolName) {
  return resourceInstances.find(item => item.resourceTypeId === resourceTypeId && item.identityMap.pool === poolName) || null
}

/**
 * @param {Record<string, any>} resourceInstance
 * @returns {string}
 */
function getResourceInstanceDisplayName(resourceInstance = {}) {
  const entityName = normalizeString(resourceInstance?.entity?.name)
  const resourceTypeName = normalizeString(resourceInstance?.resourceType?.name) || normalizeString(resourceInstance?.resourceTypeId)
  const identityName = normalizeString(resourceInstance?.identityMap?.name)
  const index = resourceInstance?.identityMap?.index
  const model = normalizeString(resourceInstance?.specMap?.model)
  const pool = normalizeString(resourceInstance?.identityMap?.pool)
  if (identityName) return identityName
  if (pool && entityName) return `${entityName} / ${resourceTypeName}`
  if (index !== '' && index !== undefined && index !== null && entityName) return `${entityName} / GPU ${index}`
  if (model && entityName) return `${entityName} / ${model}`
  return entityName || resourceTypeName || normalizeString(resourceInstance?.id)
}

/**
 * @param {Record<string, any>} service
 * @returns {string}
 */
function getServiceDisplayName(service = {}) {
  return normalizeString(service?.name) || normalizeString(service?.fieldsMap?.uid) || normalizeString(service?.id)
}

/**
 * @param {Array<Record<string, any>>} claims
 * @returns {Record<string, any>}
 */
function summarizeClaimDimensions(claims) {
  const dimensions = claims.flatMap(item => item.dimensions || [])
  const dimensionMap = buildFieldMap(dimensions)
  return {
    pid: dimensionMap.pid,
    podUid: dimensionMap.podUid,
    memoryUsedBytes: normalizeNumber(dimensionMap.memoryUsedBytes)
  }
}

/**
 * @param {Record<string, any>} entity
 * @param {Array<Record<string, any>>} resourceInstances
 * @returns {Record<string, any>}
 */
function buildHostSummary(entity, resourceInstances) {
  const cpuPool = findPoolResource(resourceInstances, 'cpu', 'cpu')
  const memoryPool = findPoolResource(resourceInstances, 'memory', 'memory')
  const storagePool = findPoolResource(resourceInstances, 'storage', 'storage')
  const gpuResources = resourceInstances.filter(item => item.resourceTypeId === 'gpu')
  return {
    entityId: entity?.id || '',
    hostname: normalizeString(entity?.fieldsMap?.hostname) || normalizeString(entity?.name),
    primaryIpv4: normalizeString(entity?.fieldsMap?.primaryIpv4),
    osName: normalizeString(entity?.fieldsMap?.osName),
    osVersion: normalizeString(entity?.fieldsMap?.osVersion),
    kernelVersion: normalizeString(entity?.fieldsMap?.kernelVersion),
    arch: normalizeString(entity?.fieldsMap?.arch),
    cpuModel: normalizeString(cpuPool?.specMap?.model),
    cpuLogicalCores: pickMeasureNumber(cpuPool?.capacityMap?.logicalCores, ['allocatable', 'capacity', 'total', 'value']),
    cpuUsedCores: pickMeasureNumber(cpuPool?.capacityMap?.logicalCoresUsed, ['used', 'value']),
    memoryBytes: pickMeasureNumber(memoryPool?.capacityMap?.memoryBytes, ['allocatable', 'capacity', 'total', 'value']),
    memoryBytesUsed: pickMeasureNumber(memoryPool?.capacityMap?.memoryBytesUsed, ['used', 'value']),
    diskBytes: pickMeasureNumber(storagePool?.capacityMap?.diskBytes, ['capacity', 'allocatable', 'total', 'value']),
    rootDiskBytes: pickMeasureNumber(storagePool?.capacityMap?.rootDiskBytes, ['capacity', 'allocatable', 'total', 'value']),
    gpuCount: gpuResources.length,
    gpuResources,
    gpuModels: uniqueStrings(gpuResources.map(item => normalizeString(item?.specMap?.model))).filter(Boolean),
    resourceInstances
  }
}

/**
 * @param {Array<Record<string, any>>} nodeEntities
 * @param {Record<string, Array<Record<string, any>>>} resourceInstancesByEntityId
 * @returns {{clusterVersion: string, nodeCount: number, cpuAllocatableMilli: number, memoryAllocatableBytes: number, gpuAllocatable: number, nodeSummaries: Array<Record<string, any>>}}
 */
function buildNodeSummaries(nodeEntities, resourceInstancesByEntityId) {
  const nodeSummaries = nodeEntities.map(entity => {
    const resourceInstances = resourceInstancesByEntityId[entity.id] || []
    const cpuPool = findPoolResource(resourceInstances, 'cpu', 'cpu')
    const memoryPool = findPoolResource(resourceInstances, 'memory', 'memory')
    const gpuResources = resourceInstances.filter(item => item.resourceTypeId === 'gpu')
    return {
      entityId: entity.id,
      name: normalizeString(entity.name) || normalizeString(entity.id),
      roles: normalizeArray(entity.fieldsMap.roles).map(item => normalizeString(item)).filter(Boolean),
      arch: normalizeString(entity.fieldsMap.arch),
      osImage: normalizeString(entity.fieldsMap.osImage),
      kubeletVersion: normalizeString(entity.fieldsMap.kubeletVersion),
      serverVersion: normalizeString(entity.fieldsMap.serverVersion),
      cpuAllocatableMilli: pickMeasureNumber(cpuPool?.capacityMap?.cpuMilli, ['allocatable', 'capacity', 'total', 'value']),
      cpuUsedMilli: pickMeasureNumber(cpuPool?.capacityMap?.cpuMilliUsed, ['used', 'value']),
      memoryAllocatableBytes: pickMeasureNumber(memoryPool?.capacityMap?.memoryBytes, ['allocatable', 'capacity', 'total', 'value']),
      memoryUsedBytes: pickMeasureNumber(memoryPool?.capacityMap?.memoryBytesUsed, ['used', 'value']),
      gpuAllocatable: gpuResources.length,
      gpuResources,
      resourceInstances
    }
  }).sort((left, right) => left.name.localeCompare(right.name, 'en'))

  return {
    clusterVersion: nodeSummaries.find(item => item.serverVersion)?.serverVersion || '',
    nodeCount: nodeSummaries.length,
    cpuAllocatableMilli: nodeSummaries.reduce((total, item) => total + item.cpuAllocatableMilli, 0),
    memoryAllocatableBytes: nodeSummaries.reduce((total, item) => total + item.memoryAllocatableBytes, 0),
    gpuAllocatable: nodeSummaries.reduce((total, item) => total + item.gpuAllocatable, 0),
    nodeSummaries
  }
}

/**
 * @param {Record<string, any>} payload
 * @returns {Record<string, any>}
 */
export function normalizeRuntimeBaseInfo(payload = {}) {
  const base = normalizeObject(payload)

  const entities = normalizeArray(base.entities)
    .map(item => {
      const fields = normalizeFields(item?.fields)
      return {
        ...normalizeObject(item),
        id: getItemId(item),
        kind: normalizeString(item?.kind),
        name: normalizeString(item?.name),
        fields,
        fieldsMap: buildFieldMap(fields)
      }
    })
    .filter(item => item.id)

  const resourceTypes = normalizeArray(base.resourceTypes)
    .map(item => ({
      ...normalizeObject(item),
      id: getItemId(item),
      name: normalizeString(item?.name),
      description: normalizeString(item?.description)
    }))
    .filter(item => item.id)

  const entitiesById = Object.fromEntries(entities.map(item => [item.id, item]))
  const resourceTypesById = Object.fromEntries(resourceTypes.map(item => [item.id, item]))

  const resourceInstances = normalizeArray(base.resourceInstances)
    .map(item => {
      const identity = normalizeFields(item?.identity)
      const spec = normalizeFields(item?.spec)
      const capacity = normalizeMeasures(item?.capacity)
      const metrics = normalizeMeasures(item?.metrics)
      return {
        ...normalizeObject(item),
        id: getItemId(item),
        resourceTypeId: normalizeString(item?.resourceTypeId),
        entityId: normalizeString(item?.entityId),
        identity,
        identityMap: buildFieldMap(identity),
        spec,
        specMap: buildFieldMap(spec),
        capacity,
        capacityMap: buildMeasureMap(capacity),
        metrics,
        metricsMap: buildMeasureMap(metrics)
      }
    })
    .filter(item => item.id)
    .map(item => ({
      ...item,
      entity: entitiesById[item.entityId] || null,
      resourceType: resourceTypesById[item.resourceTypeId] || null,
      displayName: getResourceInstanceDisplayName({
        ...item,
        entity: entitiesById[item.entityId] || null,
        resourceType: resourceTypesById[item.resourceTypeId] || null
      })
    }))

  const resourceInstancesById = Object.fromEntries(resourceInstances.map(item => [item.id, item]))

  const services = normalizeArray(base.services)
    .map(item => {
      const fields = normalizeFields(item?.fields)
      return {
        ...normalizeObject(item),
        id: getItemId(item),
        name: normalizeString(item?.name),
        entityId: normalizeString(item?.entityId),
        resourceInstanceIds: uniqueStrings(normalizeArray(item?.resourceInstanceIds)),
        fields,
        fieldsMap: buildFieldMap(fields)
      }
    })
    .filter(item => item.id)
    .map(item => ({
      ...item,
      entity: entitiesById[item.entityId] || null,
      displayName: getServiceDisplayName(item)
    }))

  const servicesById = Object.fromEntries(services.map(item => [item.id, item]))

  const allocations = normalizeArray(base.allocations)
    .map(item => ({
      ...normalizeObject(item),
      id: getItemId(item),
      claims: normalizeArray(item?.claims)
        .map((claim, index) => ({
          id: `${getItemId(item)}::${index}`,
          serviceId: normalizeString(claim?.serviceId),
          resourceInstanceId: normalizeString(claim?.resourceInstanceId),
          dimensions: normalizeFields(claim?.dimensions)
        }))
        .filter(claim => claim.serviceId && claim.resourceInstanceId)
    }))
    .filter(item => item.id)

  const allocationsById = Object.fromEntries(allocations.map(item => [item.id, item]))
  const resourceInstancesByEntityId = {}
  const claimsByServiceId = {}
  const claimsByResourceInstanceId = {}
  const claimsByCellKey = {}
  const activeServiceIds = []

  resourceInstances.forEach(item => {
    if (!item.entityId) return
    if (!resourceInstancesByEntityId[item.entityId]) resourceInstancesByEntityId[item.entityId] = []
    resourceInstancesByEntityId[item.entityId].push(item)
  })

  allocations.forEach(allocation => {
    allocation.claims.forEach(claim => {
      const service = servicesById[claim.serviceId]
      const resourceInstance = resourceInstancesById[claim.resourceInstanceId]
      if (!service || !resourceInstance) return
      const claimView = {
        ...claim,
        allocationId: allocation.id,
        service,
        resourceInstance,
        dimensionsMap: buildFieldMap(claim.dimensions)
      }
      if (!claimsByServiceId[claim.serviceId]) claimsByServiceId[claim.serviceId] = []
      if (!claimsByResourceInstanceId[claim.resourceInstanceId]) claimsByResourceInstanceId[claim.resourceInstanceId] = []
      const cellKey = `${claim.serviceId}::${claim.resourceInstanceId}`
      if (!claimsByCellKey[cellKey]) claimsByCellKey[cellKey] = []
      claimsByServiceId[claim.serviceId].push(claimView)
      claimsByResourceInstanceId[claim.resourceInstanceId].push(claimView)
      claimsByCellKey[cellKey].push(claimView)
      activeServiceIds.push(claim.serviceId)
    })
  })

  const gpuResourceInstances = resourceInstances
    .filter(item => isGpuResourceType(item.resourceType || { id: item.resourceTypeId }))
    .sort((left, right) => left.displayName.localeCompare(right.displayName, 'en'))

  const matrixRows = uniqueStrings(activeServiceIds)
    .map(serviceId => servicesById[serviceId])
    .filter(Boolean)
    .map(service => {
      const claims = claimsByServiceId[service.id] || []
      return {
        ...service,
        activeClaims: claims,
        activeClaimCount: claims.length,
        claimSummary: summarizeClaimDimensions(claims)
      }
    })
    .sort((left, right) => left.displayName.localeCompare(right.displayName, 'en'))

  const matrixColumns = gpuResourceInstances.map(resourceInstance => ({
    ...resourceInstance,
    activeClaims: claimsByResourceInstanceId[resourceInstance.id] || [],
    activeClaimCount: (claimsByResourceInstanceId[resourceInstance.id] || []).length
  }))

  const matrixCells = {}
  matrixRows.forEach(service => {
    matrixColumns.forEach(resourceInstance => {
      const cellKey = `${service.id}::${resourceInstance.id}`
      const claims = claimsByCellKey[cellKey] || []
      matrixCells[cellKey] = {
        key: cellKey,
        serviceId: service.id,
        resourceInstanceId: resourceInstance.id,
        service,
        resourceInstance,
        claims,
        claimCount: claims.length,
        summary: summarizeClaimDimensions(claims)
      }
    })
  })

  const hostEntity = entities.find(isHostEntity) || null
  const nodeEntities = entities.filter(isNodeEntity)
  const hostSummary = hostEntity ? buildHostSummary(hostEntity, resourceInstancesByEntityId[hostEntity.id] || []) : null
  const clusterSummary = buildNodeSummaries(nodeEntities, resourceInstancesByEntityId)

  return {
    schemaVersion: Number.isInteger(base.schemaVersion) ? base.schemaVersion : normalizeNumber(base.schemaVersion),
    resourceId: normalizeString(base.resourceId),
    status: normalizeString(base.status),
    source: normalizeString(base.source),
    collectedAt: normalizeString(base.collectedAt),
    labels: normalizeObject(base.labels),
    entities,
    resourceTypes,
    resourceInstances,
    services,
    allocations,
    entitiesById,
    resourceTypesById,
    resourceInstancesById,
    servicesById,
    allocationsById,
    resourceInstancesByEntityId,
    gpuResourceInstances,
    detail: {
      hostSummary,
      clusterSummary,
      nodeSummaries: clusterSummary.nodeSummaries
    },
    matrix: {
      rows: matrixRows,
      columns: matrixColumns,
      cells: matrixCells,
      rowCount: matrixRows.length,
      columnCount: matrixColumns.length,
      hasData: matrixRows.length > 0 && matrixColumns.length > 0
    },
    summary: {
      hasCanonicalData: entities.length > 0 || resourceInstances.length > 0 || services.length > 0 || allocations.length > 0,
      hostCount: entities.filter(isHostEntity).length,
      nodeCount: clusterSummary.nodeCount,
      serviceCount: services.length,
      gpuCount: gpuResourceInstances.length,
      poolResourceCount: resourceInstances.filter(isPoolResource).length
    }
  }
}
