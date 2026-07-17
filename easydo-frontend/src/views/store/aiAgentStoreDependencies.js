export const BUILTIN_EASYDO_MCP_DIGEST = 'sha256:4c827741231322d39bebcccffa8901f2ba0a264fe64f01848611c8fdccd2beeb'
export const TOOL_PERMISSION_DECISIONS = ['allow', 'request', 'deny']

export function createPinnedResourceRefRow(item = {}, fallbackResourceType = 'resource', localId = '') {
  return {
    local_id: localId,
    resource_type: item.resource_type || fallbackResourceType,
    resource_id: item.resource_id ?? '',
    resource_version_id: positiveNumberOrEmpty(item.resource_version_id),
    resource_version: item.resource_version || '',
    snapshot_digest: item.snapshot_digest || '',
    required: item.required !== false,
    configJSON: item.configJSON !== undefined ? String(item.configJSON) : stringifyJSON(item.config || {})
  }
}

export function toolPermissionRowsForResource(resource = {}, config = {}) {
  const permissionConfig = asObject(config.tool_permissions || config)
  const toolMap = asObject(permissionConfig.tools)
  const tools = normalizeToolDefinitions(resource?.spec?.discovered_tools || resource?.spec?.tools || [])
  const rowsByName = new Map()
  tools.forEach((tool) => {
    rowsByName.set(tool.name, {
      tool_name: tool.name,
      description: tool.description || '',
      operation_type: tool.operation_type || '',
      decision: 'request'
    })
  })
  Object.entries(toolMap).forEach(([toolName, raw]) => {
    const record = typeof raw === 'string' ? { decision: raw } : asObject(raw)
    const existing = rowsByName.get(toolName) || {
      tool_name: toolName,
      description: '',
      operation_type: ''
    }
    rowsByName.set(toolName, {
      ...existing,
      operation_type: String(record.operation_type || record.operationType || existing.operation_type || '').trim(),
      decision: normalizeToolPermissionDecision(record.decision || record.policy || record.mode)
    })
  })
  return [...rowsByName.values()].sort((left, right) => left.tool_name.localeCompare(right.tool_name))
}

export function toolPermissionConfigFromRows(rows = [], advanced = {}) {
  const tools = {}
  for (const row of Array.isArray(rows) ? rows : []) {
    const toolName = String(row?.tool_name || row?.name || '').trim()
    if (!toolName) continue
    tools[toolName] = {
      decision: normalizeToolPermissionDecision(row.decision),
      operation_type: String(row.operation_type || '').trim()
    }
    if (!tools[toolName].operation_type) delete tools[toolName].operation_type
  }
  const output = { tool_permissions: { tools } }
  const defaultDecision = normalizeToolPermissionDecision(advanced.default_decision || advanced.defaultDecision)
  if (defaultDecision) output.tool_permissions.default_decision = defaultDecision
  const capabilities = asObject(advanced.capabilities)
  if (Object.keys(capabilities).length > 0) output.tool_permissions.capabilities = capabilities
  const rules = Array.isArray(advanced.rules) ? advanced.rules.filter((item) => item && typeof item === 'object') : []
  if (rules.length > 0) output.tool_permissions.rules = rules
  return output
}

export function mergeResourceToolPermissionsIntoRefRow(row, resource = {}) {
  if (!row || typeof row !== 'object') return row
  const config = parseObject(row.configJSON, {})
  if (Object.keys(asObject(config.tool_permissions)).length > 0) return row
  const permissions = asObject(resource?.spec?.tool_permissions)
  if (Object.keys(permissions).length === 0) return row
  row.configJSON = stringifyJSON({
    ...config,
    tool_permissions: cloneJSON(permissions)
  })
  return row
}

export function isPinnedResourceRef(ref = {}) {
  const digest = String(ref.snapshot_digest || '').trim()
  const version = String(ref.resource_version || '').trim()
  if (!/^sha256:[a-f0-9]{64}$/.test(digest) || !version) return false
  if (isBuiltinEasyDoMcpRef(ref)) return version === 'builtin-v1' && digest === BUILTIN_EASYDO_MCP_DIGEST
  return Number.isInteger(Number(ref.resource_version_id)) && Number(ref.resource_version_id) > 0
}

export function buildPinnedResourceRefs(rows, options = {}) {
  const label = options.label || '资源'
  const fallbackResourceType = options.fallbackResourceType || 'resource'
  const parseConfig = typeof options.parseConfig === 'function' ? options.parseConfig : JSON.parse
  return (Array.isArray(rows) ? rows : [])
    .filter((row) => hasValue(row?.resource_id))
    .map((row, index) => {
      const ref = {
        resource_type: String(row.resource_type || fallbackResourceType).trim(),
        resource_id: normalizeResourceID(row.resource_id),
        resource_version_id: positiveNumberOrUndefined(row.resource_version_id),
        resource_version: String(row.resource_version || '').trim(),
        snapshot_digest: String(row.snapshot_digest || '').trim(),
        config: parseConfig(row.configJSON || '{}'),
        required: row.required !== false
      }

      if (isBuiltinEasyDoMcpRef(ref)) {
        ref.resource_version = 'builtin-v1'
        ref.snapshot_digest = BUILTIN_EASYDO_MCP_DIGEST
      }
      if (!isPinnedResourceRef(ref)) {
        throw new Error(`${label} 第 ${index + 1} 行必须选择固定版本`)
      }
      if (!ref.resource_version_id) delete ref.resource_version_id
      return ref
    })
}

export function applyVersionPinToRow(row, version = {}) {
  if (!row || typeof row !== 'object') return row
  row.resource_version_id = positiveNumberOrEmpty(version.resource_version_id ?? version.profile_version_id ?? version.id)
  row.resource_version = String(version.source_version ?? version.resource_version ?? version.version ?? '').trim()
  row.snapshot_digest = String(version.snapshot_digest ?? version.snapshot_hash ?? '').trim()
  return row
}

export function statusTransitionOperation(previousStatus, nextStatus) {
  const previous = String(previousStatus || '').trim()
  const next = String(nextStatus || '').trim()
  if (!next || previous === next) return ''
  if (next === 'disabled') return 'disable'
  if (next === 'archived') return 'archive'
  return ''
}

export function profileResourceHealthIssues(profile = {}, context = {}) {
  const issues = []
  const sections = [
    { field: 'skills', group: 'skills', label: 'Skill', targetKind: 'resource' },
    { field: 'mcp_servers', group: 'mcp_servers', label: 'MCP Server', targetKind: 'resource' },
    { field: 'subagents', group: 'profiles', label: 'Subagent', targetKind: 'profile' }
  ]

  for (const section of sections) {
    const refs = Array.isArray(profile?.[section.field]) ? profile[section.field] : []
    refs.forEach((ref, index) => {
      if (!hasValue(ref?.resource_id)) return
      const target = resolveHealthTarget(ref, section, context)
      const label = `${section.label} ${target?.name || ref.resource_id}`
      const base = {
        section: section.field,
        index,
        label,
        resource_id: ref.resource_id,
        resource_version_id: ref.resource_version_id,
        resource_version: ref.resource_version,
        snapshot_digest: ref.snapshot_digest
      }

      if (!target && !isBuiltinEasyDoMcpRef(ref)) {
        issues.push({ ...base, severity: 'invalid', message: `${label} 配置失效：引用目标不存在` })
        return
      }
      if (target && section.targetKind === 'resource' && String(target.status || 'active') !== 'active') {
        issues.push({ ...base, severity: 'invalid', message: `${label} 配置失效：资源状态不是 active` })
        return
      }
      if (target && section.targetKind === 'profile' && ['disabled', 'archived'].includes(String(target.status || ''))) {
        issues.push({ ...base, severity: 'invalid', message: `${label} 配置失效：Subagent Profile 已不可用` })
        return
      }
      if (!isPinnedResourceRef(ref)) {
        issues.push({ ...base, severity: 'invalid', message: `${label} 缺少固定版本` })
        return
      }

      const versionIssue = healthIssueFromVersionCache(ref, section, target, context)
      if (versionIssue) {
        issues.push({ ...base, ...versionIssue })
      }
    })
  }

  const validationIssues = validationHealthIssues(profile, context.validationResult)
  return [...issues, ...validationIssues]
}

export function profileDependencyHealth(profile = {}, context = {}) {
  const issues = Array.isArray(context.issues) ? context.issues : profileResourceHealthIssues(profile, context)
  if (issues.some((issue) => issue.severity === 'invalid')) {
    return { label: '配置失效', type: 'danger' }
  }
  if (issues.some((issue) => issue.severity === 'update_available')) {
    return { label: '可更新', type: 'warning' }
  }
  const count = dependencyRefCount(profile)
  return count > 0
    ? { label: `${count} 项依赖`, type: 'success' }
    : { label: '未配置依赖', type: 'info' }
}

function isBuiltinEasyDoMcpRef(ref = {}) {
  return String(ref.resource_type || '').trim() === 'mcp_server' &&
    String(ref.resource_id || '').trim().toLowerCase() === 'easydo'
}

function resolveHealthTarget(ref, section, context) {
  if (section.targetKind === 'profile') {
    return findByReference(context.profiles || [], ref.resource_id)
  }
  const groups = context.resources || {}
  return findByReference(groups[section.group] || [], ref.resource_id)
}

function healthIssueFromVersionCache(ref, section, target, context) {
  if (isBuiltinEasyDoMcpRef(ref)) return null
  const cacheKey = versionCacheKeyForHealth(ref, section, target)
  const cacheEntry = context.versionCache?.[cacheKey]
  const versions = Array.isArray(cacheEntry?.versions)
    ? cacheEntry.versions
    : []
  if (cacheEntry?.error) {
    return { severity: 'invalid', message: `配置失效：版本校验失败（${cacheEntry.error}）` }
  }
  if (versions.length === 0) {
    return cacheEntry?.loaded === true
      ? { severity: 'invalid', message: '配置失效：版本不可用' }
      : null
  }

  const selectedID = Number(ref.resource_version_id || 0)
  const selected = versions.find((version) => Number(versionID(version, section)) === selectedID)
  if (!selected) {
    return { severity: 'invalid', message: '配置失效：版本不可用' }
  }
  const expectedDigest = String(ref.snapshot_digest || '').trim()
  const selectedDigest = String(versionDigest(selected) || '').trim()
  if (expectedDigest && selectedDigest && expectedDigest !== selectedDigest) {
    return { severity: 'invalid', message: '配置失效：固定版本 digest 不一致' }
  }
  const expectedVersion = String(ref.resource_version || '').trim()
  const selectedVersion = String(versionLabel(selected, section) || '').trim()
  if (expectedVersion && selectedVersion && expectedVersion !== selectedVersion) {
    return { severity: 'invalid', message: '配置失效：固定版本号不一致' }
  }
  if (section.targetKind === 'profile' && String(selected.status || 'published') !== 'published') {
    return { severity: 'invalid', message: '配置失效：Subagent 固定版本不是 published' }
  }
  const latest = latestVersion(versions, section)
  if (latest && Number(versionID(latest, section)) !== selectedID) {
    return { severity: 'update_available', message: `可更新：存在新版本 ${versionLabel(latest, section) || versionID(latest, section)}` }
  }
  return null
}

function versionCacheKeyForHealth(ref, section, target) {
  const id = section.targetKind === 'profile' ? ref.resource_id : (target?.id || ref.resource_id)
  return `${section.field}:${id}`
}

function versionID(version, section) {
  return section.targetKind === 'profile'
    ? (version.profile_version_id ?? version.id)
    : (version.resource_version_id ?? version.id)
}

function versionLabel(version, section) {
  return section.targetKind === 'profile'
    ? (version.version ?? version.resource_version)
    : (version.source_version ?? version.resource_version ?? version.version)
}

function versionDigest(version) {
  return version.snapshot_digest ?? version.snapshot_hash
}

function latestVersion(versions, section) {
  const candidates = section.targetKind === 'profile'
    ? versions.filter((version) => String(version.status || 'published') === 'published')
    : versions
  return [...candidates].sort((left, right) => {
    if (section.targetKind === 'profile') {
      return Number(right.version || 0) - Number(left.version || 0) || Number(versionID(right, section) || 0) - Number(versionID(left, section) || 0)
    }
    return Number(right.revision || 0) - Number(left.revision || 0) || Number(versionID(right, section) || 0) - Number(versionID(left, section) || 0)
  })[0]
}

function validationHealthIssues(profile, validationResult) {
  if (!validationResult || Number(validationResult.profile_id || 0) !== Number(profile?.id || 0)) return []
  const errors = Array.isArray(validationResult.errors) ? validationResult.errors : []
  const resourceFailures = Array.isArray(validationResult.resource_health)
    ? validationResult.resource_health.filter((item) => item.status === 'failed')
    : []
  return [...errors, ...resourceFailures].map((issue, index) => ({
    section: issue.path || 'validation',
    index,
    severity: 'invalid',
    label: issue.path || issue.resource_type || 'Validate',
    message: `配置失效：${issue.message || issue.code || '校验失败'}`
  }))
}

function dependencyRefCount(profile = {}) {
  return arrayLength(profile.skills) + arrayLength(profile.subagents) + arrayLength(profile.mcp_servers)
}

function findByReference(items, reference) {
  return (Array.isArray(items) ? items : []).find((item) => [
    item?.resource_key,
    item?.resource_id,
    item?.id,
    item?.name
  ].some((value) => hasValue(value) && String(value) === String(reference)))
}

function normalizeResourceID(value) {
  const numeric = Number(value)
  if (Number.isInteger(numeric) && numeric > 0 && String(value).trim() === String(numeric)) return numeric
  return value
}

function normalizeToolDefinitions(raw) {
  if (!Array.isArray(raw)) return []
  return raw
    .map((item) => {
      if (typeof item === 'string') return { name: item, description: '', operation_type: '' }
      if (!item || typeof item !== 'object') return null
      const name = String(item.name || item.tool_name || '').trim()
      if (!name) return null
      return {
        name,
        description: String(item.description || '').trim(),
        operation_type: String(item.operation_type || item.operationType || item.operation || '').trim()
      }
    })
    .filter(Boolean)
}

function normalizeToolPermissionDecision(value) {
  const decision = String(value || '').trim().toLowerCase()
  if (decision === 'ask') return 'request'
  return TOOL_PERMISSION_DECISIONS.includes(decision) ? decision : 'request'
}

function positiveNumberOrUndefined(value) {
  const numeric = Number(value)
  return Number.isInteger(numeric) && numeric > 0 ? numeric : undefined
}

function positiveNumberOrEmpty(value) {
  return positiveNumberOrUndefined(value) || ''
}

function hasValue(value) {
  return value !== undefined && value !== null && String(value).trim() !== ''
}

function arrayLength(value) {
  return Array.isArray(value) ? value.length : 0
}

function stringifyJSON(value) {
  return JSON.stringify(value, null, 2)
}

function asObject(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

function parseObject(value, fallback = {}) {
  if (value && typeof value === 'object' && !Array.isArray(value)) return { ...value }
  try {
    const parsed = JSON.parse(String(value || '{}'))
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : fallback
  } catch {
    return fallback
  }
}

function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value || {}))
}
