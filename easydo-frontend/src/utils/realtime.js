/**
 * WebSocket Real-time Client
 * Handles WebSocket connections for receiving pipeline execution updates
 */

import { ElMessage } from 'element-plus'

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

    const wsURL = this.buildWebSocketURL(runID)
    
    try {
      this.ws = new WebSocket(wsURL)
      this.setupEventHandlers()
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
  setupEventHandlers() {
    this.ws.onopen = () => {
      console.log('WebSocket connected')
      this.isConnected = true
      this.reconnectAttempts = 0
      this.emit('connected', { runID: this.runID })
    }

    this.ws.onclose = (event) => {
      console.log('WebSocket closed:', event.code, event.reason)
      this.isConnected = false
      this.emit('disconnected', { runID: this.runID })
      
      // Attempt to reconnect if not a clean close
      if (event.code !== 1000 && this.reconnectAttempts < this.maxReconnectAttempts) {
        this.scheduleReconnect()
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

  /**
   * Handle connection errors
   */
  handleConnectionError(error) {
    this.emit('error', { error })
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
      reconnectAttempts: this.reconnectAttempts
    }
  }
}

// Singleton instance
const realtimeClient = new RealtimeClient()

export default realtimeClient
export { RealtimeClient }
