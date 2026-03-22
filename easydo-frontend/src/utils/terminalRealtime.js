const terminalEventTypes = new Set([
  'terminal_ready',
  'terminal_output',
  'terminal_error',
  'terminal_closed'
])

class TerminalRealtimeClient {
  constructor(sessionId = '') {
    this.sessionId = sessionId
    this.ws = null
    this.isConnected = false
    this.handlers = new Map()
    this.token = typeof localStorage !== 'undefined' ? localStorage.getItem('token') || '' : ''
    this.explicitDisconnect = false
    this.reconnectTimer = null
    this.heartbeatTimer = null
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = 5
  }

  setSessionId(sessionId) {
    this.sessionId = sessionId || ''
  }

  buildWebSocketURL() {
    const sessionId = encodeURIComponent(this.sessionId || '')
    const token = encodeURIComponent(this.token || '')
    return `${this.getBaseURL()}/ws/frontend/terminal?session_id=${sessionId}&token=${token}`
  }

  getBaseURL() {
    if (typeof window === 'undefined') {
      return 'ws://127.0.0.1:8080'
    }

    const hostname = window.location.hostname
    const wsPort = import.meta.env?.VITE_WS_PORT || '8080'
    const isLocalDevHost = hostname === 'localhost' || hostname === '127.0.0.1'

    if (import.meta.env?.DEV || isLocalDevHost) {
      return `ws://${hostname}:${wsPort}`
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${protocol}//${window.location.host}`
  }

  connect({ preserveReconnect = false } = {}) {
    if (!this.sessionId) {
      throw new Error('missing terminal session id')
    }

    if (this.ws && (this.ws.readyState === 0 || this.ws.readyState === 1)) {
      return this.ws
    }

    if (!preserveReconnect) {
      this.explicitDisconnect = false
    }
    this.clearReconnectTimer()
    this.token = typeof localStorage !== 'undefined' ? localStorage.getItem('token') || '' : ''
    if (!this.token) {
      this.explicitDisconnect = true
      this.isConnected = false
      this.emit('error', { session_id: this.sessionId, error: new Error('missing terminal token') })
      return null
    }
    this.ws = new WebSocket(this.buildWebSocketURL())
    this.setupEventHandlers()
    return this.ws
  }

  setupEventHandlers() {
    if (!this.ws) return

    this.ws.onopen = () => {
      this.isConnected = true
      this.reconnectAttempts = 0
      this.startHeartbeat()
      this.emit('connected', { session_id: this.sessionId })
    }

    this.ws.onclose = (event) => {
      this.isConnected = false
      this.stopHeartbeat()
      this.ws = null
      this.emit('disconnected', {
        session_id: this.sessionId,
        code: event?.code,
        reason: event?.reason || ''
      })
      if (!this.explicitDisconnect && event?.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.scheduleReconnect()
      }
    }

    this.ws.onerror = (error) => {
      this.emit('error', { session_id: this.sessionId, error })
    }

    this.ws.onmessage = (event) => {
      this.handleMessage(event.data)
    }
  }

  handleMessage(raw) {
    try {
      const message = JSON.parse(raw)
      if (!terminalEventTypes.has(message?.type)) {
        return
      }
      if (message.type === 'terminal_closed') {
        this.explicitDisconnect = true
        this.clearReconnectTimer()
        this.stopHeartbeat()
      }
      this.emit(message.type, message.payload || {})
    } catch {
      this.emit('error', { session_id: this.sessionId, error: new Error('invalid terminal message') })
    }
  }

  send(type, payload = {}) {
    if (!this.ws || this.ws.readyState !== 1) {
      return false
    }

    this.ws.send(JSON.stringify({
      type,
      payload: {
        session_id: this.sessionId,
        ...payload
      }
    }))
    return true
  }

  sendInput(data) {
    return this.send('terminal_input', { data })
  }

  sendResize({ cols, rows }) {
    return this.send('terminal_resize', { cols, rows })
  }

  sendClose(reason = 'frontend_closed') {
    return this.send('terminal_close', { reason })
  }

  sendRootSwitch() {
    return this.send('terminal_root_switch')
  }

  disconnect(code = 1000, reason = 'client_disconnect') {
    this.explicitDisconnect = true
    this.clearReconnectTimer()
    this.stopHeartbeat()
    if (!this.ws) {
      this.isConnected = false
      return
    }

    const currentSocket = this.ws
    this.ws = null
    this.isConnected = false
    currentSocket.close(code, reason)
  }

  startHeartbeat() {
    this.stopHeartbeat()
    this.heartbeatTimer = setInterval(() => {
      this.send('terminal_ping', { timestamp: Date.now() })
    }, 20000)
  }

  stopHeartbeat() {
    if (!this.heartbeatTimer) {
      return
    }
    clearInterval(this.heartbeatTimer)
    this.heartbeatTimer = null
  }

  scheduleReconnect() {
    if (this.reconnectTimer || this.explicitDisconnect || !this.sessionId) {
      return
    }
    const nextToken = typeof localStorage !== 'undefined' ? localStorage.getItem('token') || '' : ''
    if (!nextToken) {
      this.explicitDisconnect = true
      return
    }
    this.emit('reconnecting', { session_id: this.sessionId })
    this.reconnectAttempts += 1
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.explicitDisconnect) {
        return
      }
      this.connect({ preserveReconnect: true })
    }, 1000)
  }

  clearReconnectTimer() {
    if (!this.reconnectTimer) {
      return
    }
    clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  on(event, handler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, [])
    }
    this.handlers.get(event).push(handler)
  }

  off(event, handler) {
    const currentHandlers = this.handlers.get(event) || []
    this.handlers.set(event, currentHandlers.filter(item => item !== handler))
  }

  emit(event, payload) {
    const currentHandlers = this.handlers.get(event) || []
    currentHandlers.forEach(handler => {
      try {
        handler(payload)
      } catch (error) {
        console.error(`terminal realtime handler error for ${event}:`, error)
      }
    })
  }

  getStatus() {
    return {
      connected: this.isConnected,
      sessionId: this.sessionId,
      readyState: this.ws?.readyState ?? 3
    }
  }
}

export { TerminalRealtimeClient }
