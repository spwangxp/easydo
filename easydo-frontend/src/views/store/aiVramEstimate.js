const BYTE_IN_GIB = 1024 ** 3

const DEFAULT_MODEL_PRECISION_BYTES = 2
const DEFAULT_KV_PRECISION_BYTES = 2
const DEFAULT_RUNTIME_RESERVE_BYTES = 1.5 * BYTE_IN_GIB

/**
 * @param {unknown} value
 * @returns {number}
 */
export const parseModelParameterCount = (value) => {
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value <= 0) return 0
    return value > 1_000_000 ? value : value * 1e9
  }

  const text = String(value || '').trim().toLowerCase().replace(/,/g, '')
  if (!text) return 0

  const unitMap = { t: 1e12, b: 1e9, m: 1e6, k: 1e3 }
  const mixedMatch = text.match(/(\d+(?:\.\d+)?)\s*x\s*(\d+(?:\.\d+)?)\s*([tbmk])/)
  if (mixedMatch) {
    return Number(mixedMatch[1]) * Number(mixedMatch[2]) * unitMap[mixedMatch[3]]
  }

  const directMatch = text.match(/(\d+(?:\.\d+)?)\s*([tbmk])/)
  if (directMatch) {
    return Number(directMatch[1]) * unitMap[directMatch[2]]
  }

  const numeric = Number(text.replace(/[^\d.]/g, ''))
  if (!Number.isFinite(numeric) || numeric <= 0) return 0
  return numeric > 1_000_000 ? numeric : numeric * 1e9
}

const pickFirstParameterValue = (parameterValues, keys) => {
  for (const key of keys) {
    const value = parameterValues?.[key]
    if (value !== undefined && value !== null && value !== '') {
      return value
    }
  }
  return undefined
}

/**
 * @param {{parameterValues?: Record<string, unknown>, model?: Record<string, unknown>}} input
 * @returns {number}
 */
const resolvePrecisionBytesFromHint = (hint, defaultValue) => {
  const precisionHint = String(hint || '').toLowerCase()
  if (!precisionHint) return defaultValue
  if (/(awq|gptq|nf4|int4|4bit|q4)/.test(precisionHint)) return 0.5
  if (/(int5|5bit|q5)/.test(precisionHint)) return 0.625
  if (/(int6|6bit|q6)/.test(precisionHint)) return 0.75
  if (/(fp8|int8|8bit|q8)/.test(precisionHint)) return 1
  if (/(fp32|float32|32bit)/.test(precisionHint)) return 4
  return defaultValue
}

export const resolveModelPrecisionBytes = ({ parameterValues = {}, model = {} } = {}) => {
  const parameterHint = pickFirstParameterValue(parameterValues, ['quantization', 'load_format', 'dtype'])
  if (parameterHint !== undefined) {
    return resolvePrecisionBytesFromHint(parameterHint, DEFAULT_MODEL_PRECISION_BYTES)
  }

  const modelHint = model?.quantization || model?.format
  return resolvePrecisionBytesFromHint(modelHint, DEFAULT_MODEL_PRECISION_BYTES)
}

/**
 * @param {{parameterValues?: Record<string, unknown>, model?: Record<string, unknown>}} input
 * @returns {number}
 */
export const resolveKvPrecisionBytes = ({ parameterValues = {} } = {}) => {
  const kvSpecificHint = pickFirstParameterValue(parameterValues, ['kv_cache_dtype', 'kv-cache-dtype', 'cache_dtype', 'cache-dtype'])
  if (kvSpecificHint !== undefined) {
    return resolvePrecisionBytesFromHint(kvSpecificHint, DEFAULT_KV_PRECISION_BYTES)
  }

  const dtypeHint = pickFirstParameterValue(parameterValues, ['dtype'])
  if (dtypeHint !== undefined) {
    return resolvePrecisionBytesFromHint(dtypeHint, DEFAULT_KV_PRECISION_BYTES)
  }

  return DEFAULT_KV_PRECISION_BYTES
}

const parseScaledNumber = (value, unitMap = {}) => {
  if (value === undefined || value === null || value === '') return 0
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0

  const text = String(value).trim().toLowerCase().replace(/,/g, '')
  if (!text) return 0

  const match = text.match(/^(\d+(?:\.\d+)?)(?:\s*([a-z]+))?$/i)
  if (!match) {
    const numeric = Number(text)
    return Number.isFinite(numeric) ? numeric : 0
  }

  const numeric = Number(match[1])
  const unit = match[2] || ''
  return numeric * (unitMap[unit] ?? 1)
}

/**
 * @param {unknown} devices
 * @returns {Array<{index:number,memoryBytes:number,uuid:string,deviceKey:string,label:string,[key:string]:unknown}>}
 */
export const normalizeGpuDevices = (devices) => {
  if (!Array.isArray(devices)) return []
  return devices
    .map((device, index) => {
      const resolvedIndex = Number.isFinite(Number(device?.index)) ? Number(device.index) : index
      const memoryBytes = Number(device?.memoryBytes || 0)
      const name = device?.model || device?.name || device?.vendor || `GPU ${resolvedIndex}`
      return {
        ...device,
        index: resolvedIndex,
        memoryBytes,
        uuid: device?.uuid || device?.deviceUUID || '',
        deviceKey: String(device?.deviceKey || device?.uuid || device?.id || `${resolvedIndex}-${name}`),
        label: `#${resolvedIndex} · ${name}${memoryBytes > 0 ? ` · ${formatEstimateGigabytes(bytesToGigabytes(memoryBytes))}` : ''}`
      }
    })
    .filter((device) => device.memoryBytes > 0 || device.uuid || Number.isFinite(device.index))
}

const bytesToGigabytes = (value) => value / BYTE_IN_GIB

const formatEstimateGigabytes = (value) => {
  if (!Number.isFinite(value)) return '-'
  return `${value.toFixed(value >= 10 ? 0 : 1)} GB`
}

const formatParameterCountLabel = (value) => {
  if (!Number.isFinite(value) || value <= 0) return '-'
  if (value >= 1e12) return `${(value / 1e12).toFixed(value >= 10e12 ? 0 : 1)}T`
  if (value >= 1e9) return `${(value / 1e9).toFixed(value >= 10e9 ? 0 : 1)}B`
  if (value >= 1e6) return `${(value / 1e6).toFixed(value >= 10e6 ? 0 : 1)}M`
  return String(Math.round(value))
}

const formatContextLengthLabel = (value) => {
  if (!Number.isFinite(value) || value <= 0) return '-'
  if (value % 1024 === 0 && value >= 1024) return `${Math.round(value / 1024)}K`
  return String(Math.round(value))
}

const resolveEstimateContextLength = (parameterValues = {}, fallback) => {
  const value = pickFirstParameterValue(parameterValues, [
    'max_model_len',
    'max-model-len',
    'context_length',
    'context-length',
    'context_len',
    'max_context_length',
    'max_seq_len',
    'max-seq-len'
  ]) ?? fallback
  return Math.max(0, Math.round(parseScaledNumber(value, { k: 1024, m: 1024 ** 2 })))
}

const resolveEstimateMaxSequences = (parameterValues = {}) => {
  const value = pickFirstParameterValue(parameterValues, [
    'max_num_seqs',
    'max-num-seqs',
    'max_num_sequences',
    'num_seqs',
    'batch_size',
    'max_batch_size',
    'max-batch-size'
  ])
  const numeric = Math.round(parseScaledNumber(value))
  return Math.max(numeric || 1, 1)
}

const resolveEstimateGpuUtilization = (parameterValues = {}) => {
  const value = pickFirstParameterValue(parameterValues, [
    'gpu_memory_utilization',
    'gpu-memory-utilization',
    'gpuMemoryUtilization'
  ])
  const numeric = parseScaledNumber(value)
  if (!numeric) return 1
  if (numeric > 1) {
    return Math.min(Math.max(numeric / 100, 0.1), 1)
  }
  return Math.min(Math.max(numeric, 0.1), 1)
}

const resolveEstimateCpuOffloadGb = (parameterValues = {}) => {
  const value = pickFirstParameterValue(parameterValues, [
    'cpu_offload_gb',
    'cpu-offload-gb',
    'cpuOffloadGb',
    'cpu_offload',
    'cpu-offload',
    'cpuOffload'
  ])
  return Math.max(parseScaledNumber(value, { g: 1, gb: 1 }), 0)
}

const resolveKvBytesPerToken = (parameterCount, architecture, kvPrecisionBytes) => {
  const parameterBillions = parameterCount / 1e9
  const architectureHint = String(architecture || '').toLowerCase()
  let factorMbPerTokenPerBillion = 0.012

  if (/(qwen|mistral|mixtral|gemma|deepseek|phi|internlm|yi)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.008
  } else if (/(llama3|llama-3|llama 3)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.016
  } else if (/(llama|baichuan|falcon|bloom)/.test(architectureHint)) {
    factorMbPerTokenPerBillion = 0.03
  }

  return parameterBillions * factorMbPerTokenPerBillion * 1024 * 1024 * (kvPrecisionBytes / 2)
}

const resolveRuntimeReserveBytes = (weightsBytes, kvCacheBytes, gpuCount) => {
  const baselineBytes = weightsBytes + kvCacheBytes
  const reserveByLoad = baselineBytes * 0.08
  const reserveByGpuCount = gpuCount * 1.5 * BYTE_IN_GIB
  return Math.max(reserveByLoad, reserveByGpuCount, DEFAULT_RUNTIME_RESERVE_BYTES)
}

const resolveTensorParallelSize = (parameterValues = {}) => {
  const value = pickFirstParameterValue(parameterValues, ['tensor_parallel_size', 'tensor-parallel-size', 'tensorParallelSize'])
  const numeric = Math.round(parseScaledNumber(value))
  return Math.max(numeric || 1, 1)
}

const resolveGpuSelection = (gpuDevices, selectedGpuDeviceKeys, tensorParallelSize = 1) => {
  const normalizedGpuDevices = normalizeGpuDevices(gpuDevices)
  const selectedKeys = Array.isArray(selectedGpuDeviceKeys) ? new Set(selectedGpuDeviceKeys.map(String)) : new Set()
  const selectedGpuDevices = selectedKeys.size
    ? normalizedGpuDevices.filter((device) => selectedKeys.has(String(device.deviceKey)))
    : []

  const runtimeGpuDevices = selectedGpuDevices.length > 0 ? selectedGpuDevices : normalizedGpuDevices
  const selectedGpuMemoryBytes = runtimeGpuDevices.reduce((total, device) => total + device.memoryBytes, 0)
  const fallbackGpuCount = runtimeGpuDevices.length || normalizedGpuDevices.length || tensorParallelSize || 0
  const runtimeGpuCount = Math.max(fallbackGpuCount, 1)

  return {
    normalizedGpuDevices,
    selectedGpuDevices,
    runtimeGpuDevices,
    selectedGpuMemoryBytes,
    runtimeGpuCount
  }
}

/**
 * @param {object} input
 * @param {Record<string, unknown>} [input.model]
 * @param {Record<string, unknown>} [input.parameterValues]
 * @param {Array<unknown>} [input.gpuDevices]
 * @param {Array<string>} [input.selectedGpuDeviceKeys]
 * @returns {object}
 */
export const buildDeployVramEstimate = ({
  model = {},
  parameterValues = {},
  gpuDevices = [],
  selectedGpuDeviceKeys = []
} = {}) => {
  const parameterCount = parseModelParameterCount(
    model?.parameterSize
    || model?.metadata?.parameter_size
    || model?.metadata?.model_size
    || model?.metadata?.ModelInfos?.safetensor?.model_size
    || model?.metadata?.modelInfos?.safetensor?.model_size
    || model?.metadata?.model_infos?.safetensor?.model_size
    || model?.metadata?.cardData?.model_size
  )

  const modelPrecision = resolveModelPrecisionBytes({ parameterValues, model })
  const kvPrecision = resolveKvPrecisionBytes({ parameterValues, model })
  const contextLength = resolveEstimateContextLength(parameterValues, model?.contextLength)
  const maxSequences = resolveEstimateMaxSequences(parameterValues)
  const gpuUtilization = resolveEstimateGpuUtilization(parameterValues)
  const cpuOffloadGb = resolveEstimateCpuOffloadGb(parameterValues)
  const tensorParallelSize = resolveTensorParallelSize(parameterValues)
  const cpuOffloadBytes = Math.min(cpuOffloadGb * BYTE_IN_GIB, parameterCount > 0 ? parameterCount * modelPrecision : 0)

  const {
    selectedGpuMemoryBytes,
    runtimeGpuCount,
    selectedGpuDevices,
    runtimeGpuDevices,
    normalizedGpuDevices
  } = resolveGpuSelection(gpuDevices, selectedGpuDeviceKeys, tensorParallelSize)

  const weightsBytes = parameterCount > 0 ? parameterCount * modelPrecision : 0
  const kvCacheBytes = parameterCount > 0 && contextLength > 0
    ? resolveKvBytesPerToken(parameterCount, model?.architecture, kvPrecision) * contextLength * maxSequences
    : 0
  const runtimeReserveBytes = parameterCount > 0
    ? resolveRuntimeReserveBytes(weightsBytes, kvCacheBytes, runtimeGpuCount)
    : 0
  const totalBytes = Math.max(weightsBytes - cpuOffloadBytes, 0) + kvCacheBytes + runtimeReserveBytes
  const usageRatio = selectedGpuMemoryBytes > 0 ? totalBytes / selectedGpuMemoryBytes : null
  const usageCapBytes = selectedGpuMemoryBytes > 0 ? selectedGpuMemoryBytes * gpuUtilization : 0
  const hasGpuCapacity = selectedGpuMemoryBytes > 0
  const sufficientGpuSelection = selectedGpuDevices.length > 0 || normalizedGpuDevices.length > 0 || runtimeGpuDevices.length > 0

  let status = 'missing-data'
  let message = '缺少模型或 GPU 资源数据，无法估算显存。'

  if (parameterCount > 0 && hasGpuCapacity) {
    if (usageRatio > gpuUtilization) {
      status = 'insufficient'
      message = `预计显存 ${formatEstimateGigabytes(bytesToGigabytes(totalBytes))} 超过可用阈值 ${formatEstimateGigabytes(bytesToGigabytes(usageCapBytes))}。`
    } else if (usageRatio > 0.8) {
      status = 'warning'
      message = `预计显存 ${formatEstimateGigabytes(bytesToGigabytes(totalBytes))} 接近阈值 ${formatEstimateGigabytes(bytesToGigabytes(usageCapBytes))}。`
    } else {
      status = 'sufficient'
      message = `预计显存 ${formatEstimateGigabytes(bytesToGigabytes(totalBytes))} 低于阈值 ${formatEstimateGigabytes(bytesToGigabytes(usageCapBytes))}。`
    }
  } else if (parameterCount > 0 && !sufficientGpuSelection) {
    status = 'missing-data'
    message = '缺少可用 GPU 数据，无法估算显存。'
  }

  return {
    status,
    message,
    parameterCount,
    contextLength,
    maxSequences,
    weightsBytes,
    kvCacheBytes,
    runtimeReserveBytes,
    cpuOffloadBytes,
    totalBytes,
    selectedGpuMemoryBytes,
    gpuUtilization,
    usageRatio,
    runtimeGpuCount,
    tensorParallelSize,
    selectedGpuDevices,
    runtimeGpuDevices,
    normalizedGpuDevices
  }
}

const formatRatioText = (value, { signed = false, invertSign = false } = {}) => {
  const numeric = Number(value || 0)
  if (!Number.isFinite(numeric) || Math.abs(numeric) < 0.0001) return signed ? '0%' : '-'
  const resolved = invertSign ? -numeric : numeric
  const prefix = signed && resolved > 0 ? '+' : ''
  return `${prefix}${resolved.toFixed(1)}%`
}

const buildBreakdownItem = (label, bytes, totalBytes, signed = false) => ({
  label,
  bytes,
  gb: bytesToGigabytes(bytes),
  ratioText: formatRatioText(totalBytes > 0 ? (bytesToGigabytes(bytes) / bytesToGigabytes(totalBytes)) * 100 : 0, {
    signed,
    invertSign: signed
  })
})

/**
 * @param {object} input
 * @param {string} [input.resourceState]
 * @param {string} [input.resourceError]
 * @param {object} [input.estimate]
 * @returns {object}
 */
export const buildDeployVramEstimateViewModel = ({
  resourceState,
  resourceError,
  estimate
} = {}) => {
  if (!estimate) {
    return {
      displayStatus: resourceState === 'loading' ? 'collecting' : resourceState === 'error' ? 'failed' : resourceState === 'unsupported' ? 'missing-data' : 'idle',
      message: resourceState === 'loading'
        ? '正在采集 GPU 资源信息，请稍候。'
        : resourceState === 'error'
          ? (resourceError || 'GPU 资源采集失败，请重试。')
          : resourceState === 'unsupported'
            ? '当前资源不支持 GPU 估算。'
            : '请先选择目标资源以开始显存估算。',
      showRetry: resourceState === 'error',
      summary: [],
      breakdown: [],
      composition: [],
      selection: []
    }
  }

  const displayStatus = resourceState === 'loading'
    ? 'collecting'
    : resourceState === 'error'
      ? 'failed'
      : resourceState === 'unsupported'
        ? 'missing-data'
        : resourceState === 'idle'
          ? 'idle'
          : estimate.status === 'insufficient'
    ? 'insufficient'
    : estimate.status === 'warning'
      ? 'warning'
      : estimate.status === 'sufficient'
        ? 'sufficient'
        : 'missing-data'

  const totalDemandGb = bytesToGigabytes(estimate.totalBytes || 0)
  const selectedGpuCapacityGb = bytesToGigabytes(estimate.selectedGpuMemoryBytes || 0)
  const usageCapBytes = (estimate.selectedGpuMemoryBytes || 0) * (estimate.gpuUtilization || 0)
  const usageRatio = estimate.usageRatio
  const summary = [
    {
      label: '总显存需求',
      value: totalDemandGb > 0 ? formatEstimateGigabytes(totalDemandGb) : '-'
    },
    {
      label: 'GPU 容量',
      value: selectedGpuCapacityGb > 0 ? formatEstimateGigabytes(selectedGpuCapacityGb) : '无可用容量'
    },
    {
      label: '阈值',
      value: usageCapBytes > 0 ? `${formatEstimateGigabytes(bytesToGigabytes(usageCapBytes))} (${formatRatioText((estimate.gpuUtilization || 0) * 100)})` : '-'
    },
    {
      label: '占比',
      value: usageRatio !== null && usageRatio !== undefined ? formatRatioText(usageRatio * 100) : '-'
    }
  ]

  const breakdown = [
    buildBreakdownItem('Weights', estimate.weightsBytes || 0, estimate.totalBytes || 0),
    buildBreakdownItem('KV Cache', estimate.kvCacheBytes || 0, estimate.totalBytes || 0),
    buildBreakdownItem('Runtime Reserve', estimate.runtimeReserveBytes || 0, estimate.totalBytes || 0),
    buildBreakdownItem('CPU Offload', estimate.cpuOffloadBytes || 0, estimate.totalBytes || 0, true)
  ]

  const composition = [
    {
      label: '模型权重',
      value: estimate.weightsBytes > 0 ? formatEstimateGigabytes(bytesToGigabytes(estimate.weightsBytes)) : '-',
      hint: formatRatioText(totalDemandGb > 0 ? (estimate.weightsBytes / (estimate.totalBytes || 1)) * 100 : 0)
    },
    {
      label: 'KV Cache',
      value: estimate.kvCacheBytes > 0 ? formatEstimateGigabytes(bytesToGigabytes(estimate.kvCacheBytes)) : '-',
      hint: formatRatioText(totalDemandGb > 0 ? (estimate.kvCacheBytes / (estimate.totalBytes || 1)) * 100 : 0)
    },
    {
      label: '运行时开销',
      value: estimate.runtimeReserveBytes > 0 ? formatEstimateGigabytes(bytesToGigabytes(estimate.runtimeReserveBytes)) : '-',
      hint: formatRatioText(totalDemandGb > 0 ? (estimate.runtimeReserveBytes / (estimate.totalBytes || 1)) * 100 : 0)
    },
    {
      label: 'CPU 卸载',
      value: estimate.cpuOffloadBytes > 0 ? formatEstimateGigabytes(bytesToGigabytes(estimate.cpuOffloadBytes)) : '-',
      hint: formatRatioText(totalDemandGb > 0 ? (estimate.cpuOffloadBytes / (estimate.totalBytes || 1)) * 100 : 0, { signed: true, invertSign: true })
    }
  ]

  const selection = [
    {
      label: '参数量',
      value: formatParameterCountLabel(estimate.parameterCount)
    },
    {
      label: '上下文',
      value: formatContextLengthLabel(estimate.contextLength)
    },
    {
      label: '并发 / 批次',
      value: estimate.maxSequences > 0 ? String(estimate.maxSequences) : '-'
    },
    {
      label: '并行 GPU',
      value: estimate.runtimeGpuCount > 0 ? `${estimate.runtimeGpuCount} 卡（TP=${estimate.tensorParallelSize || estimate.runtimeGpuCount}）` : '-'
    }
  ]

  let message = estimate.message || ''
  let showRetry = false
  if (resourceState === 'idle') {
    message = '先选择目标资源以继续核对 GPU 容量；模型侧显存需求已先行估算。'
  } else if (resourceState === 'loading') {
    message = '模型侧显存需求已先行估算，正在采集 GPU 资源信息。'
  } else if (resourceState === 'error') {
    message = `${resourceError || 'GPU 资源采集失败，请重试。'}；已先展示模型侧显存需求。`
    showRetry = true
  } else if (resourceState === 'unsupported') {
    message = '当前资源不支持 GPU 容量核对，已先展示模型侧显存需求。'
  }

  return {
    displayStatus,
    message,
    showRetry,
    summary,
    breakdown,
    composition,
    selection
  }
}
