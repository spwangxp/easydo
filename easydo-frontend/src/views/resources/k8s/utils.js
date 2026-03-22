export const DEFAULT_K8S_KINDS = ['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Service', 'Ingress', 'CronJob', 'Job', 'ConfigMap', 'Secret']

export const K8S_KIND_OPTIONS = DEFAULT_K8S_KINDS.map(kind => ({
  label: kind,
  value: kind
}))

export const K8S_NAMESPACE_PARAMETER_KEYS = ['namespace', 'k8s_namespace', 'target_namespace']

const ACTION_LABEL_MAP = {
  rollout_restart: '滚动重启',
  scale: '调整副本',
  suspend: '暂停任务',
  resume: '恢复任务'
}

const ACTION_TYPE_MAP = {
  rollout_restart: 'warning',
  scale: 'primary',
  suspend: 'warning',
  resume: 'success'
}

const KIND_TAG_TYPE_MAP = {
  Deployment: 'primary',
  StatefulSet: 'success',
  DaemonSet: 'warning',
  Pod: 'info',
  Service: 'success',
  Ingress: 'warning',
  CronJob: 'warning',
  Job: 'primary',
  ConfigMap: 'info',
  Secret: 'danger'
}

export function buildResourceK8sRouteLocation(resourceId, namespace = '') {
  const location = {
    name: 'ResourceK8sBrowser',
    params: { id: String(resourceId) }
  }

  if (namespace) {
    location.query = { namespace }
  }

  return location
}

export function parseJSONSafely(value, fallback = {}) {
  if (value === undefined || value === null || value === '') return fallback
  if (typeof value === 'object') return value
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch {
      return fallback
    }
  }
  return fallback
}

export function formatDateTime(value) {
  if (!value) return '-'
  const timestamp = typeof value === 'number' ? value * 1000 : value
  return new Date(timestamp).toLocaleString('zh-CN')
}

export function formatRelativeAge(value) {
  if (!value) return '-'
  const targetTime = new Date(value).getTime()
  if (Number.isNaN(targetTime)) return '-'
  const diffSeconds = Math.max(0, Math.floor((Date.now() - targetTime) / 1000))
  if (diffSeconds < 60) return `${diffSeconds}s`
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m`
  if (diffSeconds < 86400) return `${Math.floor(diffSeconds / 3600)}h`
  return `${Math.floor(diffSeconds / 86400)}d`
}

export function formatBytes(value) {
  const size = Number(value || 0)
  if (!size) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = size
  let unitIndex = 0
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024
    unitIndex += 1
  }
  return `${current >= 10 || unitIndex === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[unitIndex]}`
}

export function formatCPUMilli(value) {
  const milli = Number(value || 0)
  if (!milli) return '-'
  const cores = milli / 1000
  return `${Number.isInteger(cores) ? cores : cores.toFixed(1)} CPU`
}

export function getEnvironmentLabel(environment) {
  return ({ development: '开发环境', testing: '测试环境', production: '生产环境' }[environment] || environment || '-')
}

export function getClusterStatusType(status) {
  return ({ online: 'success', offline: 'info', error: 'danger', archived: 'warning' }[status] || 'info')
}

export function getAuditStatusType(status) {
  return ({ queued: 'info', running: 'warning', success: 'success', failed: 'danger', cancelled: 'info' }[status] || 'info')
}

export function getAuditStatusLabel(status) {
  return ({ queued: '排队中', running: '执行中', success: '成功', failed: '失败', cancelled: '已取消' }[status] || status || '-')
}

export function normalizeK8sOverview(data = {}) {
  const baseInfo = parseJSONSafely(data.base_info || data.baseInfo, {})
  const summary = baseInfo?.k8s?.summary || {}
  const cluster = baseInfo?.k8s?.cluster || {}

  return {
    resourceId: Number(data.resource_id || data.resourceId || 0),
    name: data.name || '',
    endpoint: data.endpoint || '',
    environment: data.environment || '',
    status: data.status || '',
    baseInfoStatus: data.base_info_status || data.baseInfoStatus || '',
    baseInfoSource: data.base_info_source || data.baseInfoSource || '',
    baseInfoLastError: data.base_info_last_error || data.baseInfoLastError || '',
    baseInfoCollectedAt: data.base_info_collected_at || data.baseInfoCollectedAt || 0,
    lastCheckAt: data.last_check_at || data.lastCheckAt || 0,
    lastCheckResult: data.last_check_result || data.lastCheckResult || '',
    clusterVersion: cluster.serverVersion || '-',
    nodeCount: Number(summary.nodeCount || 0),
    cpuAllocatableMilli: Number(summary.cpuAllocatableMilli || 0),
    memoryAllocatableBytes: Number(summary.memoryAllocatableBytes || 0),
    gpuAllocatable: Number(summary.gpuAllocatable || 0),
    baseInfo
  }
}

export function parseTaskResultData(resultData) {
  return parseJSONSafely(resultData, {})
}

export function extractTaskStdout(task = {}) {
  const payload = parseTaskResultData(task.result_data || task.resultData)
  return typeof payload.stdout === 'string' ? payload.stdout : ''
}

export function extractTaskErrorMessage(task = {}) {
  const payload = parseTaskResultData(task.result_data || task.resultData)
  const message = [
    task.error_msg,
    task.errorMsg,
    payload.stderr,
    payload.stdout
  ].find(value => String(value || '').trim())
  return message ? String(message).trim() : '任务执行失败'
}

export function parseKubectlListOutput(stdout) {
  const raw = String(stdout || '').trim()
  const lines = raw.split(/\r?\n/)
  const candidates = [raw]

  lines.forEach((_, index) => {
    const candidate = lines.slice(index).join('\n').trim()
    if (candidate.startsWith('{') || candidate.startsWith('[')) {
      candidates.push(candidate)
    }
  })

  for (const candidate of candidates) {
    const payload = parseJSONSafely(candidate, null)
    if (Array.isArray(payload)) return payload
    if (Array.isArray(payload?.items)) return payload.items
    if (payload?.kind && payload?.metadata) return [payload]
  }

  return []
}

export function normalizeNamespaceItem(item = {}) {
  const metadata = item.metadata || {}
  return {
    uid: metadata.uid || metadata.name || `namespace-${Math.random()}`,
    name: metadata.name || '-',
    phase: item.status?.phase || 'Active',
    createdAt: metadata.creationTimestamp || '',
    raw: item
  }
}

export function getActionLabel(action) {
  return ACTION_LABEL_MAP[action] || action || '-'
}

export function getActionType(action) {
  return ACTION_TYPE_MAP[action] || 'info'
}

export function getKindTagType(kind) {
  return KIND_TAG_TYPE_MAP[kind] || 'info'
}

export function resolveDefaultReplicas(resource = {}) {
  const replicas = Number(resource?.raw?.spec?.replicas ?? resource?.spec?.replicas ?? resource?.raw?.status?.replicas ?? resource?.status?.replicas ?? 1)
  return Number.isFinite(replicas) && replicas >= 0 ? replicas : 1
}

export function getK8sActionOptions(resource = {}) {
  const kind = resource.kind || resource?.raw?.kind || ''
  const currentReplicas = resolveDefaultReplicas(resource)
  const currentSuspend = Boolean(resource?.raw?.spec?.suspend)

  if (['Deployment', 'StatefulSet', 'DaemonSet'].includes(kind)) {
    return [{ value: 'rollout_restart', label: getActionLabel('rollout_restart'), type: getActionType('rollout_restart') }]
      .concat(['Deployment', 'StatefulSet'].includes(kind)
        ? [{ value: 'scale', label: `${getActionLabel('scale')}（当前 ${currentReplicas}）`, type: getActionType('scale'), needsReplicas: true }]
        : [])
  }

  if (kind === 'CronJob') {
    return [{
      value: currentSuspend ? 'resume' : 'suspend',
      label: getActionLabel(currentSuspend ? 'resume' : 'suspend'),
      type: getActionType(currentSuspend ? 'resume' : 'suspend')
    }]
  }

  return []
}

function resolveK8sResourceStatus(item = {}) {
  const kind = item.kind || ''
  const status = item.status || {}
  const spec = item.spec || {}

  if (kind === 'Deployment' || kind === 'StatefulSet') {
    const ready = Number(status.readyReplicas || 0)
    const replicas = Number(spec.replicas || 0)
    return ready >= replicas && replicas > 0 ? '已就绪' : `${ready}/${replicas} Ready`
  }

  if (kind === 'DaemonSet') {
    const ready = Number(status.numberReady || 0)
    const desired = Number(status.desiredNumberScheduled || 0)
    return ready >= desired && desired > 0 ? '已就绪' : `${ready}/${desired} Ready`
  }

  if (kind === 'Pod') {
    return status.phase || 'Unknown'
  }

  if (kind === 'Job') {
    const completions = Number(spec.completions || 1)
    const succeeded = Number(status.succeeded || 0)
    return succeeded >= completions ? '已完成' : `${succeeded}/${completions} Succeeded`
  }

  if (kind === 'CronJob') {
    return spec.suspend ? '已暂停' : '运行中'
  }

  if (kind === 'Service') {
    return spec.type || 'ClusterIP'
  }

  if (kind === 'Ingress') {
    return status.loadBalancer?.ingress?.[0]?.ip || status.loadBalancer?.ingress?.[0]?.hostname || '待分配入口'
  }

  return kind
}

function resolveK8sResourceSummary(item = {}) {
  const kind = item.kind || ''
  const metadata = item.metadata || {}
  const spec = item.spec || {}
  const status = item.status || {}

  if (kind === 'Deployment' || kind === 'StatefulSet') {
    const updated = Number(status.updatedReplicas || 0)
    const available = Number(status.availableReplicas || 0)
    return `${updated} 更新 / ${available} 可用`
  }

  if (kind === 'DaemonSet') {
    return `${Number(status.currentNumberScheduled || 0)} 已调度 / ${Number(status.numberAvailable || 0)} 可用`
  }

  if (kind === 'Pod') {
    const nodeName = spec.nodeName || '未调度'
    const podIP = status.podIP || '无 Pod IP'
    return `${nodeName} · ${podIP}`
  }

  if (kind === 'Service') {
    return `${spec.clusterIP || '无 ClusterIP'} · ${Array.isArray(spec.ports) ? spec.ports.map(port => port.port).join(', ') : '-'}`
  }

  if (kind === 'Ingress') {
    const hosts = Array.isArray(spec.rules) ? spec.rules.map(rule => rule.host).filter(Boolean) : []
    return hosts.length > 0 ? hosts.join(', ') : '未配置域名规则'
  }

  if (kind === 'CronJob') {
    return `${spec.schedule || '未配置调度'} · ${spec.suspend ? '已暂停' : '启用中'}`
  }

  if (kind === 'Job') {
    const active = Number(status.active || 0)
    const failed = Number(status.failed || 0)
    return `${active} 运行中 / ${failed} 失败`
  }

  if (kind === 'ConfigMap') {
    return `${Object.keys(item.data || {}).length} 个键`
  }

  if (kind === 'Secret') {
    return `${spec.type || item.type || 'Opaque'} · ${Object.keys(item.data || {}).length} 个键`
  }

  return `${Object.keys(metadata.labels || {}).length} 个标签`
}

export function normalizeK8sResourceItem(item = {}) {
  const metadata = item.metadata || {}
  const kind = item.kind || 'Unknown'
  const resource = {
    uid: metadata.uid || `${kind}:${metadata.namespace || 'cluster'}:${metadata.name || 'unknown'}`,
    kind,
    name: metadata.name || '-',
    namespace: metadata.namespace || '',
    createdAt: metadata.creationTimestamp || '',
    labels: metadata.labels || {},
    statusText: resolveK8sResourceStatus(item),
    summaryText: resolveK8sResourceSummary(item),
    raw: item
  }

  resource.actionOptions = getK8sActionOptions(resource)
  return resource
}

export function sortNamespaces(items = []) {
  return [...items].sort((left, right) => {
    if (left.name === 'default') return -1
    if (right.name === 'default') return 1
    return String(left.name).localeCompare(String(right.name), 'zh-CN')
  })
}

export function sortK8sResources(items = []) {
  return [...items].sort((left, right) => {
    const kindCompare = String(left.kind).localeCompare(String(right.kind), 'en')
    if (kindCompare !== 0) return kindCompare
    return String(left.name).localeCompare(String(right.name), 'zh-CN')
  })
}

export function resolveNamespaceFromParameterSnapshot(snapshot) {
  const payload = parseJSONSafely(snapshot, {})
  return K8S_NAMESPACE_PARAMETER_KEYS.map(key => payload?.[key]).find(value => String(value || '').trim()) || ''
}

export function applyNamespacePreset(parameters, fields, namespace) {
  const resolvedNamespace = String(namespace || '').trim()
  if (!resolvedNamespace) return []

  const fieldKeys = new Set((fields || []).map(field => field.key))
  const matchedKeys = K8S_NAMESPACE_PARAMETER_KEYS.filter(key => fieldKeys.has(key))
  const keysToApply = matchedKeys.length > 0 ? matchedKeys : ['namespace']

  keysToApply.forEach(key => {
    parameters[key] = resolvedNamespace
  })

  return keysToApply
}
