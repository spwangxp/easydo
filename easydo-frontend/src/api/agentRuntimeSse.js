import { agentRuntimeErrorPayload, normalizeAgentRuntimeError } from '../utils/agentRuntimeError.js'

function isRetryableHttpStatus(status) {
  return status === 429 || status >= 500
}

export async function fetchAgentRuntime(input, init = {}, options = {}) {
  const fetchImpl = options.fetchImpl || globalThis.fetch
  try {
    return await fetchImpl(input, init)
  } catch (error) {
    if (error?.name === 'AbortError') throw error
    throw normalizeAgentRuntimeError(error, options.fallbackMessage || 'AI runtime 请求失败', {
      code: 'fetch_transport_error',
      category: 'transport',
      source: 'fetch',
      retryable: true,
      phase: options.phase || 'fetch'
    })
  }
}

export async function parseAgentRuntimeSseChunk(buffer, onEvent, options) {
  const chunks = buffer.split(/\r?\n\r?\n/)
  const rest = chunks.pop() || ''
  let visibleEvents = 0
  for (const chunk of chunks) {
    const lines = chunk.split(/\r?\n/)
    const event = lines.find((line) => line.startsWith('event:'))?.replace(/^event:\s?/, '').trim() || 'message'
    const data = lines
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.replace(/^data:\s?/, ''))
      .join('\n')
    if (!data) continue
    let payload
    try {
      payload = JSON.parse(data)
    } catch {
      onEvent({
        event: 'stream_protocol_error',
        data: agentRuntimeErrorPayload(null, options.protocolMessage, {
          code: 'stream_event_json_invalid',
          category: 'transport',
          source: 'sse',
          retryable: true,
          phase: 'stream_decode'
        })
      })
      continue
    }
    onEvent({ event, data: payload })
    if (options.isVisibleEvent(options.visibleEventName(event, payload))) {
      visibleEvents += 1
      if (visibleEvents >= options.eventsPerFrame) {
        visibleEvents = 0
        await options.waitForPaint()
      }
    }
  }
  if (visibleEvents > 0) await options.waitForPaint()
  return rest
}

export async function readAgentRuntimeSseResponse(response, onEvent, options) {
  if (!response.ok) {
    const errorText = await response.text()
    throw normalizeAgentRuntimeError(errorText, options.fallbackMessage, {
      category: isRetryableHttpStatus(response.status) ? 'transport' : 'http',
      source: 'http',
      retryable: isRetryableHttpStatus(response.status),
      http_status: response.status,
      phase: 'http_response'
    })
  }
  if (!response.body) {
    throw normalizeAgentRuntimeError(null, options.emptyMessage, {
      code: 'stream_body_missing',
      category: 'transport',
      source: 'sse',
      retryable: true,
      phase: 'stream_open'
    })
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let streamError = null
  const consumeEvent = ({ event, data }) => {
    onEvent({ event, data })
    if (event === 'error' || event === 'stream_protocol_error') {
      streamError = normalizeAgentRuntimeError(data, options.fallbackMessage, { source: 'sse' })
    }
  }
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      buffer = await parseAgentRuntimeSseChunk(buffer, consumeEvent, options)
      if (streamError) throw streamError
    }
    if (buffer.trim()) {
      await parseAgentRuntimeSseChunk(`${buffer}\n\n`, consumeEvent, options)
      if (streamError) throw streamError
    }
  } catch (error) {
    if (error?.name === 'AbortError' || error?.name === 'AgentRuntimeError') throw error
    if (!(error instanceof TypeError) && error?.name !== 'NetworkError') throw error
    throw normalizeAgentRuntimeError(error, options.fallbackMessage || 'AI runtime 请求失败', {
      code: 'fetch_transport_error',
      category: 'transport',
      source: 'fetch',
      retryable: true,
      phase: 'stream_read'
    })
  }
}
