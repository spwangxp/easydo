package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"easydo-agent/internal/task"
	"github.com/gorilla/websocket"
	"k8s.io/klog/v2"
)

// TaskHandler interface for handling incoming tasks
type TaskHandler interface {
	HandleTaskAssign(msg *TaskAssignMessage) error
	HandlePullTaskNow(taskID uint64, dispatchToken string) error
	HandlePipelineAssign(msg *PipelineAssignMessage) error
	HandleTaskCancel(taskID uint64) error
}

// PipelineAssignMessage represents a pipeline assignment message from server
type PipelineAssignMessage struct {
	RunID       uint64         `json:"run_id"`
	Config      PipelineConfig `json:"config"`
	AgentConfig AgentConfig    `json:"agent_config"`
}

// TaskAssignMessage represents a concrete task assignment from server.
type TaskAssignMessage struct {
	Task      TaskAssignPayload `json:"task"`
	Timestamp int64             `json:"timestamp"`
}

// TaskAssignPayload mirrors the task fields required by task executor.
type TaskAssignPayload struct {
	ID              uint64 `json:"id"`
	AgentID         uint64 `json:"agent_id"`
	PipelineRunID   uint64 `json:"pipeline_run_id"`
	NodeID          string `json:"node_id"`
	TaskType        string `json:"task_type"`
	Name            string `json:"name"`
	Params          string `json:"params"`
	Script          string `json:"script"`
	WorkDir         string `json:"work_dir"`
	EnvVars         string `json:"env_vars"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Timeout         int    `json:"timeout"`
	RetryCount      int    `json:"retry_count"`
	MaxRetries      int    `json:"max_retries"`
	DispatchToken   string `json:"dispatch_token"`
	DispatchAttempt int    `json:"dispatch_attempt"`
}

// PipelineConfig represents the pipeline configuration
type PipelineConfig struct {
	Version     string               `json:"version"`
	Nodes       []PipelineNode       `json:"nodes"`
	Edges       []PipelineEdge       `json:"edges"`
	Connections []PipelineConnection `json:"connections"`
}

// PipelineNode represents a node in the pipeline
type PipelineNode struct {
	ID      string                 `json:"id"`
	Type    string                 `json:"type"`
	Name    string                 `json:"name"`
	Config  map[string]interface{} `json:"config,omitempty"`
	Params  map[string]interface{} `json:"params,omitempty"`
	Timeout int                    `json:"timeout"`
}

// PipelineEdge represents an edge in the pipeline DAG
type PipelineEdge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	IgnoreFailure bool   `json:"ignore_failure"`
}

// PipelineConnection represents a connection (old format)
type PipelineConnection struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// AgentConfig represents agent-specific configuration
type AgentConfig struct {
	Workspace string            `json:"workspace"`
	Timeout   int               `json:"timeout"`
	EnvVars   map[string]string `json:"env_vars"`
}

// WebSocketClient owns the agent's single live WS session to the current owner
// server.
//
// In the distributed runtime, reconnect is not just a transport concern: a new
// connection also means a new server-issued session identity, and downstream
// task/log reporting must switch to that session cleanly.
type WebSocketClient struct {
	baseURL        string
	conn           *websocket.Conn
	mu             sync.RWMutex
	running        bool
	stopChan       chan struct{}
	agentID        uint64
	token          string
	registerKey    string
	sessionID      string
	cfg            *websocketConfig
	taskHandler    TaskHandler
	executor       *task.Executor
	onReconnect    func()
	onHeartbeatAck func(map[string]interface{})
}

// websocketConfig holds WebSocket configuration
type websocketConfig struct {
	heartbeatInterval    int
	reconnectDelay       time.Duration
	maxReconnectAttempts int
}

var defaultConfig = &websocketConfig{
	heartbeatInterval:    10,
	reconnectDelay:       5 * time.Second,
	maxReconnectAttempts: 10,
}

// WebSocketMessage represents a message sent/received via WebSocket
type WebSocketMessage struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(baseURL string, agentID uint64, token, registerKey string) *WebSocketClient {
	return &WebSocketClient{
		baseURL:     baseURL,
		agentID:     agentID,
		token:       token,
		registerKey: registerKey,
		stopChan:    make(chan struct{}),
		cfg:         defaultConfig,
	}
}

// SetTaskHandler sets the task handler for processing incoming tasks
func (c *WebSocketClient) SetTaskHandler(handler TaskHandler) {
	c.mu.Lock()
	c.taskHandler = handler
	c.mu.Unlock()
}

// SetExecutor sets the task executor
func (c *WebSocketClient) SetExecutor(executor *task.Executor) {
	c.mu.Lock()
	c.executor = executor
	c.mu.Unlock()
}

// SetReconnectHandler registers a callback that should run only after reconnect
// has actually restored a live socket.
func (c *WebSocketClient) SetReconnectHandler(handler func()) {
	c.mu.Lock()
	c.onReconnect = handler
	c.mu.Unlock()
}

func (c *WebSocketClient) SetHeartbeatAckHandler(handler func(map[string]interface{})) {
	c.mu.Lock()
	c.onHeartbeatAck = handler
	c.mu.Unlock()
}

// SetConfig sets the WebSocket configuration
func (c *WebSocketClient) SetConfig(cfg *websocketConfig) {
	c.cfg = cfg
}

// Connect establishes a WebSocket connection to the server
func (c *WebSocketClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	wasRunning := c.running
	c.running = true
	c.mu.Unlock()

	// Build WebSocket URL
	wsURL := c.baseURL
	if len(wsURL) >= 7 && wsURL[:7] == "http://" {
		wsURL = "ws://" + wsURL[7:] + "/ws/agent/heartbeat"
	} else if len(wsURL) >= 8 && wsURL[:8] == "https://" {
		wsURL = "wss://" + wsURL[8:] + "/ws/agent/heartbeat"
	} else {
		wsURL = wsURL + "/ws/agent/heartbeat"
	}

	// Add query parameters
	if c.agentID > 0 {
		if c.token != "" {
			wsURL += fmt.Sprintf("?agent_id=%d&token=%s", c.agentID, c.token)
		} else if c.registerKey != "" {
			wsURL += fmt.Sprintf("?agent_id=%d&register_key=%s", c.agentID, c.registerKey)
		} else {
			wsURL += fmt.Sprintf("?agent_id=%d", c.agentID)
		}
	}

	// Create dialer with timeout
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, http.Header{
		"Content-Type": {"application/json"},
	})
	if err != nil {
		if !wasRunning {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
		}
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	klog.Infof("WebSocket connected to %s", wsURL)

	// Start read and write loops
	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	return nil
}

// readLoop continuously reads messages from the WebSocket connection
func (c *WebSocketClient) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		default:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				time.Sleep(time.Second)
				continue
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				klog.Warningf("WebSocket read error: %v", err)
				c.handleDisconnect(ctx)
				return
			}

			// Parse and handle the message
			c.handleMessage(message)
		}
	}
}

// writeLoop handles sending messages and heartbeats
func (c *WebSocketClient) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(c.cfg.heartbeatInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			// Send heartbeat
			if err := c.sendHeartbeat(); err != nil {
				klog.Warningf("Failed to send heartbeat: %v", err)
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *WebSocketClient) handleMessage(message []byte) {
	var msg WebSocketMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		klog.Warningf("Failed to parse WebSocket message: %v", err)
		return
	}

	switch msg.Type {
	case "task_assign":
		c.handleTaskAssign(msg.Payload)
	case "task_payload":
		c.handleTaskAssign(msg.Payload)
	case "pull_task_now":
		c.handlePullTaskNow(msg.Payload)
	case "pipeline_assign":
		c.handlePipelineAssign(msg.Payload)
	case "task_cancel":
		c.handleTaskCancel(msg.Payload)
	case "agent_config":
		c.handleAgentConfig(msg.Payload)
	case "heartbeat_ack":
		c.handleHeartbeatAck(msg.Payload)
	default:
		klog.V(4).Infof("Received message of type: %s", msg.Type)
	}
}

func (c *WebSocketClient) handlePullTaskNow(payload map[string]interface{}) {
	c.mu.RLock()
	handler := c.taskHandler
	c.mu.RUnlock()
	if handler == nil {
		klog.Warning("No task handler configured, cannot process pull_task_now")
		return
	}
	taskID := uint64(0)
	if id, ok := payload["task_id"].(float64); ok {
		taskID = uint64(id)
	}
	dispatchToken := ""
	if token, ok := payload["dispatch_token"].(string); ok {
		dispatchToken = token
	}
	if taskID == 0 {
		klog.Warning("Invalid task ID in pull_task_now")
		return
	}
	go func() {
		if err := handler.HandlePullTaskNow(taskID, dispatchToken); err != nil {
			klog.Warningf("Failed to handle pull_task_now for task %d: %v", taskID, err)
		}
	}()
}

// handleHeartbeatAck captures the authoritative session id assigned by the
// server. The agent mirrors that value back on later control/data messages so
// the server can fence stale connections after failover.
func (c *WebSocketClient) handleHeartbeatAck(payload map[string]interface{}) {
	if sessionID, ok := payload["agent_session_id"].(string); ok && sessionID != "" {
		c.mu.Lock()
		c.sessionID = sessionID
		c.mu.Unlock()
	}
	c.mu.RLock()
	handler := c.onHeartbeatAck
	c.mu.RUnlock()
	if handler != nil {
		handler(payload)
	}
}

func (c *WebSocketClient) handleTaskAssign(payload map[string]interface{}) {
	c.mu.RLock()
	handler := c.taskHandler
	c.mu.RUnlock()

	if handler == nil {
		klog.Warning("No task handler configured, cannot process task assignment")
		return
	}

	assignMsg := &TaskAssignMessage{}
	if taskRaw, ok := payload["task"]; ok {
		data, err := json.Marshal(map[string]interface{}{
			"task":      taskRaw,
			"timestamp": payload["timestamp"],
		})
		if err != nil {
			klog.Warningf("Failed to marshal task assign payload: %v", err)
			return
		}
		if err := json.Unmarshal(data, assignMsg); err != nil {
			klog.Warningf("Failed to parse task assign payload: %v", err)
			return
		}
	} else {
		// Backward compatibility: payload itself may be the task object.
		data, err := json.Marshal(payload)
		if err != nil {
			klog.Warningf("Failed to marshal task payload: %v", err)
			return
		}
		if err := json.Unmarshal(data, &assignMsg.Task); err != nil {
			klog.Warningf("Failed to parse task payload: %v", err)
			return
		}
	}

	if assignMsg.Task.ID == 0 {
		klog.Warning("Invalid task assignment, missing task id")
		return
	}

	go func() {
		if err := handler.HandleTaskAssign(assignMsg); err != nil {
			klog.Warningf("Failed to handle task assignment: %v", err)
		}
	}()
}

// handlePipelineAssign handles pipeline assignment message from server
func (c *WebSocketClient) handlePipelineAssign(payload map[string]interface{}) {
	c.mu.RLock()
	handler := c.taskHandler
	c.mu.RUnlock()

	if handler == nil {
		klog.Warning("No task handler configured, cannot process pipeline assignment")
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		klog.Warningf("Failed to marshal pipeline assign payload: %v", err)
		return
	}

	var msg PipelineAssignMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		klog.Warningf("Failed to parse pipeline assign message: %v", err)
		return
	}

	klog.Infof("Received pipeline assignment: run_id=%d", msg.RunID)

	go func() {
		if err := handler.HandlePipelineAssign(&msg); err != nil {
			klog.Warningf("Failed to handle pipeline assignment: %v", err)
		}
	}()
}

// handleTaskCancel handles task cancellation message from server
func (c *WebSocketClient) handleTaskCancel(payload map[string]interface{}) {
	c.mu.RLock()
	handler := c.taskHandler
	c.mu.RUnlock()

	if handler == nil {
		klog.Warning("No task handler configured, cannot process task cancel")
		return
	}

	taskID := uint64(0)
	if id, ok := payload["task_id"].(float64); ok {
		taskID = uint64(id)
	}

	if taskID == 0 {
		klog.Warning("Invalid task ID in cancel message")
		return
	}

	klog.Infof("Received task cancellation: task_id=%d", taskID)

	go func() {
		if err := handler.HandleTaskCancel(taskID); err != nil {
			klog.Warningf("Failed to handle task cancel: %v", err)
		}
	}()
}

// handleAgentConfig handles agent configuration update from server
func (c *WebSocketClient) handleAgentConfig(payload map[string]interface{}) {
	klog.V(4).Infof("Received agent config update: %+v", payload)
}

// handleDisconnect tears down the dead socket and performs bounded reconnect
// attempts. A successful reconnect triggers the registered replay callback so
// buffered terminal/log messages can be resent on the new session.
func (c *WebSocketClient) handleDisconnect(ctx context.Context) {
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()

	klog.Warning("WebSocket disconnected, attempting to reconnect...")

	// Attempt to reconnect
	for attempt := 0; attempt < c.cfg.maxReconnectAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-time.After(c.cfg.reconnectDelay):
			if err := c.Connect(ctx); err != nil {
				klog.Warningf("Reconnection attempt %d failed: %v", attempt+1, err)
				continue
			}
			if !c.IsConnected() {
				klog.Warningf("Reconnection attempt %d did not establish connection", attempt+1)
				continue
			}
			klog.Infof("WebSocket reconnected successfully")
			c.mu.RLock()
			handler := c.onReconnect
			c.mu.RUnlock()
			if handler != nil {
				go handler()
			}
			return
		}
	}

	klog.Error("Max reconnection attempts reached")
}

// sendHeartbeat sends a heartbeat message to the server
func (c *WebSocketClient) sendHeartbeat() error {
	c.mu.RLock()
	agentID := c.agentID
	token := c.token
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || agentID == 0 {
		return fmt.Errorf("not connected or not registered")
	}

	// Log heartbeat send
	klog.V(4).Infof("Sending heartbeat for agent %d", agentID)

	msg := WebSocketMessage{
		Type: "heartbeat",
		Payload: map[string]interface{}{
			"agent_id":         agentID,
			"token":            token,
			"timestamp":        time.Now().Unix(),
			"agent_session_id": c.GetSessionID(),
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	c.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

// SendMessage sends a custom message to the server
func (c *WebSocketClient) SendMessage(msgType string, payload map[string]interface{}) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := WebSocketMessage{
		Type:    msgType,
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	c.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()

	return err
}

// SetAgentID sets the agent ID
func (c *WebSocketClient) SetAgentID(agentID uint64) {
	c.mu.Lock()
	c.agentID = agentID
	c.mu.Unlock()
}

// SetToken sets the authentication token
func (c *WebSocketClient) SetToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

func (c *WebSocketClient) SetRegisterKey(registerKey string) {
	c.mu.Lock()
	c.registerKey = registerKey
	c.mu.Unlock()
}

// IsConnected returns whether the client is connected
func (c *WebSocketClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

func (c *WebSocketClient) GetSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// ReportTaskLog reports a task log entry via WebSocket
func (c *WebSocketClient) ReportTaskLog(runID uint64, nodeID string, level, message, source string, lineNumber int) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	payload := map[string]interface{}{
		"run_id":      runID,
		"node_id":     nodeID,
		"level":       level,
		"message":     message,
		"source":      source,
		"line_number": lineNumber,
		"timestamp":   time.Now().UnixMilli(),
	}

	msg := WebSocketMessage{
		Type:    "task_log_stream",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal log message: %w", err)
	}

	c.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()

	return err
}

// ReportTaskStatus reports task status via WebSocket
func (c *WebSocketClient) ReportTaskStatus(runID uint64, nodeID, status string, exitCode int, errorMsg string, result map[string]interface{}) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	payload := map[string]interface{}{
		"run_id":    runID,
		"node_id":   nodeID,
		"status":    status,
		"exit_code": exitCode,
		"error_msg": errorMsg,
		"timestamp": time.Now().UnixMilli(),
	}

	if result != nil {
		payload["result"] = result
	}

	msg := WebSocketMessage{
		Type:    "task_status",
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal status message: %w", err)
	}

	c.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()

	return err
}

// Close closes the WebSocket connection
func (c *WebSocketClient) Close() error {
	close(c.stopChan)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.running = false

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}

	return nil
}
