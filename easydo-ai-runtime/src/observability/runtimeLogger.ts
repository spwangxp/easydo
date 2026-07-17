export type RuntimeLogLevel = 'debug' | 'info' | 'warn' | 'error'

export type RuntimeLogSink = (line: string) => void

export type RuntimeLogInput = Record<string, unknown> & {
  component?: unknown
  operation?: unknown
  outcome?: unknown
  request_id?: unknown
  workspace_id?: unknown
  session_id?: unknown
  runtime_run_id?: unknown
  parent_runtime_run_id?: unknown
  provider_id?: unknown
  mcp_server_id?: unknown
  subagent_id?: unknown
  code?: unknown
  category?: unknown
}

export interface RuntimeLogger {
  debug(input: RuntimeLogInput): void
  info(input: RuntimeLogInput): void
  warn(input: RuntimeLogInput): void
  error(input: RuntimeLogInput): void
}

export interface RuntimeLoggerOptions {
  sink?: RuntimeLogSink
  now?: () => string
}

const REDACTED = '[REDACTED]'
const CIRCULAR = '[CIRCULAR]'
const MAX_REDACTION_DEPTH = 12
const CONTEXT_FIELDS = [
  'component',
  'operation',
  'outcome',
  'request_id',
  'workspace_id',
  'session_id',
  'runtime_run_id',
  'parent_runtime_run_id',
  'provider_id',
  'mcp_server_id',
  'subagent_id',
  'code',
  'category'
] as const

function normalizedKey(key: string) {
  return key.toLowerCase().replace(/[^a-z0-9]/g, '')
}

function isSensitiveKey(key: string) {
  const normalized = normalizedKey(key)
  return normalized.endsWith('key') ||
    normalized === 'auth' ||
    normalized.includes('cookie') ||
    normalized === 'request' ||
    normalized === 'requestbody' ||
    normalized === 'responsebody' ||
    normalized === 'body' ||
    normalized === 'content' ||
    normalized === 'messages' ||
    normalized === 'profile' ||
    normalized.includes('authorization') ||
    normalized.includes('token') ||
    normalized.includes('apikey') ||
    normalized.includes('password') ||
    normalized.includes('passwd') ||
    normalized.includes('secret') ||
    normalized.includes('credential') ||
    normalized.includes('prompt') ||
    normalized.includes('toolresult') ||
    normalized.includes('tooloutput') ||
    normalized.includes('profilesnapshot')
}

function redactString(value: string) {
  return value
    .replace(/\bBearer\s+[^\s,;]+/gi, `Bearer ${REDACTED}`)
    .replace(/\b(Authorization|Proxy-Authorization)\s*[:=]\s*(?!Bearer\s+\[REDACTED\])(?:Basic\s+)?[^\s,;]+/gi, '$1: [REDACTED]')
    .replace(/\b(token|api[_-]?key|password|passwd|secret|credential)\s*[:=]\s*[^\s,;]+/gi, '$1=[REDACTED]')
}

function redactValue(value: unknown, seen: WeakSet<object>, depth: number): unknown {
  if (depth > MAX_REDACTION_DEPTH) return REDACTED
  if (typeof value === 'string') return redactString(value)
  if (typeof value === 'bigint') return value.toString()
  if (value === null || typeof value !== 'object') return value
  if (seen.has(value)) return CIRCULAR
  seen.add(value)

  if (value instanceof Error) {
    return {
      name: redactString(value.name || 'Error'),
      message: redactString(value.message || 'Runtime operation failed')
    }
  }
  if (Array.isArray(value)) {
    return value.map((item) => redactValue(item, seen, depth + 1))
  }

  const result: Record<string, unknown> = {}
  for (const [key, nestedValue] of Object.entries(value as Record<string, unknown>)) {
    result[key] = isSensitiveKey(key)
      ? REDACTED
      : redactValue(nestedValue, seen, depth + 1)
  }
  return result
}

function safeValue(value: unknown) {
  return redactValue(value, new WeakSet<object>(), 0)
}

function safeContextValue(value: unknown) {
  return ['string', 'number', 'boolean', 'bigint'].includes(typeof value)
    ? safeValue(value)
    : undefined
}

export function createRuntimeLogger(options: RuntimeLoggerOptions = {}): RuntimeLogger {
  const sink = options.sink ?? ((line: string) => process.stdout.write(`${line}\n`))
  const currentTime = options.now ?? (() => new Date().toISOString())

  const write = (level: RuntimeLogLevel, input: RuntimeLogInput) => {
    const entry: Record<string, unknown> = {
      timestamp: currentTime(),
      level
    }
    for (const field of CONTEXT_FIELDS) {
      const value = input[field]
      const safe = safeContextValue(value)
      if (safe !== undefined && safe !== '') entry[field] = safe
    }
    sink(JSON.stringify(entry))
  }

  return {
    debug: (input) => write('debug', input),
    info: (input) => write('info', input),
    warn: (input) => write('warn', input),
    error: (input) => write('error', input)
  }
}

export const runtimeLogger = createRuntimeLogger()
