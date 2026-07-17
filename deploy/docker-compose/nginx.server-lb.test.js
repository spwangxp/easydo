import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const CONFIG_PATH = 'deploy/docker-compose/nginx.server-lb.conf'

function locationBlock(source, pattern) {
  const index = source.indexOf(pattern)
  if (index < 0) return ''
  const tail = source.slice(index)
  const end = tail.indexOf('\n    }\n')
  return end >= 0 ? tail.slice(0, end) : tail
}

test('server-lb disables proxy buffering for all AI agent stream endpoints', async () => {
  const source = await readFile(CONFIG_PATH, 'utf8')
  const sessionStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?sessions/.*/entries/stream$')
  const continuationStream = locationBlock(source, '^/api/ai/agent-chatbox/sessions/.*/continue/stream$')
  const actionStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?actions/.*/decision/stream$')
  const piApprovalStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?runs/.*/pi-approval/decision/stream$')

  assert.match(sessionStream, /proxy_buffering off;/, 'session stream must disable buffering')
  assert.match(sessionStream, /proxy_cache off;/, 'session stream must disable cache')
  assert.match(continuationStream, /proxy_buffering off;/, 'continuation stream must disable buffering')
  assert.match(actionStream, /proxy_buffering off;/, 'action stream must disable buffering')
  assert.match(piApprovalStream, /proxy_buffering off;/, 'pi approval stream must disable buffering')
  assert.match(piApprovalStream, /proxy_cache off;/, 'pi approval stream must disable cache')
})
