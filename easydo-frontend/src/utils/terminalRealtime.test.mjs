import assert from 'node:assert/strict'

let lastSocket = null
let timeoutCalls = 0
let scheduledTimeouts = []
const originalSetTimeout = global.setTimeout
const originalClearTimeout = global.clearTimeout

global.localStorage = {
  getItem(key) {
    if (key === 'token') return 'test-terminal-token'
    return null
  }
}

global.window = {
  location: {
    hostname: 'localhost',
    host: 'localhost:3000',
    protocol: 'http:'
  }
}

global.WebSocket = class MockWebSocket {
  constructor(url) {
    this.url = url
    this.sent = []
    this.closed = null
    this.readyState = 1
    lastSocket = this
  }

  send(payload) {
    this.sent.push(payload)
  }

  close(code, reason) {
    this.closed = { code, reason }
  }
}

global.setTimeout = (handler, delay) => {
  timeoutCalls += 1
  const timer = { handler, delay, mocked: true }
  scheduledTimeouts.push(timer)
  return timer
}

global.clearTimeout = () => {}

const { TerminalRealtimeClient } = await import('./terminalRealtime.js')

{
  const client = new TerminalRealtimeClient('session-alpha')
  assert.equal(
    client.buildWebSocketURL(),
    'ws://localhost:8080/ws/frontend/terminal?session_id=session-alpha&token=test-terminal-token'
  )
}

{
  const client = new TerminalRealtimeClient('session-bravo')
  const events = []

  client.on('terminal_ready', payload => events.push({ type: 'ready', payload }))
  client.on('terminal_output', payload => events.push({ type: 'output', payload }))
  client.on('terminal_closed', payload => events.push({ type: 'closed', payload }))

  client.connect()
  assert.equal(lastSocket.url, 'ws://localhost:8080/ws/frontend/terminal?session_id=session-bravo&token=test-terminal-token')

  lastSocket.onopen()
  assert.equal(client.getStatus().connected, true)

  lastSocket.onmessage({ data: JSON.stringify({ type: 'terminal_ready', payload: { session_id: 'session-bravo' } }) })
  lastSocket.onmessage({ data: JSON.stringify({ type: 'terminal_output', payload: { session_id: 'session-bravo', data: 'pwd\n' } }) })
  lastSocket.onmessage({ data: JSON.stringify({ type: 'terminal_closed', payload: { session_id: 'session-bravo', reason: 'user_closed' } }) })

  assert.deepEqual(events, [
    { type: 'ready', payload: { session_id: 'session-bravo' } },
    { type: 'output', payload: { session_id: 'session-bravo', data: 'pwd\n' } },
    { type: 'closed', payload: { session_id: 'session-bravo', reason: 'user_closed' } }
  ])

  client.sendInput('ls -la\n')
  client.sendResize({ cols: 160, rows: 48 })
  client.sendClose('user_closed')

  assert.deepEqual(lastSocket.sent.map(item => JSON.parse(item)), [
    { type: 'terminal_input', payload: { session_id: 'session-bravo', data: 'ls -la\n' } },
    { type: 'terminal_resize', payload: { session_id: 'session-bravo', cols: 160, rows: 48 } },
    { type: 'terminal_close', payload: { session_id: 'session-bravo', reason: 'user_closed' } }
  ])
}

{
  const originalGetItem = global.localStorage.getItem
  global.localStorage.getItem = () => null
  lastSocket = null

  const client = new TerminalRealtimeClient('session-charlie')
  const result = client.connect()

  assert.equal(result, null)
  assert.equal(lastSocket, null)

  global.localStorage.getItem = originalGetItem
}

{
  timeoutCalls = 0
  scheduledTimeouts = []
  const client = new TerminalRealtimeClient('session-delta')
  client.connect()
  lastSocket.onopen()
  lastSocket.onmessage({ data: JSON.stringify({ type: 'terminal_closed', payload: { session_id: 'session-delta', reason: 'tab_closed' } }) })
  lastSocket.onclose({ code: 1006, reason: '' })

  assert.equal(timeoutCalls, 0)
}

{
  timeoutCalls = 0
  scheduledTimeouts = []
  const client = new TerminalRealtimeClient('session-echo')
  client.maxReconnectAttempts = 2

  client.connect()
  lastSocket.onclose({ code: 1006, reason: '' })
  assert.equal(timeoutCalls, 1)

  scheduledTimeouts.shift().handler()
  lastSocket.onclose({ code: 1006, reason: '' })
  assert.equal(timeoutCalls, 2)

  scheduledTimeouts.shift().handler()
  lastSocket.onclose({ code: 1006, reason: '' })
  assert.equal(timeoutCalls, 2)
}

global.setTimeout = originalSetTimeout
global.clearTimeout = originalClearTimeout

console.log('terminal realtime tests passed')
