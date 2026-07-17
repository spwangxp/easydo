export type RuntimeErrorCategory =
  | 'provider_timeout'
  | 'provider_rate_limit'
  | 'transport'
  | 'mcp'
  | 'permission'
  | 'cancelled'
  | 'validation'
  | 'internal'

export type RuntimeErrorSource = 'provider' | 'mcp' | 'permission' | 'user' | 'validation' | 'runtime' | 'workspace' | 'tool' | 'harness'

export interface RuntimeErrorDescriptor {
  code: string
  category: RuntimeErrorCategory
  message: string
  user_message: string
  retryable: boolean
  http_status: number
  source: RuntimeErrorSource
  terminal_status: 'failed' | 'timeout' | 'cancelled'
}

export interface RuntimeErrorContext {
  source?: RuntimeErrorSource
  code?: string
  http_status?: number
}

export interface RuntimeSourceErrorInput {
  source: RuntimeErrorSource
  code: string
  message: string
  http_status?: number
  retryable?: boolean
  cause?: unknown
}

export class RuntimeSourceError extends Error {
  readonly source: RuntimeErrorSource
  readonly code: string
  readonly http_status?: number
  readonly retryable?: boolean
  override readonly cause?: unknown

  constructor(input: RuntimeSourceErrorInput) {
    super(input.message)
    this.name = 'RuntimeSourceError'
    this.source = input.source
    this.code = input.code
    this.http_status = input.http_status
    this.retryable = input.retryable
    this.cause = input.cause
  }
}

export function classifyRuntimeError(error: unknown, context: RuntimeErrorContext = {}): RuntimeErrorDescriptor {
  const record = asRecord(error)
  const rawMessage = error instanceof Error ? error.message : firstString(record.message, String(error || ''))
  const message = redactSensitiveText(rawMessage || 'Unknown runtime error')
  const source = context.source || runtimeErrorSource(record.source) || inferSource(record)
  const suppliedCode = firstString(context.code, record.code)
  const httpStatus = context.http_status || positiveStatus(record.status) || positiveStatus(record.http_status) || statusFromMessage(rawMessage)
  const explicitRetryable = typeof record.retryable === 'boolean' ? record.retryable : undefined
  const errorName = error instanceof Error ? error.name : firstString(record.name)
  const text = `${suppliedCode} ${errorName} ${rawMessage}`.toLowerCase()

  if (source === 'user' || /user_cancelled|cancelled_by_user|canceled_by_user/.test(text)) {
    return descriptor(suppliedCode || 'runtime_cancelled', 'cancelled', message, 'The run was cancelled.', false, httpStatus || 409, 'user', 'cancelled')
  }
  if (
    suppliedCode === 'runtime_cancelled' ||
    /runtime_cancelled|harness stopped with aborted|stop(?:ped)? with aborted|\bstopreason\s*[:=]\s*aborted\b/.test(text) ||
    (source !== 'provider' && !isTimeout(text) && (/\baborted\b/.test(text) || errorName === 'AbortError'))
  ) {
    return descriptor(
      suppliedCode || 'runtime_cancelled',
      'cancelled',
      message,
      'The run was cancelled.',
      false,
      httpStatus || 409,
      source === 'harness' ? 'harness' : 'runtime',
      'cancelled'
    )
  }
  if (source === 'permission' || httpStatus === 403 || /permission|forbidden|approval.?denied|access.?denied/.test(text)) {
    return descriptor(suppliedCode || 'runtime_permission_denied', 'permission', message, 'Permission was denied.', false, httpStatus || 403, source === 'runtime' ? 'permission' : source, 'failed')
  }
  if (source === 'validation' || [400, 404, 409, 422].includes(httpStatus || 0) || /validation|invalid|requires?|required|not.?found|conflict|no api.?key|missing.*credential|credential.*required/.test(text)) {
    return descriptor(suppliedCode || 'runtime_validation_failed', 'validation', message, message, false, httpStatus || 400, source === 'runtime' ? 'validation' : source, 'failed')
  }
  if (source === 'mcp' || /^mcp[_-]/.test(suppliedCode)) {
    const retryable = explicitRetryable ?? (isTimeout(text) || isTransientTransport(text, httpStatus))
    return descriptor(suppliedCode || 'mcp_request_failed', 'mcp', message, 'An MCP tool request failed.', retryable, httpStatus || (isTimeout(text) ? 504 : 502), 'mcp', 'failed')
  }
  if (isTimeout(text)) {
    if (source === 'provider') {
      return descriptor(stableProviderCode(suppliedCode, 'provider_timeout'), 'provider_timeout', message, 'The model provider timed out. Try again.', explicitRetryable ?? true, httpStatus || 504, source, 'timeout')
    }
    return descriptor(suppliedCode || `${source}_timeout`, 'transport', message, 'The AI runtime operation timed out. Try again.', explicitRetryable ?? true, httpStatus || 504, source, 'timeout')
  }
  if (httpStatus === 429 || /rate.?limit|too many requests/.test(text)) {
    if (source === 'provider') {
      return descriptor(stableProviderCode(suppliedCode, 'provider_rate_limit'), 'provider_rate_limit', message, 'The model provider is rate limited. Try again shortly.', explicitRetryable ?? true, 429, source, 'failed')
    }
    return descriptor(suppliedCode || `${source}_rate_limit`, 'transport', message, 'The AI runtime request was rate limited.', explicitRetryable ?? true, 429, source, 'failed')
  }
  if (isTransientTransport(text, httpStatus)) {
    const providerTransport = source === 'provider'
    return descriptor(
      providerTransport ? stableProviderCode(suppliedCode, 'provider_transport_error') : suppliedCode || `${source}_transport_error`,
      'transport',
      message,
      providerTransport ? 'The AI provider is temporarily unavailable. Try again.' : 'The AI runtime transport is temporarily unavailable. Try again.',
      explicitRetryable ?? true,
      httpStatus || 502,
      source,
      'failed'
    )
  }
  return descriptor(suppliedCode || 'runtime_internal_error', 'internal', message, 'The AI runtime encountered an internal error.', false, httpStatus || 500, source, 'failed')
}

export function runtimeErrorPublicPayload(descriptor: RuntimeErrorDescriptor) {
  return {
    code: descriptor.code,
    category: descriptor.category,
    message: descriptor.user_message,
    user_message: descriptor.user_message,
    retryable: descriptor.retryable,
    http_status: descriptor.http_status,
    source: descriptor.source,
    terminal_status: descriptor.terminal_status
  }
}

function descriptor(
  code: string,
  category: RuntimeErrorCategory,
  message: string,
  userMessage: string,
  retryable: boolean,
  httpStatus: number,
  source: RuntimeErrorSource,
  terminalStatus: RuntimeErrorDescriptor['terminal_status']
): RuntimeErrorDescriptor {
  return {
    code,
    category,
    message,
    user_message: userMessage,
    retryable,
    http_status: httpStatus,
    source,
    terminal_status: terminalStatus
  }
}

function stableProviderCode(code: string, fallback: string) {
  return /^(ETIMEDOUT|ECONNRESET|ECONNREFUSED|EAI_AGAIN|ENETUNREACH|ABORT_ERR)$/i.test(code) ? fallback : code || fallback
}

function inferSource(record: Record<string, unknown>): RuntimeErrorSource {
  const code = firstString(record.code).toLowerCase()
  if (code.startsWith('mcp_')) return 'mcp'
  if (code.startsWith('provider_') || code.startsWith('openrouter_') || code.startsWith('openai_')) return 'provider'
  if (code.includes('permission') || code.includes('approval')) return 'permission'
  if (code.includes('cancel')) return 'user'
  if (code.includes('validation') || code.includes('invalid')) return 'validation'
  return 'runtime'
}

function runtimeErrorSource(value: unknown): RuntimeErrorSource | undefined {
  return ['provider', 'mcp', 'permission', 'user', 'validation', 'runtime', 'workspace', 'tool', 'harness'].includes(String(value || ''))
    ? value as RuntimeErrorSource
    : undefined
}

function isTimeout(text: string) {
  return /timed?\s*out|timeout|deadline exceeded|etimedout/.test(text)
}

function isTransientTransport(text: string, httpStatus?: number) {
  return Boolean(
    (httpStatus && httpStatus >= 500 && httpStatus <= 599) ||
    /econnreset|econnrefused|eai_again|enetunreach|socket hang up|network error|service.?unavailable|temporarily unavailable|upstream.?connect|connection reset|connection refused/.test(text)
  )
}

function statusFromMessage(message: string) {
  const match = String(message || '').match(/\b([45]\d{2})\b/)
  return match ? Number(match[1]) : undefined
}

function positiveStatus(value: unknown) {
  const status = Number(value)
  return Number.isInteger(status) && status >= 400 && status <= 599 ? status : undefined
}

function redactSensitiveText(value: string) {
  return String(value || '')
    .replace(/(["']?(?:access[_-]?token|bearer[_-]?token|refresh[_-]?token|client[_-]?secret|api[_-]?key|x-api-key|password|token|secret|authorization)["']?\s*:\s*["'])[^"']*(["'])/gi, '$1[REDACTED]$2')
    .replace(/(authorization\s*[:=]\s*)(?:bearer\s+)?[^\s,;}]+/gi, '$1[REDACTED]')
    .replace(/\b(access[_-]?token|bearer[_-]?token|refresh[_-]?token|client[_-]?secret|api[_-]?key|password|token|secret|authorization)\s*[=:]\s*[^\s&]+/gi, '$1=[REDACTED]')
    .replace(/([?&](?:access[_-]?token|bearer[_-]?token|refresh[_-]?token|client[_-]?secret|api[_-]?key|password|token|secret|authorization)=)[^&#\s]+/gi, '$1[REDACTED]')
    .replace(/\bBearer\s+[a-zA-Z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
    .replace(/\bsk-[a-zA-Z0-9_-]+\b/g, '[REDACTED]')
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}
