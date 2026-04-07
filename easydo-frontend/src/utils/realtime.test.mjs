import assert from 'node:assert/strict'

global.localStorage = {
  getItem(key) {
    if (key === 'token') return 'test-token'
    return null
  }
}

const { RealtimeClient } = await import('./realtime.js')
const { resolveRealtimeBaseURL } = await import('./realtime.js')

{
  const baseURL = resolveRealtimeBaseURL({
    dev: false,
    wsPort: '8080',
    location: {
      hostname: 'localhost',
      host: 'localhost:8088',
      protocol: 'http:'
    }
  })
  assert.equal(baseURL, 'ws://localhost:8088')
}

{
  const baseURL = resolveRealtimeBaseURL({
    dev: true,
    wsPort: '5173',
    location: {
      hostname: '127.0.0.1',
      host: '127.0.0.1:5173',
      protocol: 'http:'
    }
  })
  assert.equal(baseURL, 'ws://127.0.0.1:5173')
}

{
  const client = new RealtimeClient()
  const socket = {}
  client.runID = '42'
  client.ws = socket
  client.activeConnectionSerial = 1
  client.maxReconnectAttempts = 2
  client.reconnectAttempts = 0
  let scheduled = false
  const events = []
  client.scheduleReconnect = () => {
    scheduled = true
  }
  client.on('reconnecting', () => events.push('reconnecting'))
  client.setupEventHandlers(socket, { previousState: 'connected', runID: '42', connectionSerial: 1 })
  socket.onclose({ code: 1006, reason: 'abnormal' })
  assert.equal(client.connectionState, 'reconnecting')
  assert.equal(scheduled, true)
  assert.deepEqual(events, ['reconnecting'])
}

{
  const client = new RealtimeClient()
  const socket = {}
  client.runID = '42'
  client.ws = socket
  client.activeConnectionSerial = 1
  client.maxReconnectAttempts = 1
  client.reconnectAttempts = 1
  const events = []
  client.on('polling', () => events.push('polling'))
  client.setupEventHandlers(socket, { previousState: 'connected', runID: '42', connectionSerial: 1 })
  socket.onclose({ code: 1006, reason: 'abnormal' })
  assert.equal(client.connectionState, 'polling')
  assert.deepEqual(events, ['polling'])
}

{
  const client = new RealtimeClient()
  const socket = {}
  client.runID = '42'
  client.ws = socket
  client.activeConnectionSerial = 1
  const events = []
  client.on('recovered', () => events.push('recovered'))
  client.on('connected', () => events.push('connected'))
  client.setupEventHandlers(socket, { previousState: 'polling', runID: '42', connectionSerial: 1 })
  socket.onopen()
  assert.equal(client.connectionState, 'connected')
  assert.deepEqual(events, ['recovered', 'connected'])
}

console.log('realtime client tests passed')
