/**
 * WebSocket Real-time Client
 * Handles WebSocket connections for receiving pipeline execution updates
 */

import { ElMessage } from 'element-plus'

export const resolveRealtimeBaseURL = ({
  dev = false,
  wsPort = '8080',
  location = {}
} = {}) => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const hostname = location.hostname || 'localhost'
  const host = location.host || hostname

  if (dev) {
    return `${protocol}//${hostname}:${wsPort}`
  }

  return `${protocol}//${host}`
}

// RealtimeClient drives the pipeline detail page's transport-level state machine.
//
// The backend can fail over between replicas, and the browser can lose WS while
// HTTP still works. Because of that, the UI distinguishes:
// - reconnecting: we still expect WS recovery soon
// - polling: WS degraded long enough that the page must rely on HTTP refresh
// - recovered: WS returned after degraded mode, so the UI can clear fallback
//   indicators and resync realtime behavior
class RealtimeClient {
  constructor() {
    this.ws = null
    this.runID = null
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = 5
    this.reconnectDelay = 3000
    this.messageHandlers = new Map()
    this.isConnected = false
    // Tracks if onopen ever fired for the current run — used to distinguish
    // initial connection failures (pre-connect errors) from true disconnects.
    this.hasEverConnected = false
    this.token = localStorage.getItem('token')
    this.connectionState = 'idle'
    this.connectionSerial = 0
    this.activeConnectionSerial = 0
    this.reconnectTimer = null
  }

  /**
   * Connect to WebSocket server
   * @param {string} runID - Pipeline run ID to subscribe to
   */
  connect(runID) {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    if (this.ws) {
      this.disconnect()
    }

    this.runID = runID
    const socketRunID = runID
    this.token = localStorage.getItem('token')
    const previousState = this.connectionState
    this.connectionState = this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting'
    const connectionSerial = ++this.connectionSerial
    this.activeConnectionSerial = connectionSerial

    const wsURL = this.buildWebSocketURL(runID)
    
    try {
      const socket = new WebSocket(wsURL)
      this.ws = socket
      this.setupEventHandlers(socket, {
        previousState,
        runID: socketRunID,
        connectionSerial
      })
    } catch (error) {
      console.error('WebSocket connection failed:', error)
      this.handleConnectionError(error, { runID: socketRunID })
    }
  }

  /**
   * Build WebSocket URL with query parameters
   */
  buildWebSocketURL(runID) {
    const baseURL = this.getBaseURL()
    const token = this.token || ''
    return `${baseURL}/ws/frontend/pipeline?run_id=${runID}&token=${token}`
  }

  /**
   * Get WebSocket base URL based on current environment
   */
  getBaseURL() {
    return resolveRealtimeBaseURL({
      dev: import.meta.env.DEV,
      wsPort: import.meta.env.VITE_WS_PORT || '8080',
      location: window.location
    })
  }

  /**
   * Setup WebSocket event handlers
   */
  // setupEventHandlers maps raw browser WS lifecycle events into the frontend's
  // higher-level realtime states.
  //
  // `previousState` matters because a successful `onopen` after polling or
  // reconnecting is semantically different from the initial first connection.
  setupEventHandlers(socket, context = {}) {
    const {
      previousState = 'idle',
      runID: socketRunID,
      connectionSerial
    } = context

    socket.onopen = () => {
      if (!this.isActiveSocket(socket, connectionSerial)) return
      console.log('WebSocket connected')
      this.isConnected = true
      this.hasEverConnected = true
      this.reconnectAttempts = 0
      const recovered = previousState === 'reconnecting' || previousState === 'polling'
      this.connectionState = 'connected'
      if (recovered) {
        this.emit('recovered', { runID: socketRunID })
      }
      this.emit('connected', { runID: socketRunID })
    }

    socket.onclose = (event) => {
      if (!this.isActiveSocket(socket, connectionSerial)) return
      console.log('WebSocket closed:', event.code, event.reason)
      this.isConnected = false
      this.emit('disconnected', { runID: socketRunID })
      
      if (event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.connectionState = 'reconnecting'
        this.emit('reconnecting', {
          runID: socketRunID,
          attempt: this.reconnectAttempts + 1,
          maxAttempts: this.maxReconnectAttempts
        })
        this.scheduleReconnect(socketRunID, connectionSerial)
        return
      }

      if (event.code !== 1000) {
        this.enterPollingMode()
      } else {
        this.connectionState = 'idle'
      }
    }

    socket.onerror = (error) => {
      if (!this.isActiveSocket(socket, connectionSerial)) return
      console.error('WebSocket error:', error)
      this.handleConnectionError(error, { runID: socketRunID })
    }

    socket.onmessage = (event) => {
      if (!this.isActiveSocket(socket, connectionSerial)) return
      this.handleMessage(event.data)
    }
  }

  isActiveSocket(socket, connectionSerial) {
    return this.ws === socket && this.activeConnectionSerial === connectionSerial
  }

  /**
   * Handle incoming WebSocket message
   */
  handleMessage(data) {
    try {
      const message = JSON.parse(data)
      
      switch (message.type) {
        case 'task_status':
          this.handleTaskStatus(message.payload)
          break
        case 'task_log':
        case 'task_log_stream':
          this.handleTaskLog(message.payload)
          break
        case 'run_status':
          this.handleRunStatus(message.payload)
          break
        case 'heartbeat_ack':
          console.log('Heartbeat acknowledged')
          break
        default:
          console.log('Unknown message type:', message.type)
      }
    } catch (error) {
      console.error('Failed to parse WebSocket message:', error)
    }
  }

  /**
   * Handle task status update
   */
  handleTaskStatus(payload) {
    this.emit('task_status', payload)
  }

  /**
   * Handle task log update
   */
  handleTaskLog(payload) {
    this.emit('task_log', payload)
  }

  /**
   * Handle pipeline run status update
   */
  handleRunStatus(payload) {
    this.emit('run_status', payload)
  }

  /**
   * Schedule reconnection attempt
   */
  // scheduleReconnect uses a bounded linear backoff. Once attempts are exhausted,
  // the caller degrades to polling instead of leaving the page stuck in a
  // permanent reconnect loop.
  scheduleReconnect(runID, connectionSerial) {
    this.reconnectAttempts++
    const delay = this.reconnectDelay * this.reconnectAttempts
    
    console.log(`Scheduling reconnection attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts} in ${delay}ms`)
    
    this.reconnectTimer = setTimeout(() => {
      if (this.activeConnectionSerial === connectionSerial && this.runID && Number(this.runID) === Number(runID)) {
        this.connect(runID)
      }
      this.reconnectTimer = null
    }, delay)
  }

  // enterPollingMode is the explicit degraded mode for "HTTP still works, WS is
  // not trustworthy right now". Detail pages listen for this and continue state
  // refresh through polling until a later WS recovery emits `recovered`.
  enterPollingMode() {
    if (this.connectionState === 'polling') {
      return
    }
    this.connectionState = 'polling'
    this.emit('polling', { runID: this.runID })
  }

  /**
   * Handle connection errors
   */
  handleConnectionError(error, context = {}) {
    const { runID = this.runID } = context
    this.emit('error', { error, runID })
    if (!this.isConnected && this.reconnectAttempts >= this.maxReconnectAttempts) {
      this.enterPollingMode()
    }
    // Only show disconnect toast when a connection that was previously
    // established is lost. Pre-connect errors (hasEverConnected === false)
    // should not trigger the toast.
    if (this.hasEverConnected) {
      ElMessage.error('实时连接断开，正在尝试重连...')
    }
  }

  /**
   * Disconnect from WebSocket server
   */
  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.close(1000, 'Client disconnect')
      this.ws = null
    }
    this.isConnected = false
    this.hasEverConnected = false
    this.runID = null
    this.reconnectAttempts = 0
    this.connectionState = 'idle'
    this.activeConnectionSerial = 0
  }

  /**
   * Subscribe to event type
   */
  on(event, handler) {
    if (!this.messageHandlers.has(event)) {
      this.messageHandlers.set(event, [])
    }
    this.messageHandlers.get(event).push(handler)
  }

  /**
   * Unsubscribe from event type
   */
  off(event, handler) {
    if (this.messageHandlers.has(event)) {
      const handlers = this.messageHandlers.get(event)
      const index = handlers.indexOf(handler)
      if (index > -1) {
        handlers.splice(index, 1)
      }
    }
  }

  /**
   * Emit event to handlers
   */
  emit(event, data) {
    if (this.messageHandlers.has(event)) {
      this.messageHandlers.get(event).forEach(handler => {
        try {
          handler(data)
        } catch (error) {
          console.error(`Error in event handler for ${event}:`, error)
        }
      })
    }
  }

  /**
   * Get connection status
   */
  getStatus() {
    return {
      connected: this.isConnected,
      runID: this.runID,
      reconnectAttempts: this.reconnectAttempts,
      state: this.connectionState
    }
  }
}

// Singleton instance
const realtimeClient = new RealtimeClient()

export default realtimeClient
export { RealtimeClient }
