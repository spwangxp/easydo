/**
 * WebSocket Real-time Client
 * Handles WebSocket connections for receiving pipeline execution updates
 */

import { ElMessage } from 'element-plus'

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
    this.token = localStorage.getItem('token')
    this.connectionState = 'idle'
  }

  /**
   * Connect to WebSocket server
   * @param {string} runID - Pipeline run ID to subscribe to
   */
  connect(runID) {
    if (this.ws) {
      this.disconnect()
    }

    this.runID = runID
    this.token = localStorage.getItem('token')
    const previousState = this.connectionState
    this.connectionState = this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting'

    const wsURL = this.buildWebSocketURL(runID)
    
    try {
      this.ws = new WebSocket(wsURL)
      this.setupEventHandlers(previousState)
    } catch (error) {
      console.error('WebSocket connection failed:', error)
      this.handleConnectionError(error)
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
    const hostname = window.location.hostname
    const port = import.meta.env.VITE_WS_PORT || '8080'
    
    // Check if running in development
    if (import.meta.env.DEV || hostname === 'localhost') {
      return `ws://${hostname}:${port}`
    }
    
    // Production: use current host
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${protocol}//${window.location.host}`
  }

  /**
   * Setup WebSocket event handlers
   */
  // setupEventHandlers maps raw browser WS lifecycle events into the frontend's
  // higher-level realtime states.
  //
  // `previousState` matters because a successful `onopen` after polling or
  // reconnecting is semantically different from the initial first connection.
  setupEventHandlers(previousState = 'idle') {
    this.ws.onopen = () => {
      console.log('WebSocket connected')
      this.isConnected = true
      this.reconnectAttempts = 0
      const recovered = previousState === 'reconnecting' || previousState === 'polling'
      this.connectionState = 'connected'
      if (recovered) {
        this.emit('recovered', { runID: this.runID })
      }
      this.emit('connected', { runID: this.runID })
    }

    this.ws.onclose = (event) => {
      console.log('WebSocket closed:', event.code, event.reason)
      this.isConnected = false
      this.emit('disconnected', { runID: this.runID })
      
      if (event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.connectionState = 'reconnecting'
        this.emit('reconnecting', {
          runID: this.runID,
          attempt: this.reconnectAttempts + 1,
          maxAttempts: this.maxReconnectAttempts
        })
        this.scheduleReconnect()
        return
      }

      if (event.code !== 1000) {
        this.enterPollingMode()
      } else {
        this.connectionState = 'idle'
      }
    }

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      this.handleConnectionError(error)
    }

    this.ws.onmessage = (event) => {
      this.handleMessage(event.data)
    }
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
  scheduleReconnect() {
    this.reconnectAttempts++
    const delay = this.reconnectDelay * this.reconnectAttempts
    
    console.log(`Scheduling reconnection attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts} in ${delay}ms`)
    
    setTimeout(() => {
      if (this.runID) {
        this.connect(this.runID)
      }
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
  handleConnectionError(error) {
    this.emit('error', { error })
    if (!this.isConnected && this.reconnectAttempts >= this.maxReconnectAttempts) {
      this.enterPollingMode()
    }
    ElMessage.error('实时连接断开，正在尝试重连...')
  }

  /**
   * Disconnect from WebSocket server
   */
  disconnect() {
    if (this.ws) {
      this.ws.close(1000, 'Client disconnect')
      this.ws = null
    }
    this.isConnected = false
    this.runID = null
    this.reconnectAttempts = 0
    this.connectionState = 'idle'
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
