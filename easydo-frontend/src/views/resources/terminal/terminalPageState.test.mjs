import assert from 'node:assert/strict'

const {
  buildSessionLabel,
  closeTerminalSessionsOnPageUnload,
  createTerminalLaunchPath,
  normalizeTerminalSession,
  pickNextActiveSessionId,
  upsertTerminalSession
} = await import('./terminalPageState.js')

const resourcesById = {
  7: { id: 7, name: 'alpha-vm', endpoint: '10.0.0.7:22' },
  8: { id: 8, name: 'beta-vm', endpoint: '10.0.0.8:22' }
}

const firstSession = normalizeTerminalSession({
  session_id: 'session-1',
  resource_id: 7,
  endpoint: '10.0.0.7:22',
  status: 'active',
  created_at: '2026-03-22T08:00:00Z'
}, resourcesById)

const secondSession = normalizeTerminalSession({
  session_id: 'session-2',
  resource_id: 7,
  endpoint: '10.0.0.7:22',
  status: 'active',
  created_at: '2026-03-22T08:01:00Z'
}, resourcesById)

const thirdSession = normalizeTerminalSession({
  session_id: 'session-3',
  resource_id: 8,
  endpoint: '10.0.0.8:22',
  status: 'active',
  created_at: '2026-03-22T08:02:00Z'
}, resourcesById)

let sessions = []
sessions = upsertTerminalSession(sessions, firstSession)
sessions = upsertTerminalSession(sessions, secondSession)
sessions = upsertTerminalSession(sessions, thirdSession)

assert.equal(sessions.length, 3)
assert.deepEqual(sessions.map(item => item.sessionId), ['session-3', 'session-2', 'session-1'])
assert.equal(buildSessionLabel(firstSession, sessions), 'alpha-vm · 会话 1')
assert.equal(buildSessionLabel(secondSession, sessions), 'alpha-vm · 会话 2')
assert.equal(buildSessionLabel(thirdSession, sessions), 'beta-vm')
assert.equal(pickNextActiveSessionId(sessions, 'session-3', 'session-3'), 'session-2')
assert.equal(pickNextActiveSessionId(sessions, 'session-2', 'session-2'), 'session-3')
assert.equal(pickNextActiveSessionId(sessions, 'session-1', 'session-3'), 'session-3')
assert.equal(createTerminalLaunchPath(9), '/terminal?resourceId=9')
assert.equal(createTerminalLaunchPath(), '/terminal')

const fetchCalls = []
const closedCount = closeTerminalSessionsOnPageUnload([
  firstSession,
  { ...secondSession, status: 'closed' },
  thirdSession
], {
  token: 'test-token',
  workspaceId: '1',
  origin: 'http://localhost',
  fetchImpl: (url, options) => {
    fetchCalls.push({ url, options })
    return Promise.resolve({ ok: true })
  }
})

assert.equal(closedCount, 2)
assert.deepEqual(fetchCalls.map(call => call.url), [
  'http://localhost/api/resources/7/terminal-sessions/session-1/close',
  'http://localhost/api/resources/8/terminal-sessions/session-3/close'
])
assert.equal(fetchCalls[0].options.keepalive, true)
assert.equal(fetchCalls[0].options.method, 'POST')
assert.equal(fetchCalls[0].options.headers.Authorization, 'Bearer test-token')
assert.equal(fetchCalls[0].options.headers['X-Workspace-ID'], '1')
assert.deepEqual(JSON.parse(fetchCalls[0].options.body), { reason: 'tab_closed' })

console.log('terminal page state tests passed')
