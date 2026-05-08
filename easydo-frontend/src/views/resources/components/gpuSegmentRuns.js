const normalizeNumber = (value) => {
  const number = Number(value || 0)
  return Number.isFinite(number) ? number : 0
}

const uniqueItems = (items = []) => {
  const seen = new Set()
  return items.filter(item => {
    const key = item == null ? '' : String(item)
    if (!key || seen.has(key)) return false
    seen.add(key)
    return true
  })
}

const getServiceId = (segment) => segment?.service?.id || segment?.serviceId || segment?.colorKey || segment?.id

const getGpuLabel = (resourceInstance, fallbackIndex) => {
  const index = resourceInstance?.identityMap?.index
  if (index != null && index !== '') return String(index)
  return String(fallbackIndex + 1)
}

const buildGpuMemoryUsages = (cells, segments, startIndex) => {
  return segments.map((segment, segmentIndex) => {
    const cell = cells[segmentIndex]
    const resourceInstance = cell?.resourceInstance || segment?.resourceInstance
    const gpuIndex = resourceInstance?.identityMap?.index
    return {
      gpuIndex,
      gpuLabel: getGpuLabel(resourceInstance, startIndex + segmentIndex),
      memoryUsedBytes: normalizeNumber(segment?.summary?.memoryUsedBytes)
    }
  })
}

const mergeServiceHover = (segments) => {
  const base = segments[0]?.serviceHover || null
  if (!base) return null
  const descendantProcesses = segments.flatMap(segment => segment?.serviceHover?.descendantProcesses || [])
  return {
    ...base,
    memoryUsedBytes: segments.reduce((total, segment) => total + normalizeNumber(segment?.serviceHover?.memoryUsedBytes ?? segment?.summary?.memoryUsedBytes), 0),
    pids: uniqueItems(segments.flatMap(segment => segment?.serviceHover?.pids || [])),
    observedPids: uniqueItems(segments.flatMap(segment => segment?.serviceHover?.observedPids || [])),
    descendantProcesses,
    descendantProcessCount: descendantProcesses.length
  }
}

const createSegmentRun = (serviceId, startIndex, cells, segments) => {
  const base = segments[0]
  const memoryUsedBytes = segments.reduce((total, segment) => total + normalizeNumber(segment?.summary?.memoryUsedBytes), 0)
  return {
    ...base,
    id: `gpu-run::${serviceId}::${startIndex}::${startIndex + segments.length - 1}`,
    serviceId,
    startColumn: startIndex + 1,
    span: segments.length,
    cellIds: cells.map(cell => cell.id),
    cells,
    segments,
    resourceInstances: cells.map(cell => cell.resourceInstance).filter(Boolean),
    claims: segments.flatMap(segment => segment?.claims || []),
    allocations: segments.flatMap(segment => segment?.allocations || []),
    allocationIds: uniqueItems(segments.flatMap(segment => segment?.allocationIds || [])),
    gpuMemoryUsages: buildGpuMemoryUsages(cells, segments, startIndex),
    gpuHover: segments.length === 1 ? base?.gpuHover : null,
    summary: {
      ...(base?.summary || {}),
      memoryUsedBytes
    },
    serviceHover: mergeServiceHover(segments)
  }
}

const buildRuns = (gpuCells) => {
  const serviceIds = uniqueItems(gpuCells.flatMap(cell => (cell?.segments || []).map(getServiceId)))
  const runs = []

  serviceIds.forEach(serviceId => {
    let index = 0
    while (index < gpuCells.length) {
      const firstSegment = (gpuCells[index]?.segments || []).find(segment => getServiceId(segment) === serviceId)
      if (!firstSegment) {
        index += 1
        continue
      }

      const runCells = []
      const runSegments = []
      const startIndex = index
      while (index < gpuCells.length) {
        const segment = (gpuCells[index]?.segments || []).find(item => getServiceId(item) === serviceId)
        if (!segment) break
        runCells.push(gpuCells[index])
        runSegments.push(segment)
        index += 1
      }
      runs.push(createSegmentRun(serviceId, startIndex, runCells, runSegments))
    }
  })

  return runs.sort((left, right) => {
    if (left.startColumn !== right.startColumn) return left.startColumn - right.startColumn
    if (left.span !== right.span) return right.span - left.span
    return String(left.label || '').localeCompare(String(right.label || ''), 'en')
  })
}

export const buildGpuSegmentLanes = (gpuCells = []) => {
  const lanes = []
  buildRuns(gpuCells).forEach(run => {
    let targetLane = lanes.find(lane => lane.every(item => item.startColumn + item.span <= run.startColumn || run.startColumn + run.span <= item.startColumn))
    if (!targetLane) {
      targetLane = []
      lanes.push(targetLane)
    }
    targetLane.push(run)
  })
  return lanes
}
