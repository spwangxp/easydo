import test from 'node:test'
import assert from 'node:assert/strict'

const runtimeErrorModule = await import('./agentRuntimeError.js').catch(() => ({}))
const {
  agentRuntimeErrorMessage,
  isRetryableAgentRuntimeTransportError,
  normalizeAgentRuntimeError
} = runtimeErrorModule

test('normalizes axios HTTP JSON without losing the runtime error contract', () => {
  assert.equal(typeof normalizeAgentRuntimeError, 'function')
  const error = normalizeAgentRuntimeError({
    response: {
      status: 503,
      data: {
        code: 'provider_transport_error',
        category: 'transport',
        message: 'upstream reset',
        user_message: '模型服务连接已中断',
        retryable: true,
        http_status: 503,
        request_id: 'req-1',
        runtime_run_id: 'run-1',
        terminal_status: 'failed',
        phase: 'provider_call'
      }
    }
  })

  assert.deepEqual(error.toJSON(), {
    code: 'provider_transport_error',
    category: 'transport',
    message: 'upstream reset',
    user_message: '模型服务连接已中断',
    retryable: true,
    http_status: 503,
    request_id: 'req-1',
    runtime_run_id: 'run-1',
    terminal_status: 'failed',
    phase: 'provider_call'
  })
  assert.equal(agentRuntimeErrorMessage(error), '模型服务连接已中断')
})

test('normalizes live and replay errors through the same field projection', () => {
  assert.equal(typeof normalizeAgentRuntimeError, 'function')
  const live = normalizeAgentRuntimeError({
    code: 'provider_timeout',
    category: 'provider_timeout',
    message: 'provider timed out',
    user_message: '模型响应超时',
    retryable: true,
    http_status: 504,
    request_id: 'req-2',
    runtime_run_id: 'run-2',
    terminal_status: 'timeout',
    phase: 'model'
  })
  const replay = normalizeAgentRuntimeError({
    type: 'session.error',
    payload: {
      error: {
        code: 'provider_timeout',
        category: 'provider_timeout',
        message: 'provider timed out',
        user_message: '模型响应超时',
        retryable: true,
        http_status: 504,
        request_id: 'req-2',
        runtime_run_id: 'run-2',
        terminal_status: 'timeout',
        phase: 'model'
      }
    }
  })

  assert.deepEqual(replay.toJSON(), live.toJSON())
})

test('never exposes raw JSON strings as the user-facing error message', () => {
  assert.equal(typeof normalizeAgentRuntimeError, 'function')
  const error = normalizeAgentRuntimeError('{"code":"invalid_request","message":"bad request","user_message":"请求参数无效"}')

  assert.equal(error.code, 'invalid_request')
  assert.equal(error.message, 'bad request')
  assert.equal(agentRuntimeErrorMessage(error), '请求参数无效')
})

test('retries only explicitly retryable transport errors while a Run is active', () => {
  assert.equal(typeof isRetryableAgentRuntimeTransportError, 'function')
  const transport = normalizeAgentRuntimeError({ category: 'transport', source: 'fetch', retryable: true, message: 'socket closed' })
  const terminalTransport = normalizeAgentRuntimeError({ category: 'transport', source: 'fetch', retryable: true, terminal_status: 'failed', message: 'run failed' })
  const providerTimeout = normalizeAgentRuntimeError({ category: 'provider_timeout', retryable: true, message: 'provider timed out' })
  const httpTransport = normalizeAgentRuntimeError({ category: 'transport', source: 'http', retryable: true, message: 'bad gateway' })
  const sseTransport = normalizeAgentRuntimeError({ category: 'transport', source: 'sse', retryable: true, message: 'bad event' })

  assert.equal(isRetryableAgentRuntimeTransportError(transport, { active: true }), true)
  assert.equal(isRetryableAgentRuntimeTransportError(transport, { active: false }), false)
  assert.equal(isRetryableAgentRuntimeTransportError(terminalTransport, { active: true }), false)
  assert.equal(isRetryableAgentRuntimeTransportError(providerTimeout, { active: true }), false)
  assert.equal(isRetryableAgentRuntimeTransportError(httpTransport, { active: true }), false)
  assert.equal(isRetryableAgentRuntimeTransportError(sseTransport, { active: true }), false)
})

test('does not classify an arbitrary TypeError as a transport failure', () => {
  assert.equal(typeof normalizeAgentRuntimeError, 'function')
  const error = normalizeAgentRuntimeError(new TypeError('Cannot read properties of undefined'), '实时连接已中断')

  assert.equal(error.category, 'unknown')
  assert.equal(error.retryable, false)
  assert.equal(agentRuntimeErrorMessage(error), '实时连接已中断')
})

test('preserves source while redacting credential aliases from structured messages', () => {
  const error = normalizeAgentRuntimeError({
    code: 'provider_transport_error',
    category: 'transport',
    source: 'fetch',
    retryable: true,
    message: 'Authorization: Bearer access-123 api_key=key-456 password: pass-789 key=raw-key access_key=access-key secret_key=secret-key x-api-key=x-key',
    user_message: 'token=visible-secret 请求失败'
  }, '模型请求失败')

  assert.equal(error.source, 'fetch')
  assert.doesNotMatch(error.message, /access-123|key-456|pass-789|raw-key|access-key|secret-key|x-key/)
  assert.doesNotMatch(error.user_message, /visible-secret/)
  assert.match(error.message, /\[REDACTED\]/)
  assert.match(error.user_message, /\[REDACTED\]/)
})

test('uses the safe fallback for unknown unstructured errors', () => {
  const error = normalizeAgentRuntimeError(new Error('database failed password=hunter2'), '页面助手请求失败')

  assert.equal(agentRuntimeErrorMessage(error), '页面助手请求失败')
  assert.doesNotMatch(error.message, /hunter2/)
})
