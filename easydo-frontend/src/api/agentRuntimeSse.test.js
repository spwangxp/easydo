import test from 'node:test'
import assert from 'node:assert/strict'

const sseModule = await import('./agentRuntimeSse.js').catch(() => ({}))
const { fetchAgentRuntime, readAgentRuntimeSseResponse } = sseModule

function sseResponse(text, init = {}) {
  return new Response(new TextEncoder().encode(text), {
    status: init.status || 200,
    headers: { 'Content-Type': 'text/event-stream' }
  })
}

const parserOptions = {
  fallbackMessage: '实时连接失败',
  emptyMessage: '流式响应为空',
  protocolMessage: '收到无法解析的事件',
  isVisibleEvent: () => false,
  visibleEventName: (event) => event,
  waitForPaint: async () => {},
  eventsPerFrame: 12
}

test('SSE reader preserves and throws a structured live error after delivering it', async () => {
  assert.equal(typeof readAgentRuntimeSseResponse, 'function')
  const events = []
  const payload = {
    code: 'provider_timeout',
    category: 'provider_timeout',
    source: 'provider',
    message: 'provider timed out',
    user_message: '模型响应超时',
    retryable: true,
    http_status: 504,
    request_id: 'req-sse',
    runtime_run_id: 'run-sse',
    terminal_status: 'timeout',
    phase: 'model'
  }
  const response = sseResponse(`event: error\ndata: ${JSON.stringify(payload)}\n\n`)

  await assert.rejects(
    readAgentRuntimeSseResponse(response, (event) => events.push(event), parserOptions),
    (error) => {
      assert.deepEqual(error.toJSON(), payload)
      return true
    }
  )
  assert.deepEqual(events, [{ event: 'error', data: payload }])
})

test('SSE reader turns malformed JSON into a retryable protocol error without raw payload leakage', async () => {
  assert.equal(typeof readAgentRuntimeSseResponse, 'function')
  const events = []

  await assert.rejects(
    readAgentRuntimeSseResponse(sseResponse('event: runtime_event\ndata: {api_key:"secret-value"}\n\n'), (event) => events.push(event), parserOptions),
    (error) => error.code === 'stream_event_json_invalid' && error.category === 'transport' && error.retryable === true
  )
  assert.equal(events.length, 1)
  assert.equal(events[0].event, 'stream_protocol_error')
  assert.equal('raw' in events[0].data, false)
  assert.doesNotMatch(JSON.stringify(events[0]), /secret-value/)
})

test('fetch wrapper marks only an actual fetch rejection as retryable transport', async () => {
  assert.equal(typeof fetchAgentRuntime, 'function')

  await assert.rejects(
    fetchAgentRuntime('/api/ai/test', {}, {
      fetchImpl: async () => { throw new TypeError('Failed to fetch') },
      fallbackMessage: '实时连接失败',
      phase: 'stream_open'
    }),
    (error) => error.category === 'transport' && error.retryable === true && error.source === 'fetch'
  )
})

test('SSE reader marks mid-stream transport disconnects as retryable fetch transport errors', async () => {
  assert.equal(typeof readAgentRuntimeSseResponse, 'function')
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode('event: runtime_event\ndata: {"event":{"event_id":"evt-1"}}\n\n'))
      queueMicrotask(() => controller.error(new TypeError('network error')))
    }
  })
  const response = new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' }
  })

  await assert.rejects(
    readAgentRuntimeSseResponse(response, () => {}, parserOptions),
    (error) => (
      error.category === 'transport'
      && error.retryable === true
      && error.source === 'fetch'
      && error.phase === 'stream_read'
      && error.code === 'fetch_transport_error'
    )
  )
})

test('SSE reader propagates consumer failures instead of treating the event as consumed', async () => {
  assert.equal(typeof readAgentRuntimeSseResponse, 'function')
  const handlerError = new Error('render failed')

  await assert.rejects(
    readAgentRuntimeSseResponse(
      sseResponse('event: runtime_event\ndata: {"event":{"event_id":"evt-1"}}\n\n'),
      () => { throw handlerError },
      parserOptions
    ),
    (error) => error === handlerError
  )
})
