import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const CONFIG_PATHS = [
  'deploy/docker-compose/frontend/nginx.conf',
  'easydo-frontend/nginx/default.conf'
]

function locationBlock(source, pattern) {
  const index = source.indexOf(pattern)
  if (index < 0) return ''
  const tail = source.slice(index)
  const end = tail.indexOf('\n    }\n')
  return end >= 0 ? tail.slice(0, end) : tail
}

test('frontend nginx disables proxy buffering for all AI agent stream endpoints', async () => {
  for (const path of CONFIG_PATHS) {
    const source = await readFile(path, 'utf8')
    const sessionStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?sessions/.*/entries/stream$')
    const continuationStream = locationBlock(source, '^/api/ai/agent-chatbox/sessions/.*/continue/stream$')
    const actionStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?actions/.*/decision/stream$')
    const piApprovalStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?runs/.*/pi-approval/decision/stream$')
    const runEventStream = locationBlock(source, '^/api/ai/(agent-chatbox/)?runs/.*/events/stream$')

    assert.match(sessionStream, /proxy_buffering off;/, `${path} session stream must disable buffering`)
    assert.match(sessionStream, /proxy_cache off;/, `${path} session stream must disable cache`)
    assert.match(sessionStream, /proxy_read_timeout 3600s;/, `${path} session stream must allow long-running agents`)
    assert.match(continuationStream, /proxy_buffering off;/, `${path} continuation stream must disable buffering`)
    assert.match(continuationStream, /proxy_read_timeout 3600s;/, `${path} continuation stream must allow long-running agents`)
    assert.match(actionStream, /proxy_buffering off;/, `${path} action stream must disable buffering`)
    assert.match(actionStream, /proxy_read_timeout 3600s;/, `${path} action stream must allow long-running agents`)
    assert.match(piApprovalStream, /proxy_buffering off;/, `${path} pi approval stream must disable buffering`)
    assert.match(piApprovalStream, /proxy_cache off;/, `${path} pi approval stream must disable cache`)
    assert.match(piApprovalStream, /proxy_read_timeout 3600s;/, `${path} pi approval stream must allow long-running agents`)
    assert.match(runEventStream, /proxy_buffering off;/, `${path} run event stream must disable buffering`)
    assert.match(runEventStream, /proxy_cache off;/, `${path} run event stream must disable cache`)
    assert.match(runEventStream, /proxy_read_timeout 3600s;/, `${path} run event stream must allow long-running agents`)
  }
})
