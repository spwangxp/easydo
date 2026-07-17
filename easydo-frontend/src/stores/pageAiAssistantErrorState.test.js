import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('page assistant reconnect state shares normalized terminal and retry behavior', async () => {
  const source = await readFile(new URL('./pageAiAssistant.js', import.meta.url), 'utf8')

  assert.match(source, /agentRuntimeErrorMessage/)
  assert.match(source, /shouldRetryAgentRuntimeConnection/)
  assert.match(source, /handleRuntimeEventWithCursor/)
  assert.match(source, /const epoch = \+\+streamEpoch/)
  assert.match(source, /resolveAgentRuntimeFollowError\(err/)
  assert.match(source, /normalizeAgentRuntimeError/)
  assert.match(source, /event === 'error'/)
  assert.match(source, /event === 'stream_protocol_error'/)
  assert.match(source, /shouldRetryAgentRuntimeConnection\(normalized,\s*summary\)/)
  assert.doesNotMatch(source, /err\?\.response\?\.data\?\.message/)
  assert.doesNotMatch(source, /err\?\.name === 'AbortError' \? '已停止生成'/)
})

test('page assistant maintains and reuses a per-Run SSE cursor', async () => {
  const source = await readFile(new URL('./pageAiAssistant.js', import.meta.url), 'utf8')

  assert.match(source, /const lastEventIdByRun = sessionController\.lastEventIdByRun/)
  assert.match(source, /lastEventIdByRun\.value\[runtimeRunId\] \|\| ''/)
  assert.match(source, /sessionController\.handleHeartbeat\(data\)/)
  assert.match(source, /sessionController\.handleRuntimeEventPayload\(/)
  assert.match(source, /sessionController\.reset\('idle'\)/)
})

test('page assistant aborts the active reader before awaiting cancel', async () => {
  const source = await readFile(new URL('./pageAiAssistant.js', import.meta.url), 'utf8')
  const stopSource = source.slice(source.indexOf('async function stopGeneration'), source.indexOf('async function continueAction'))

  assert.ok(stopSource.indexOf('activeStreamController.value?.abort()') < stopSource.indexOf('await cancelPageAssistantSession'))
})
