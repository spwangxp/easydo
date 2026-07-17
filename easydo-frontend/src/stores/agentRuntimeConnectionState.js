import { isRetryableAgentRuntimeTransportError, normalizeAgentRuntimeError } from '../utils/agentRuntimeError.js'

export function shouldRetryAgentRuntimeConnection(error, sessionSummary) {
  return isRetryableAgentRuntimeTransportError(error, {
    active: Boolean(sessionSummary?.active_run?.runtime_run_id)
  })
}

export function handleRuntimeEventWithCursor(handleEvent, commitCursor) {
  const result = handleEvent()
  if (result && typeof result.then === 'function') {
    return Promise.resolve(result).then((value) => {
      commitCursor()
      return value
    })
  }
  commitCursor()
  return result
}

export function isAgentRuntimeFollowCurrent({
  epoch,
  currentEpoch,
  sessionId,
  currentSessionId,
  stopping = false
}) {
  return !stopping && epoch === currentEpoch && String(sessionId || '') === String(currentSessionId || '')
}

export async function resolveAgentRuntimeFollowError(error, options) {
  if (!isAgentRuntimeFollowCurrent(options.followState)) {
    return { silent: true, error: null, summary: options.previousSummary, retry: false }
  }

  const normalized = normalizeAgentRuntimeError(error, options.fallbackMessage, error?.name === 'AbortError'
    ? {
        code: 'fetch_aborted_unexpectedly',
        category: 'transport',
        source: 'fetch',
        retryable: true,
        phase: 'stream_read'
      }
    : {})
  const summary = await options.refreshSummary().catch(() => options.previousSummary)
  return {
    silent: false,
    error: normalized,
    summary,
    retry: shouldRetryAgentRuntimeConnection(normalized, summary)
  }
}
