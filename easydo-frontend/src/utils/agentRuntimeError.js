const RUNTIME_ERROR_FIELDS = [
  'code',
  'category',
  'message',
  'user_message',
  'retryable',
  'http_status',
  'request_id',
  'runtime_run_id',
  'terminal_status',
  'phase',
  'source'
]

function isRecord(value) {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function parseJsonRecord(value) {
  if (typeof value !== 'string' || !value.trim()) return null
  try {
    const parsed = JSON.parse(value)
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

function nestedErrorRecords(value, seen = new Set()) {
  if (!isRecord(value) || seen.has(value)) return []
  seen.add(value)
  const nested = []
  const responseData = value.response?.data
  for (const candidate of [responseData, value.data, value.payload, value.error]) {
    const parsed = parseJsonRecord(candidate)
    const record = parsed || (isRecord(candidate) ? candidate : null)
    if (record) nested.push(...nestedErrorRecords(record, seen))
  }
  return [...nested, value]
}

function firstDefined(records, field) {
  for (const record of records) {
    if (record[field] !== undefined && record[field] !== null && record[field] !== '') return record[field]
  }
  return undefined
}

function positiveHttpStatus(value) {
  const status = Number(value)
  return Number.isInteger(status) && status >= 100 && status <= 599 ? status : undefined
}

function plainMessage(value) {
  if (typeof value !== 'string') return ''
  const text = value.trim()
  if (!text) return ''
  const parsed = parseJsonRecord(text)
  if (!parsed) return text
  return String(parsed.user_message || parsed.message || parsed.error || '').trim()
}

const CREDENTIAL_ASSIGNMENT = /(\b(?:x[_-]?api[_-]?key|api[_-]?key|apikey|access[_-]?key|secret[_-]?key|authorization|access[_-]?token|refresh[_-]?token|token|secret|password|credential|client[_-]?secret|cookie|key)\b\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&}\]]+)/gi
const BEARER_CREDENTIAL = /(\bBearer\s+)[A-Za-z0-9._~+/=-]+/gi

function redactCredentialAliases(value) {
  const text = plainMessage(value)
  if (!text) return ''
  return text
    .replace(BEARER_CREDENTIAL, '$1[REDACTED]')
    .replace(CREDENTIAL_ASSIGNMENT, '$1[REDACTED]')
}

export class AgentRuntimeError extends Error {
  constructor(fields = {}, cause) {
    super(String(fields.message || fields.user_message || 'AI runtime request failed'), cause ? { cause } : undefined)
    this.name = 'AgentRuntimeError'
    for (const field of RUNTIME_ERROR_FIELDS) {
      if (fields[field] !== undefined) this[field] = fields[field]
    }
  }

  toJSON() {
    return Object.fromEntries(RUNTIME_ERROR_FIELDS
      .filter((field) => this[field] !== undefined)
      .map((field) => [field, this[field]]))
  }
}

export function normalizeAgentRuntimeError(value, fallbackMessage = 'AI runtime request failed', defaults = {}) {
  if (value instanceof AgentRuntimeError) return value

  const parsedValue = parseJsonRecord(value)
  const root = parsedValue || (isRecord(value) ? value : { message: value })
  const records = nestedErrorRecords(root)
  const responseStatus = positiveHttpStatus(root?.response?.status)
  const sourceMessage = redactCredentialAliases(firstDefined(records, 'message')) || redactCredentialAliases(firstDefined(records, 'error'))
  const category = String(firstDefined(records, 'category') || defaults.category || 'unknown')
  const terminalStatus = firstDefined(records, 'terminal_status') || defaults.terminal_status
  const explicitRetryable = firstDefined(records, 'retryable')
  const fallback = redactCredentialAliases(defaults.user_message || fallbackMessage) || 'AI runtime request failed'
  const explicitUserMessage = redactCredentialAliases(firstDefined(records, 'user_message'))
  const unstructuredInput = value instanceof Error || (typeof value === 'string' && !parsedValue)
  const fields = {
    code: String(firstDefined(records, 'code') || defaults.code || 'agent_runtime_error'),
    category,
    message: sourceMessage || redactCredentialAliases(defaults.message || fallbackMessage) || fallback,
    user_message: explicitUserMessage || (category === 'unknown' || unstructuredInput ? fallback : sourceMessage || fallback),
    retryable: typeof explicitRetryable === 'boolean' ? explicitRetryable : Boolean(defaults.retryable),
    http_status: positiveHttpStatus(firstDefined(records, 'http_status')) || responseStatus || positiveHttpStatus(defaults.http_status),
    request_id: firstDefined(records, 'request_id') || defaults.request_id,
    runtime_run_id: firstDefined(records, 'runtime_run_id') || defaults.runtime_run_id,
    terminal_status: terminalStatus,
    phase: firstDefined(records, 'phase') || defaults.phase,
    source: firstDefined(records, 'source') || defaults.source
  }
  return new AgentRuntimeError(fields, value instanceof Error ? value : undefined)
}

export function agentRuntimeErrorMessage(value, fallbackMessage = 'AI runtime request failed') {
  const error = normalizeAgentRuntimeError(value, fallbackMessage)
  return plainMessage(error.user_message) || plainMessage(error.message) || fallbackMessage
}

export function isRetryableAgentRuntimeTransportError(value, { active = false } = {}) {
  if (!active) return false
  const error = normalizeAgentRuntimeError(value)
  return error.category === 'transport' && error.source === 'fetch' && error.retryable === true && !error.terminal_status
}

export function agentRuntimeErrorPayload(value, fallbackMessage, defaults = {}) {
  return normalizeAgentRuntimeError(value, fallbackMessage, defaults).toJSON()
}
