import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

test('agent chatbox reconnect state uses normalized errors and stops on terminal failures', async () => {
  const source = await readFile(new URL('./agentChatbox.js', import.meta.url), 'utf8')

  assert.match(source, /agentRuntimeErrorMessage/)
  assert.match(source, /shouldRetryAgentRuntimeConnection/)
  assert.match(source, /handleRuntimeEventWithCursor/)
  assert.match(source, /const epoch = \+\+streamEpoch/)
  assert.match(source, /resolveAgentRuntimeFollowError\(err/)
  assert.match(source, /normalizeAgentRuntimeError/)
  assert.match(source, /event === 'error' \|\| event === 'stream_protocol_error'/)
  assert.match(source, /shouldRetryAgentRuntimeConnection\(normalized,\s*summary\)/)
  assert.match(source, /connectionState\.value = 'terminal'/)
  assert.match(source, /session\.value\?\.active_run \|\| activeRuntimeRun\.value/)
  assert.doesNotMatch(source, /err\?\.response\?\.data\?\.message/)
  assert.doesNotMatch(source, /err\?\.name === 'AbortError' \? '已停止生成'/)
})
