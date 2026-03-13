import assert from 'node:assert/strict'

global.localStorage = {
  getItem(key) {
    if (key === 'token') return 'test-token'
    return null
  }
}

const { RealtimeClient } = await import('./realtime.js')

{
  const client = new RealtimeClient()
  client.runID = '42'
  client.ws = {}
  client.maxReconnectAttempts = 2
  client.reconnectAttempts = 0
  let scheduled = false
  const events = []
  client.scheduleReconnect = () => {
    scheduled = true
  }
  client.on('reconnecting', () => events.push('reconnecting'))
  client.setupEventHandlers('connected')
  client.ws.onclose({ code: 1006, reason: 'abnormal' })
  assert.equal(client.connectionState, 'reconnecting')
  assert.equal(scheduled, true)
  assert.deepEqual(events, ['reconnecting'])
}

{
  const client = new RealtimeClient()
  client.runID = '42'
  client.ws = {}
  client.maxReconnectAttempts = 1
  client.reconnectAttempts = 1
  const events = []
  client.on('polling', () => events.push('polling'))
  client.setupEventHandlers('connected')
  client.ws.onclose({ code: 1006, reason: 'abnormal' })
  assert.equal(client.connectionState, 'polling')
  assert.deepEqual(events, ['polling'])
}

{
  const client = new RealtimeClient()
  client.runID = '42'
  client.ws = {}
  const events = []
  client.on('recovered', () => events.push('recovered'))
  client.on('connected', () => events.push('connected'))
  client.setupEventHandlers('polling')
  client.ws.onopen()
  assert.equal(client.connectionState, 'connected')
  assert.deepEqual(events, ['recovered', 'connected'])
}

console.log('realtime client tests passed')
