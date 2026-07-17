import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { classifyRuntimeError, RuntimeSourceError } from './runtimeErrorClassifier.js'
import { runtimeLogger, type RuntimeLogger, type RuntimeLogInput } from '../observability/runtimeLogger.js'

export interface McpClient {
  request(method: string, params: Record<string, unknown>, requestID?: string, correlation?: RuntimeLogInput): Promise<Record<string, unknown>>
  close(): Promise<void> | void
}

export function resolveMcpClientConfigSecrets(
  config: Record<string, unknown>,
  secretRef: Record<string, unknown> = {}
): Record<string, unknown> {
  const values = asRecord(secretRef.values ?? secretRef.secrets ?? secretRef.resolved)
  const resolveValue = (value: unknown): unknown => {
    if (typeof value === 'string') {
      const match = value.match(/^secret_ref:(.+)$/)
      if (!match) return value
      const key = match[1]?.trim() || ''
      const resolved = firstString(values[key], getSecretRefPath(secretRef, key))
      if (!resolved) {
        throw mcpClientConfigError(
          'mcp_secret_ref_unresolved',
          `MCP secret_ref ${key} is not configured`
        )
      }
      return resolved
    }
    if (Array.isArray(value)) return value.map(resolveValue)
    if (value && typeof value === 'object') {
      return Object.fromEntries(
        Object.entries(value as Record<string, unknown>)
          .map(([key, item]) => [key, resolveValue(item)])
      )
    }
    return value
  }
  return resolveValue(config) as Record<string, unknown>
}

export function createMcpClient(config: Record<string, unknown>, logger: RuntimeLogger = runtimeLogger): McpClient {
  const type = firstString(config.type, config.transport, config.command ? 'stdio' : 'streamable_http').toLowerCase()
  const timeoutMs = firstPositiveNumber(config.timeout_ms, config.timeoutMs, config.timeout) || 30_000
  const headers = stringRecord(config.headers)
  if (type === 'sse') {
    const url = firstString(config.url, config.sse_url, config.sseUrl)
    if (!url) throw mcpClientConfigError('mcp_sse_url_required', 'MCP SSE URL is required')
    return new McpSseClient(url, headers, timeoutMs, logger)
  }
  if (type === 'stdio' || type === 'command') {
    const command = firstString(config.command)
    if (!command) throw mcpClientConfigError('mcp_stdio_command_required', 'MCP stdio command is required')
    return new McpStdioClient(
      command,
      stringArray(config.args),
      stringRecord(config.env),
      firstString(config.cwd),
      timeoutMs,
      logger
    )
  }
  if (type !== 'streamable_http') {
    throw mcpClientConfigError('mcp_transport_unsupported', `Unsupported MCP transport: ${type || 'unknown'}`)
  }
  const url = firstString(config.url)
  if (!url) throw mcpClientConfigError('mcp_streamable_http_url_required', 'MCP Streamable HTTP URL is required')
  return new McpStreamableHttpClient(url, headers, timeoutMs, logger)
}

type PendingRequest = {
  resolve: (value: Record<string, unknown>) => void
  reject: (error: Error) => void
  timer: ReturnType<typeof setTimeout>
}

export class McpStreamableHttpClient {
  private sessionID = ''
  private initializeJob?: Promise<void>
  private requestSequence = 0

  constructor(
    private readonly url: string,
    private readonly headers: Record<string, string> = {},
    private readonly timeoutMs = 30_000,
    private readonly logger: RuntimeLogger = runtimeLogger
  ) {}

  async request(method: string, params: Record<string, unknown>, requestID?: string, correlation: RuntimeLogInput = {}) {
    const context: RuntimeLogInput = { ...correlation, component: 'mcp-client', operation: method }
    this.logger.info({ ...context, outcome: 'started' })
    try {
      await this.ensureInitialized()
      const result = await this.send(method, params, requestID)
      this.logger.info({ ...context, outcome: 'completed' })
      return result
    } catch (error) {
      const descriptor = classifyRuntimeError(error, { source: 'mcp' })
      this.logger.error({ ...context, outcome: descriptor.terminal_status, code: descriptor.code, category: descriptor.category })
      throw error
    }
  }

  async close() {
    if (!this.sessionID) return
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    timer.unref?.()
    try {
      await fetch(this.url, {
        method: 'DELETE',
        headers: this.requestHeaders(),
        signal: controller.signal
      })
    } finally {
      clearTimeout(timer)
      this.sessionID = ''
      this.initializeJob = undefined
    }
  }

  private async ensureInitialized() {
    if (this.sessionID) return
    if (!this.initializeJob) {
      this.initializeJob = this.initialize().catch((error) => {
        this.initializeJob = undefined
        throw error
      })
    }
    await this.initializeJob
  }

  private async initialize() {
    const response = await this.sendMessage('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'easydo-ai-runtime', version: '1' }
    }, false, false)
    this.sessionID = response.sessionID
    await this.sendMessage('notifications/initialized', {}, true, true)
  }

  private async send(method: string, params: Record<string, unknown>, requestID?: string) {
    const response = await this.sendMessage(method, params, false, true, requestID)
    return response.result
  }

  private async sendMessage(method: string, params: Record<string, unknown>, notification: boolean, includeSession: boolean, explicitID?: string) {
    const id = notification ? undefined : explicitID || `mcp-${++this.requestSequence}`
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    timer.unref?.()
    try {
      const headers = this.requestHeaders(includeSession)
      const response = await fetch(this.url, {
        method: 'POST',
        headers,
        body: JSON.stringify({ jsonrpc: '2.0', ...(id ? { id } : {}), method, params }),
        signal: controller.signal
      })
      const sessionID = response.headers.get('mcp-session-id') || this.sessionID
      if (notification && response.ok) return { result: {}, sessionID }
      const body = await response.text()
      if (!response.ok) {
        throw new RuntimeSourceError({
          source: 'mcp',
          code: response.status === 429 ? 'mcp_rate_limit' : 'mcp_http_error',
          message: `MCP request failed with HTTP ${response.status}`,
          http_status: response.status,
          retryable: response.status === 429 || response.status >= 500
        })
      }
      const message = parseMcpResponse(body, response.headers.get('content-type') || '', id)
      if (message.error) {
        const error = asRecord(message.error)
        throw new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_rpc_error',
          message: String(error.message || 'MCP request failed'),
          http_status: 502,
          retryable: false
        })
      }
      return { result: asRecord(message.result), sessionID }
    } catch (error) {
      if (controller.signal.aborted) {
        throw new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_timeout',
          message: `MCP request timed out after ${this.timeoutMs}ms`,
          http_status: 504,
          retryable: true,
          cause: error
        })
      }
      if (error instanceof RuntimeSourceError) throw error
      throw new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_transport_error',
        message: error instanceof Error ? error.message : 'MCP transport failed',
        http_status: 502,
        retryable: true,
        cause: error
      })
    } finally {
      clearTimeout(timer)
    }
  }

  private requestHeaders(includeSession = true) {
    return {
      accept: 'application/json, text/event-stream',
      'content-type': 'application/json',
      ...this.headers,
      ...(includeSession && this.sessionID ? { 'mcp-session-id': this.sessionID } : {})
    }
  }
}

export class McpSseClient implements McpClient {
  private endpointURL = ''
  private connectionJob?: Promise<void>
  private initializeJob?: Promise<void>
  private requestSequence = 0
  private abortController?: AbortController
  private pendingRequests = new Map<string, PendingRequest>()
  private endpointWaiter?: { resolve: () => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }

  constructor(
    private readonly url: string,
    private readonly headers: Record<string, string> = {},
    private readonly timeoutMs = 30_000,
    private readonly logger: RuntimeLogger = runtimeLogger
  ) {}

  async request(method: string, params: Record<string, unknown>, requestID?: string, correlation: RuntimeLogInput = {}) {
    const context: RuntimeLogInput = { ...correlation, component: 'mcp-client', operation: method }
    this.logger.info({ ...context, outcome: 'started' })
    try {
      await this.ensureInitialized()
      const result = await this.send(method, params, requestID)
      this.logger.info({ ...context, outcome: 'completed' })
      return result
    } catch (error) {
      const descriptor = classifyRuntimeError(error, { source: 'mcp' })
      this.logger.error({ ...context, outcome: descriptor.terminal_status, code: descriptor.code, category: descriptor.category })
      throw error
    }
  }

  async close() {
    this.abortController?.abort()
    this.abortController = undefined
    this.connectionJob = undefined
    this.initializeJob = undefined
    this.endpointURL = ''
    this.failPending(new RuntimeSourceError({
      source: 'mcp',
      code: 'mcp_transport_closed',
      message: 'MCP SSE transport closed',
      http_status: 499,
      retryable: true
    }))
  }

  private async ensureInitialized() {
    await this.ensureConnected()
    if (!this.initializeJob) {
      this.initializeJob = this.initialize().catch((error) => {
        this.initializeJob = undefined
        throw error
      })
    }
    await this.initializeJob
  }

  private async initialize() {
    await this.send('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'easydo-ai-runtime', version: '1' }
    })
    await this.postMessage('notifications/initialized', {}, true)
  }

  private async ensureConnected() {
    if (this.endpointURL) return
    if (!this.connectionJob) {
      this.connectionJob = this.openConnection().catch((error) => {
        this.connectionJob = undefined
        throw error
      })
    }
    await this.connectionJob
  }

  private async openConnection() {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    timer.unref?.()
    this.abortController = controller
    try {
      const response = await fetch(this.url, {
        method: 'GET',
        headers: {
          accept: 'text/event-stream',
          ...this.headers
        },
        signal: controller.signal
      })
      clearTimeout(timer)
      if (!response.ok || !response.body) {
        throw new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_sse_connect_failed',
          message: `MCP SSE connection failed with HTTP ${response.status}`,
          http_status: response.status || 502,
          retryable: response.status === 429 || response.status >= 500
        })
      }
      const endpointReady = new Promise<void>((resolve, reject) => {
        const endpointTimer = setTimeout(() => reject(new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_timeout',
          message: `MCP SSE endpoint was not received after ${this.timeoutMs}ms`,
          http_status: 504,
          retryable: true
        })), this.timeoutMs)
        endpointTimer.unref?.()
        this.endpointWaiter = { resolve, reject, timer: endpointTimer }
      })
      void this.readEvents(response.body.getReader()).catch((error) => this.failPending(error instanceof Error ? error : new Error(String(error))))
      await endpointReady
    } catch (error) {
      clearTimeout(timer)
      if (controller.signal.aborted) {
        throw new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_timeout',
          message: `MCP SSE connection timed out after ${this.timeoutMs}ms`,
          http_status: 504,
          retryable: true,
          cause: error
        })
      }
      if (error instanceof RuntimeSourceError) throw error
      throw new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_transport_error',
        message: error instanceof Error ? error.message : 'MCP SSE transport failed',
        http_status: 502,
        retryable: true,
        cause: error
      })
    }
  }

  private async send(method: string, params: Record<string, unknown>, requestID?: string) {
    return this.postMessage(method, params, false, requestID)
  }

  private async postMessage(method: string, params: Record<string, unknown>, notification = false, explicitID?: string) {
    await this.ensureConnected()
    const id = notification ? undefined : explicitID || `mcp-sse-${++this.requestSequence}`
    const body = JSON.stringify({ jsonrpc: '2.0', ...(id ? { id } : {}), method, params })
    const pending = id ? this.createPendingRequest(id) : Promise.resolve({})
    try {
      const response = await fetch(this.endpointURL, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          ...this.headers
        },
        body
      })
      const text = await response.text()
      if (!response.ok) {
        this.rejectPending(id, new RuntimeSourceError({
          source: 'mcp',
          code: response.status === 429 ? 'mcp_rate_limit' : 'mcp_http_error',
          message: `MCP SSE message failed with HTTP ${response.status}`,
          http_status: response.status,
          retryable: response.status === 429 || response.status >= 500
        }))
      }
      const contentType = response.headers.get('content-type') || ''
      if (id && text.trim() && contentType.includes('application/json')) {
        const message = asRecord(JSON.parse(text))
        if (message.id === id) this.resolveMessage(message)
      }
      return pending
    } catch (error) {
      this.rejectPending(id, error instanceof Error ? error : new Error(String(error)))
      if (error instanceof RuntimeSourceError) throw error
      throw new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_transport_error',
        message: error instanceof Error ? error.message : 'MCP SSE transport failed',
        http_status: 502,
        retryable: true,
        cause: error
      })
    }
  }

  private createPendingRequest(id: string) {
    return new Promise<Record<string, unknown>>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingRequests.delete(id)
        reject(new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_timeout',
          message: `MCP SSE request timed out after ${this.timeoutMs}ms`,
          http_status: 504,
          retryable: true
        }))
      }, this.timeoutMs)
      timer.unref?.()
      this.pendingRequests.set(id, { resolve, reject, timer })
    })
  }

  private async readEvents(reader: ReadableStreamDefaultReader<Uint8Array>) {
    const decoder = new TextDecoder()
    let buffer = ''
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const chunks = buffer.split(/\r?\n\r?\n/)
      buffer = chunks.pop() || ''
      for (const chunk of chunks) this.handleEventChunk(chunk)
    }
    if (buffer.trim()) this.handleEventChunk(buffer)
  }

  private handleEventChunk(chunk: string) {
    let event = 'message'
    const data: string[] = []
    for (const rawLine of chunk.split(/\r?\n/)) {
      const line = rawLine.trimEnd()
      if (line.startsWith('event:')) event = line.replace(/^event:\s?/, '').trim()
      if (line.startsWith('data:')) data.push(line.replace(/^data:\s?/, ''))
    }
    const body = data.join('\n')
    if (event === 'endpoint') {
      this.endpointURL = new URL(body, this.url).toString()
      if (this.endpointWaiter) {
        clearTimeout(this.endpointWaiter.timer)
        this.endpointWaiter.resolve()
        this.endpointWaiter = undefined
      }
      return
    }
    if (event === 'message' && body && body !== '[DONE]') {
      this.resolveMessage(asRecord(JSON.parse(body)))
    }
  }

  private resolveMessage(message: Record<string, unknown>) {
    const id = String(message.id || '')
    if (!id) return
    const pending = this.pendingRequests.get(id)
    if (!pending) return
    this.pendingRequests.delete(id)
    clearTimeout(pending.timer)
    if (message.error) {
      const error = asRecord(message.error)
      pending.reject(new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_rpc_error',
        message: String(error.message || 'MCP request failed'),
        http_status: 502,
        retryable: false
      }))
      return
    }
    pending.resolve(asRecord(message.result))
  }

  private rejectPending(id: string | undefined, error: Error) {
    if (!id) return
    const pending = this.pendingRequests.get(id)
    if (!pending) return
    this.pendingRequests.delete(id)
    clearTimeout(pending.timer)
    pending.reject(error)
  }

  private failPending(error: Error) {
    for (const [id, pending] of this.pendingRequests.entries()) {
      this.pendingRequests.delete(id)
      clearTimeout(pending.timer)
      pending.reject(error)
    }
    if (this.endpointWaiter) {
      clearTimeout(this.endpointWaiter.timer)
      this.endpointWaiter.reject(error)
      this.endpointWaiter = undefined
    }
  }
}

export class McpStdioClient implements McpClient {
  private child?: ChildProcessWithoutNullStreams
  private startJob?: Promise<void>
  private initializeJob?: Promise<void>
  private requestSequence = 0
  private pendingRequests = new Map<string, PendingRequest>()
  private stdoutBuffer = ''
  private stderrBuffer = ''

  constructor(
    private readonly command: string,
    private readonly args: string[] = [],
    private readonly env: Record<string, string> = {},
    private readonly cwd = '',
    private readonly timeoutMs = 30_000,
    private readonly logger: RuntimeLogger = runtimeLogger
  ) {}

  async request(method: string, params: Record<string, unknown>, requestID?: string, correlation: RuntimeLogInput = {}) {
    const context: RuntimeLogInput = { ...correlation, component: 'mcp-client', operation: method }
    this.logger.info({ ...context, outcome: 'started' })
    try {
      await this.ensureInitialized()
      const result = await this.send(method, params, requestID)
      this.logger.info({ ...context, outcome: 'completed' })
      return result
    } catch (error) {
      const descriptor = classifyRuntimeError(error, { source: 'mcp' })
      this.logger.error({ ...context, outcome: descriptor.terminal_status, code: descriptor.code, category: descriptor.category })
      throw error
    }
  }

  async close() {
    const child = this.child
    this.child = undefined
    this.startJob = undefined
    this.initializeJob = undefined
    if (child && !child.killed) child.kill()
    this.failPending(new RuntimeSourceError({
      source: 'mcp',
      code: 'mcp_transport_closed',
      message: 'MCP stdio transport closed',
      http_status: 499,
      retryable: true
    }))
  }

  private async ensureInitialized() {
    await this.ensureStarted()
    if (!this.initializeJob) {
      this.initializeJob = this.initialize().catch((error) => {
        this.initializeJob = undefined
        throw error
      })
    }
    await this.initializeJob
  }

  private async initialize() {
    await this.send('initialize', {
      protocolVersion: '2025-06-18',
      capabilities: {},
      clientInfo: { name: 'easydo-ai-runtime', version: '1' }
    })
    await this.writeMessage('notifications/initialized', {}, true)
  }

  private async ensureStarted() {
    if (this.child && !this.child.killed) return
    if (!this.startJob) {
      this.startJob = this.start().catch((error) => {
        this.startJob = undefined
        throw error
      })
    }
    await this.startJob
  }

  private async start() {
    if (!this.command.trim()) {
      throw new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_stdio_command_required',
        message: 'MCP stdio command is required',
        http_status: 400,
        retryable: false
      })
    }
    const child = spawn(this.command, this.args, {
      cwd: this.cwd || undefined,
      env: buildStdioSandboxEnv(this.env),
      shell: false,
      stdio: 'pipe'
    })
    this.child = child
    child.stdout.on('data', (chunk) => this.handleStdout(String(chunk)))
    child.stderr.on('data', (chunk) => {
      this.stderrBuffer = `${this.stderrBuffer}${String(chunk)}`.slice(-4096)
    })
    child.on('error', (error) => this.failPending(error))
    child.on('exit', (code) => {
      this.failPending(new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_stdio_exited',
        message: `MCP stdio process exited with code ${code ?? 'unknown'}${this.stderrBuffer ? `: ${this.stderrBuffer.trim()}` : ''}`,
        http_status: 502,
        retryable: true
      }))
    })
  }

  private async send(method: string, params: Record<string, unknown>, requestID?: string) {
    return this.writeMessage(method, params, false, requestID)
  }

  private async writeMessage(method: string, params: Record<string, unknown>, notification = false, explicitID?: string) {
    await this.ensureStarted()
    const id = notification ? undefined : explicitID || `mcp-stdio-${++this.requestSequence}`
    const pending = id ? this.createPendingRequest(id) : Promise.resolve({})
    const child = this.child
    if (!child || child.killed || !child.stdin.writable) {
      this.rejectPending(id, new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_stdio_unavailable',
        message: 'MCP stdio process is unavailable',
        http_status: 502,
        retryable: true
      }))
      return pending
    }
    child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', ...(id ? { id } : {}), method, params })}\n`)
    return pending
  }

  private createPendingRequest(id: string) {
    return new Promise<Record<string, unknown>>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pendingRequests.delete(id)
        reject(new RuntimeSourceError({
          source: 'mcp',
          code: 'mcp_timeout',
          message: `MCP stdio request timed out after ${this.timeoutMs}ms`,
          http_status: 504,
          retryable: true
        }))
      }, this.timeoutMs)
      timer.unref?.()
      this.pendingRequests.set(id, { resolve, reject, timer })
    })
  }

  private handleStdout(chunk: string) {
    this.stdoutBuffer += chunk
    const lines = this.stdoutBuffer.split(/\r?\n/)
    this.stdoutBuffer = lines.pop() || ''
    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed) continue
      this.resolveMessage(asRecord(JSON.parse(trimmed)))
    }
  }

  private resolveMessage(message: Record<string, unknown>) {
    const id = String(message.id || '')
    if (!id) return
    const pending = this.pendingRequests.get(id)
    if (!pending) return
    this.pendingRequests.delete(id)
    clearTimeout(pending.timer)
    if (message.error) {
      const error = asRecord(message.error)
      pending.reject(new RuntimeSourceError({
        source: 'mcp',
        code: 'mcp_rpc_error',
        message: String(error.message || 'MCP request failed'),
        http_status: 502,
        retryable: false
      }))
      return
    }
    pending.resolve(asRecord(message.result))
  }

  private rejectPending(id: string | undefined, error: Error) {
    if (!id) return
    const pending = this.pendingRequests.get(id)
    if (!pending) return
    this.pendingRequests.delete(id)
    clearTimeout(pending.timer)
    pending.reject(error)
  }

  private failPending(error: Error) {
    for (const [id, pending] of this.pendingRequests.entries()) {
      this.pendingRequests.delete(id)
      clearTimeout(pending.timer)
      pending.reject(error)
    }
  }
}

function parseMcpResponse(body: string, contentType: string, requestID?: string) {
  if (!contentType.includes('text/event-stream')) return asRecord(JSON.parse(body || '{}'))
  const messages = body.split(/\r?\n\r?\n/).flatMap((chunk) => {
    const data = chunk.split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.replace(/^data:\s?/, ''))
      .join('\n')
    if (!data || data === '[DONE]') return []
    return [asRecord(JSON.parse(data))]
  })
  return messages.find((message) => !requestID || String(message.id || '') === requestID) || messages.at(-1) || {}
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const item = String(value || '').trim()
    if (item) return item
  }
  return ''
}

function firstPositiveNumber(...values: unknown[]) {
  for (const value of values) {
    const numeric = Number(value)
    if (Number.isFinite(numeric) && numeric > 0) return numeric
  }
  return 0
}

function stringRecord(value: unknown) {
  return Object.fromEntries(Object.entries(asRecord(value)).map(([key, item]) => [key, String(item)]))
}

function stringArray(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item)).filter((item) => item.trim())
}

function getSecretRefPath(secretRef: Record<string, unknown>, key: string) {
  if (!key) return ''
  let current: unknown = secretRef
  for (const segment of key.split('.')) {
    const record = asRecord(current)
    if (!(segment in record)) return ''
    current = record[segment]
  }
  return typeof current === 'string' || typeof current === 'number' || typeof current === 'boolean'
    ? String(current)
    : ''
}

export function buildStdioSandboxEnv(configuredEnv: Record<string, string> = {}) {
  const allowlist = [
    'PATH',
    'HOME',
    'TMPDIR',
    'TMP',
    'TEMP',
    'LANG',
    'LC_ALL',
    'LC_CTYPE',
    'TERM',
    'NODE_PATH',
    'npm_config_cache',
    'XDG_CACHE_HOME',
    'XDG_CONFIG_HOME',
    'XDG_DATA_HOME'
  ]
  const base: Record<string, string> = {}
  for (const key of allowlist) {
    const value = process.env[key]
    if (value !== undefined && value !== '') base[key] = value
  }
  for (const [key, value] of Object.entries(configuredEnv || {})) {
    if (!key || value === undefined || value === null) continue
    base[String(key)] = String(value)
  }
  base.EASYDO_MCP_STDIO_SANDBOX = '1'
  return base
}

function mcpClientConfigError(code: string, message: string) {
  return new RuntimeSourceError({
    source: 'mcp',
    code,
    message,
    http_status: 400,
    retryable: false
  })
}
