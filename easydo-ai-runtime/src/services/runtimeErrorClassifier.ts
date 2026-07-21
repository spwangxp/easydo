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
  const rawMessage = unwrapErrorMessage(error) || firstString(record.message, String(error || ''))
  // Truthful surface text: redacted real error only. Never replace with generic masks.
  const message = redactSensitiveText(rawMessage || 'Unknown runtime error')
  const source = context.source || runtimeErrorSource(record.source) || inferSource(record, rawMessage)
  const suppliedCode = firstString(context.code, record.code)
  const httpStatus = context.http_status || positiveStatus(record.status) || positiveStatus(record.http_status) || statusFromMessage(rawMessage)
  const explicitRetryable = typeof record.retryable === 'boolean' ? record.retryable : undefined
  const errorName = error instanceof Error ? error.name : firstString(record.name)
  const text = `${suppliedCode} ${errorName} ${rawMessage}`.toLowerCase()

  if (source === 'user' || /user_cancelled|cancelled_by_user|canceled_by_user/.test(text)) {
    return descriptor(suppliedCode || 'runtime_cancelled', 'cancelled', message, false, httpStatus || 409, 'user', 'cancelled')
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
      false,
      httpStatus || 409,
      source === 'harness' ? 'harness' : 'runtime',
      'cancelled'
    )
  }
  if (source === 'permission' || httpStatus === 403 || /permission|forbidden|approval.?denied|access.?denied/.test(text)) {
    return descriptor(suppliedCode || 'runtime_permission_denied', 'permission', message, false, httpStatus || 403, source === 'runtime' ? 'permission' : source, 'failed')
  }
  if (source === 'validation' || [400, 404, 409, 422].includes(httpStatus || 0) || /validation|invalid|requires?|required|not.?found|conflict|no api.?key|missing.*credential|credential.*required/.test(text)) {
    return descriptor(suppliedCode || 'runtime_validation_failed', 'validation', message, false, httpStatus || 400, source === 'runtime' ? 'validation' : source, 'failed')
  }
  if (source === 'mcp' || /^mcp[_-]/.test(suppliedCode)) {
    const retryable = explicitRetryable ?? (isTimeout(text) || isTransientTransport(text, httpStatus))
    return descriptor(suppliedCode || 'mcp_request_failed', 'mcp', message, retryable, httpStatus || (isTimeout(text) ? 504 : 502), 'mcp', 'failed')
  }
  if (isTimeout(text)) {
    if (source === 'provider') {
      return descriptor(stableProviderCode(suppliedCode, 'provider_timeout'), 'provider_timeout', message, explicitRetryable ?? true, httpStatus || 504, source, 'timeout')
    }
    return descriptor(suppliedCode || `${source}_timeout`, 'transport', message, explicitRetryable ?? true, httpStatus || 504, source, 'timeout')
  }
  if (isRateLimit(text, httpStatus)) {
    if (source === 'provider' || isProviderUpstreamText(text)) {
      return descriptor(
        stableProviderCode(suppliedCode, 'provider_rate_limit'),
        'provider_rate_limit',
        message,
        explicitRetryable ?? true,
        httpStatus || 429,
        source === 'runtime' ? 'provider' : source,
        'failed'
      )
    }
    return descriptor(suppliedCode || `${source}_rate_limit`, 'transport', message, explicitRetryable ?? true, httpStatus || 429, source, 'failed')
  }
  if (isTransientTransport(text, httpStatus)) {
    const providerTransport = source === 'provider' || isProviderUpstreamText(text)
    return descriptor(
      providerTransport ? stableProviderCode(suppliedCode, 'provider_transport_error') : suppliedCode || `${source}_transport_error`,
      'transport',
      message,
      explicitRetryable ?? true,
      httpStatus || 502,
      providerTransport && source === 'runtime' ? 'provider' : source,
      'failed'
    )
  }
  return descriptor(suppliedCode || 'runtime_internal_error', 'internal', message, false, httpStatus || 500, source, 'failed')
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
  retryable: boolean,
  httpStatus: number,
  source: RuntimeErrorSource,
  terminalStatus: RuntimeErrorDescriptor['terminal_status']
): RuntimeErrorDescriptor {
  return {
    code,
    category,
    message,
    user_message: message,
    retryable,
    http_status: httpStatus,
    source,
    terminal_status: terminalStatus
  }
}

function unwrapErrorMessage(error: unknown, depth = 0): string {
  if (depth > 6 || error == null) return ''
  if (typeof error === 'string') return error.trim()
  if (error instanceof AggregateError && Array.isArray(error.errors) && error.errors.length > 0) {
    const nested = unwrapErrorMessage(error.errors[0], depth + 1)
    if (nested) return nested
  }
  if (error instanceof Error) {
    const own = String(error.message || '').trim()
    const cause = 'cause' in error ? unwrapErrorMessage((error as { cause?: unknown }).cause, depth + 1) : ''
    if (cause && isWrapperErrorMessage(own)) return cause
    if (own) return own
    if (cause) return cause
  }
  const record = asRecord(error)
  return firstString(record.message, record.error_message, record.errorMessage, record.detail, record.reason)
}

function isWrapperErrorMessage(message: string) {
  return /agent run failed and failure reporting failed|failure reporting failed|the ai runtime encountered an internal error|the ai runtime transport is temporarily unavailable|the ai provider is temporarily unavailable|an mcp tool request failed/i.test(message)
}

function stableProviderCode(code: string, fallback: string) {
  return /^(ETIMEDOUT|ECONNRESET|ECONNREFUSED|EAI_AGAIN|ENETUNREACH|ABORT_ERR)$/i.test(code) ? fallback : code || fallback
}

function inferSource(record: Record<string, unknown>, rawMessage = ''): RuntimeErrorSource {
  const code = firstString(record.code).toLowerCase()
  const text = `${code} ${rawMessage}`.toLowerCase()
  if (code.startsWith('mcp_')) return 'mcp'
  if (code.startsWith('provider_') || code.startsWith('openrouter_') || code.startsWith('openai_')) return 'provider'
  if (isProviderUpstreamText(text)) return 'provider'
  if (code.includes('permission') || code.includes('approval')) return 'permission'
  if (code.includes('cancel')) return 'user'
  if (code.includes('validation') || code.includes('invalid')) return 'validation'
  return 'runtime'
}

function isProviderUpstreamText(text: string) {
  return /upstream error from|openrouter|nvidia|resourceexhausted|model provider|provider request|chat\.completions|openai/i.test(text)
}

function runtimeErrorSource(value: unknown): RuntimeErrorSource | undefined {
  return ['provider', 'mcp', 'permission', 'user', 'validation', 'runtime', 'workspace', 'tool', 'harness'].includes(String(value || ''))
    ? value as RuntimeErrorSource
    : undefined
}

function isTimeout(text: string) {
  return /timed?\s*out|timeout|deadline exceeded|etimedout/.test(text)
}

function isRateLimit(text: string, httpStatus?: number) {
  return Boolean(
    httpStatus === 429 ||
    /rate.?limit|too many requests|resourceexhausted|resource exhausted|request limit reached|worker local total request limit|quota.?exceeded|capacity.?exceeded|throttl/i.test(text)
  )
}

function isTransientTransport(text: string, httpStatus?: number) {
  if (/econnreset|econnrefused|eai_again|enetunreach|socket hang up|network error|service.?unavailable|temporarily unavailable|upstream.?connect|connection reset|connection refused|bad gateway|gateway timeout|fetch failed/i.test(text)) {
    return true
  }
  // HTTP 5xx alone is not enough — only treat as transport when the body is empty/generic.
  // Concrete application failures (including AggregateError wrappers) stay internal/provider.
  if (!(httpStatus && httpStatus >= 500 && httpStatus <= 599)) return false
  if (/resourceexhausted|rate.?limit|request limit|agent run failed|failure reporting failed|internal error/i.test(text)) {
    return false
  }
  return !text.trim() || /unknown runtime error|request failed|http\s*5\d\d|status\s*5\d\d/i.test(text)
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
