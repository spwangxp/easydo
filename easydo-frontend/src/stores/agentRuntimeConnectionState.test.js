import test from 'node:test'
import assert from 'node:assert/strict'

const stateModule = await import('./agentRuntimeConnectionState.js').catch(() => ({}))
const {
  handleRuntimeEventWithCursor,
  isAgentRuntimeFollowCurrent,
  resolveAgentRuntimeFollowError,
  shouldRetryAgentRuntimeConnection
} = stateModule

function abortError() {
  const error = new Error('stream aborted unexpectedly')
  error.name = 'AbortError'
  return error
}

test('reconnect decision requires an active Run and an explicit nonterminal transport descriptor', () => {
  assert.equal(typeof shouldRetryAgentRuntimeConnection, 'function')
  const active = { active_run: { runtime_run_id: 'run-1' } }

  assert.equal(shouldRetryAgentRuntimeConnection({ category: 'transport', retryable: true, source: 'fetch' }, active), true)
  assert.equal(shouldRetryAgentRuntimeConnection(new TypeError('render failed'), active), false)
  assert.equal(shouldRetryAgentRuntimeConnection({ category: 'transport', retryable: true, terminal_status: 'failed' }, active), false)
  assert.equal(shouldRetryAgentRuntimeConnection({ category: 'transport', retryable: true, source: 'fetch' }, {}), false)
})

test('cursor is committed only after the event handler succeeds', () => {
  assert.equal(typeof handleRuntimeEventWithCursor, 'function')
  let cursor = ''
  const handlerError = new Error('entry patch failed')

  assert.throws(() => handleRuntimeEventWithCursor(
    () => { throw handlerError },
    () => { cursor = 'evt-1' }
  ), (error) => error === handlerError)
  assert.equal(cursor, '')

  handleRuntimeEventWithCursor(() => 'handled', () => { cursor = 'evt-1' })
  assert.equal(cursor, 'evt-1')
})

test('async handler rejection does not commit the cursor', async () => {
  let cursor = ''
  const handlerError = new Error('async entry patch failed')

  await assert.rejects(
    handleRuntimeEventWithCursor(
      async () => { throw handlerError },
      () => { cursor = 'evt-2' }
    ),
    (error) => error === handlerError
  )
  assert.equal(cursor, '')
})

test('follow guard stops stale Page Assistant loops after reset, replacement, or stop', () => {
  assert.equal(typeof isAgentRuntimeFollowCurrent, 'function')
  const current = { epoch: 3, currentEpoch: 3, sessionId: '7', currentSessionId: '7', stopping: false }

  assert.equal(isAgentRuntimeFollowCurrent(current), true)
  assert.equal(isAgentRuntimeFollowCurrent({ ...current, currentEpoch: 4 }), false)
  assert.equal(isAgentRuntimeFollowCurrent({ ...current, currentSessionId: '8' }), false)
  assert.equal(isAgentRuntimeFollowCurrent({ ...current, stopping: true }), false)
})

test('unexpected AbortError on the current active Run is normalized and refreshes Run policy', async () => {
  assert.equal(typeof resolveAgentRuntimeFollowError, 'function')
  let refreshCount = 0
  const activeSummary = { active_run: { runtime_run_id: 'run-1' } }
  const result = await resolveAgentRuntimeFollowError(abortError(), {
    followState: { epoch: 3, currentEpoch: 3, sessionId: '7', currentSessionId: '7', stopping: false },
    fallbackMessage: '页面助手实时连接已中断',
    previousSummary: activeSummary,
    refreshSummary: async () => {
      refreshCount += 1
      return activeSummary
    }
  })

  assert.equal(refreshCount, 1)
  assert.equal(result.silent, false)
  assert.equal(result.error.name, 'AgentRuntimeError')
  assert.equal(result.error.category, 'transport')
  assert.equal(result.error.source, 'fetch')
  assert.equal(result.error.retryable, true)
  assert.equal(result.summary, activeSummary)
  assert.equal(result.retry, true)
})

test('stop reset and session replacement AbortErrors exit without normalization or refresh', async () => {
  assert.equal(typeof resolveAgentRuntimeFollowError, 'function')
  const base = { epoch: 3, currentEpoch: 3, sessionId: '7', currentSessionId: '7', stopping: false }
  for (const followState of [
    { ...base, stopping: true },
    { ...base, currentEpoch: 4 },
    { ...base, currentSessionId: '8' }
  ]) {
    let refreshCount = 0
    const result = await resolveAgentRuntimeFollowError(abortError(), {
      followState,
      fallbackMessage: '页面助手实时连接已中断',
      previousSummary: { active_run: { runtime_run_id: 'run-1' } },
      refreshSummary: async () => {
        refreshCount += 1
        return null
      }
    })
    assert.equal(result.silent, true)
    assert.equal(result.error, null)
    assert.equal(refreshCount, 0)
  }
})
