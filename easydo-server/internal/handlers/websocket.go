package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm/clause"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WebSocketHandler handles WebSocket connections for agents and frontend clients
type WebSocketHandler struct {
	agents          map[uint64]*wsClient
	agentsMu        sync.RWMutex
	frontends       map[string]map[string]*frontendClient // key: runID, value: map of clientID->client
	frontendsMu     sync.RWMutex
	clientIDCounter uint64
	clientIDMu      sync.Mutex
}

var (
	sharedWebSocketHandler     *WebSocketHandler
	sharedWebSocketHandlerOnce sync.Once
)

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		agents:    make(map[uint64]*wsClient),
		frontends: make(map[string]map[string]*frontendClient),
	}
}

// SharedWebSocketHandler returns the process-wide WebSocket handler instance.
// Runtime handlers/router must use this instance to share agent/frontend connections.
func SharedWebSocketHandler() *WebSocketHandler {
	sharedWebSocketHandlerOnce.Do(func() {
		sharedWebSocketHandler = NewWebSocketHandler()
	})
	return sharedWebSocketHandler
}

// wsClient represents a connected agent WebSocket client
type wsClient struct {
	conn        *websocket.Conn
	agentID     uint64
	lastHeartAt int64
	mu          sync.Mutex
}

// frontendClient represents a connected frontend WebSocket client
type frontendClient struct {
	conn   *websocket.Conn
	runID  string
	userID uint64
	mu     sync.Mutex
}

// WebSocketMessage represents a WebSocket message
type WebSocketMessage struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// HandleAgentConnection handles new WebSocket connection requests from agents
func (h *WebSocketHandler) HandleAgentConnection(c *gin.Context) {
	agentIDStr := c.Query("agent_id")
	token := c.Query("token")

	if agentIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "agent_id is required",
		})
		return
	}

	agentID, err := strconv.ParseUint(agentIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid agent_id",
		})
		return
	}

	var agent models.Agent
	if err := models.DB.Where("id = ?", agentID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "agent not found",
		})
		return
	}

	// Verify token for approved agents
	if agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
		if token == "" || agent.Token != token {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "invalid token",
			})
			return
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}

	client := &wsClient{
		conn:        conn,
		agentID:     agentID,
		lastHeartAt: time.Now().Unix(),
	}

	h.agentsMu.Lock()
	h.agents[agentID] = client
	h.agentsMu.Unlock()

	fmt.Printf("Agent %d connected via WebSocket\n", agentID)

	models.DB.Model(agent).Updates(map[string]interface{}{
		"status":        models.AgentStatusOnline,
		"last_heart_at": time.Now().Unix(),
	})

	// Push all currently pending tasks to the just-connected agent.
	_ = h.dispatchPendingTasks(agentID)

	h.handleAgentMessages(client, &agent)
}

// HandleFrontendConnection handles WebSocket connections from frontend clients
func (h *WebSocketHandler) HandleFrontendConnection(c *gin.Context) {
	runID := c.Query("run_id")
	token := c.Query("token")

	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "run_id is required",
		})
		return
	}

	// Verify user via JWT token (simplified - in production, properly validate JWT)
	userID := c.GetUint64("user_id")
	if userID == 0 {
		// Try to get from token query param as fallback
		if token != "" {
			// In a real implementation, parse JWT and extract user ID
			// For now, use a default
			userID = 1
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}

	// Generate unique client ID
	h.clientIDMu.Lock()
	h.clientIDCounter++
	clientID := fmt.Sprintf("client_%d", h.clientIDCounter)
	h.clientIDMu.Unlock()

	client := &frontendClient{
		conn:   conn,
		runID:  runID,
		userID: userID,
	}

	h.frontendsMu.Lock()
	if h.frontends[runID] == nil {
		h.frontends[runID] = make(map[string]*frontendClient)
	}
	h.frontends[runID][clientID] = client
	h.frontendsMu.Unlock()

	fmt.Printf("Frontend client %s connected for run %s\n", clientID, runID)

	h.handleFrontendMessages(client, runID, clientID)
}

// handleAgentMessages handles incoming messages from an agent
func (h *WebSocketHandler) handleAgentMessages(client *wsClient, agent *models.Agent) {
	defer func() {
		h.agentsMu.Lock()
		delete(h.agents, client.agentID)
		h.agentsMu.Unlock()

		models.DB.Model(agent).Updates(map[string]interface{}{
			"status": models.AgentStatusOffline,
		})

		client.mu.Lock()
		client.conn.Close()
		client.mu.Unlock()

		fmt.Printf("Agent %d disconnected\n", client.agentID)
	}()

	for {
		client.mu.Lock()
		client.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		client.mu.Unlock()

		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("WebSocket error for agent %d: %v\n", client.agentID, err)
			}
			break
		}

		var msg WebSocketMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			fmt.Printf("Failed to parse message from agent %d: %v\n", client.agentID, err)
			continue
		}

		switch msg.Type {
		case "heartbeat":
			h.handleAgentHeartbeat(client, agent, msg.Payload)
		case "task_update_v2":
			h.handleTaskUpdateV2(client, agent, msg.Payload)
		case "task_log_chunk_v2":
			h.handleTaskLogChunkV2(client, agent, msg.Payload)
		case "task_log_end_v2":
			h.handleTaskLogEndV2(client, agent, msg.Payload)
		case "task_status":
			h.handleTaskStatus(client, agent, msg.Payload)
		case "task_log":
			h.handleTaskLog(client, agent, msg.Payload)
		case "task_log_stream":
			h.handleTaskLogStream(client, agent, msg.Payload)
		default:
			fmt.Printf("Unknown message type from agent %d: %s\n", client.agentID, msg.Type)
		}
	}
}

// handleFrontendMessages handles incoming messages from frontend
func (h *WebSocketHandler) handleFrontendMessages(client *frontendClient, runID, clientID string) {
	defer func() {
		h.frontendsMu.Lock()
		if runClients, exists := h.frontends[runID]; exists {
			delete(runClients, clientID)
			if len(runClients) == 0 {
				delete(h.frontends, runID)
			}
		}
		h.frontendsMu.Unlock()

		client.mu.Lock()
		client.conn.Close()
		client.mu.Unlock()

		fmt.Printf("Frontend client %s disconnected for run %s\n", clientID, runID)
	}()

	for {
		client.mu.Lock()
		client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		client.mu.Unlock()

		_, _, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("WebSocket error for frontend client %s, run %s: %v\n", clientID, runID, err)
			}
			break
		}
	}
}

// handleAgentHeartbeat processes a heartbeat message from an agent
func (h *WebSocketHandler) handleAgentHeartbeat(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	agentTimestamp := getInt64(payload, "timestamp")
	if agentTimestamp == 0 {
		agentTimestamp = time.Now().Unix()
	}

	client.lastHeartAt = agentTimestamp

	newSuccessCount := agent.ConsecutiveSuccess + 1
	if newSuccessCount > 3 {
		newSuccessCount = 3
	}

	updates := map[string]interface{}{
		"last_heart_at":        agentTimestamp,
		"consecutive_success":  newSuccessCount,
		"consecutive_failures": 0,
	}

	if agent.Status != models.AgentStatusOnline && newSuccessCount >= 3 {
		updates["status"] = models.AgentStatusOnline
		fmt.Printf("Agent %d status updated to online\n", client.agentID)
	}

	models.DB.Model(agent).Updates(updates)

	// Get pending tasks
	var pendingTasks []models.AgentTask
	if agent.RegistrationStatus == models.AgentRegistrationStatusApproved {
		models.DB.Where("agent_id = ? AND status = ?", client.agentID, models.TaskStatusPending).Find(&pendingTasks)
	}

	response := WebSocketMessage{
		Type: "heartbeat_ack",
		Payload: map[string]interface{}{
			"status":             "ok",
			"server_time":        time.Now().Unix(),
			"pending_tasks":      len(pendingTasks),
			"heartbeat_interval": agent.HeartbeatInterval,
		},
	}

	responseData, _ := json.Marshal(response)
	client.mu.Lock()
	client.conn.WriteMessage(websocket.TextMessage, responseData)
	client.mu.Unlock()

	if len(pendingTasks) > 0 {
		_ = h.dispatchPendingTasks(client.agentID)
	}
}

// handleTaskStatus processes a task status message from an agent
func (h *WebSocketHandler) handleTaskStatus(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	legacy := map[string]interface{}{
		"task_id": uint64(getFloat64(payload, "task_id")),
		"attempt": 1,
		"status":  getString(payload, "status"),
	}

	if exitCode := int(getFloat64(payload, "exit_code")); exitCode != 0 {
		legacy["exit_code"] = exitCode
	}
	if errorMsg := getString(payload, "error_msg"); errorMsg != "" {
		legacy["error_msg"] = errorMsg
	}

	if result, ok := payload["result"].(map[string]interface{}); ok {
		legacy["result"] = result
		if d := int64(getFloat64(result, "duration")); d > 0 {
			legacy["duration_ms"] = d * 1000
		}
	}
	if _, ok := legacy["duration_ms"]; !ok {
		if d := int64(getFloat64(payload, "duration")); d > 0 {
			legacy["duration_ms"] = d * 1000
		}
	}
	if key := getString(payload, "idempotency_key"); key != "" {
		legacy["idempotency_key"] = key
	}
	legacy["timestamp"] = time.Now().Unix()

	h.handleTaskUpdateV2(client, agent, legacy)
}

// handleTaskLog processes a task log message from an agent
func (h *WebSocketHandler) handleTaskLog(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	seq := int64(getFloat64(payload, "line_number"))
	if seq <= 0 {
		seq = time.Now().UnixNano()
	}

	logPayload := map[string]interface{}{
		"task_id":   uint64(getFloat64(payload, "task_id")),
		"attempt":   1,
		"seq":       seq,
		"level":     getString(payload, "level"),
		"stream":    getString(payload, "source"),
		"chunk":     getString(payload, "message"),
		"timestamp": getInt64(payload, "timestamp"),
	}

	h.handleTaskLogChunkV2(client, agent, logPayload)
}

// handleTaskLogStream handles streaming task log chunks from an agent
func (h *WebSocketHandler) handleTaskLogStream(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	seq := int64(getFloat64(payload, "line_number"))
	if seq <= 0 {
		seq = time.Now().UnixNano()
	}
	logPayload := map[string]interface{}{
		"task_id":   uint64(getFloat64(payload, "task_id")),
		"attempt":   1,
		"seq":       seq,
		"stream":    getString(payload, "source"),
		"chunk":     getString(payload, "chunk"),
		"timestamp": getInt64(payload, "timestamp"),
	}

	h.handleTaskLogChunkV2(client, agent, logPayload)
}

type taskUpdatePayloadV2 struct {
	TaskID         uint64
	Attempt        int
	Status         string
	ExitCode       int
	ErrorMsg       string
	DurationMs     int64
	IdempotencyKey string
	Result         map[string]interface{}
	Timestamp      int64
}

type taskLogChunkPayloadV2 struct {
	TaskID    uint64
	Attempt   int
	Seq       int64
	Level     string
	Stream    string
	Chunk     string
	Timestamp int64
}

func (h *WebSocketHandler) handleTaskUpdateV2(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	update := parseTaskUpdatePayloadV2(payload)
	if update.TaskID == 0 || update.Status == "" {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "invalid task update payload", nil)
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, update.TaskID).Error; err != nil {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "task not found", nil)
		return
	}
	if task.AgentID != client.agentID {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "task does not belong to agent", nil)
		return
	}

	if update.Attempt <= 0 {
		update.Attempt = task.RetryCount + 1
	}
	if update.IdempotencyKey == "" {
		update.IdempotencyKey = buildTaskUpdateIdempotencyKey(update.TaskID, update.Attempt, update.Status, update.ExitCode)
	}

	var existingEvent models.AgentTaskEvent
	if err := models.DB.Where("task_id = ? AND attempt = ? AND idempotency_key = ?", update.TaskID, update.Attempt, update.IdempotencyKey).
		First(&existingEvent).Error; err == nil {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, true, "", map[string]interface{}{"duplicate": true})
		return
	}

	if !isValidTaskStatusTransition(task.Status, update.Status) {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "invalid status transition", nil)
		return
	}

	event := &models.AgentTaskEvent{
		TaskID:         update.TaskID,
		PipelineRunID:  task.PipelineRunID,
		AgentID:        client.agentID,
		Attempt:        update.Attempt,
		Status:         update.Status,
		IdempotencyKey: update.IdempotencyKey,
		ExitCode:       update.ExitCode,
		DurationMs:     update.DurationMs,
		ErrorMsg:       update.ErrorMsg,
	}

	result := models.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "attempt"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(event)
	if result.Error != nil {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "failed to persist task event", nil)
		return
	}
	if result.RowsAffected == 0 {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, true, "", map[string]interface{}{"duplicate": true})
		return
	}

	now := time.Now().Unix()
	updates := map[string]interface{}{
		"status":    update.Status,
		"exit_code": update.ExitCode,
		"error_msg": update.ErrorMsg,
	}

	if update.Result != nil {
		if data, err := json.Marshal(update.Result); err == nil {
			updates["result_data"] = string(data)
		}
	}

	if update.Status == models.TaskStatusRunning {
		if task.StartTime == 0 {
			updates["start_time"] = now
		}
	}

	isTerminal := isTerminalTaskStatus(update.Status)
	if isTerminal {
		updates["end_time"] = now
		durationSec := int(update.DurationMs / 1000)
		if durationSec <= 0 && task.StartTime > 0 {
			durationSec = int(now - task.StartTime)
		}
		updates["duration"] = durationSec
	}

	if err := models.DB.Model(&task).Updates(updates).Error; err != nil {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "failed to update task", nil)
		return
	}

	executionStartTime := task.StartTime
	if v, ok := updates["start_time"].(int64); ok {
		executionStartTime = v
	}
	durationForFrontend := 0
	if v, ok := updates["duration"].(int); ok {
		durationForFrontend = v
	}

	h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
		"task_id":    update.TaskID,
		"run_id":     task.PipelineRunID,
		"status":     update.Status,
		"exit_code":  update.ExitCode,
		"error_msg":  update.ErrorMsg,
		"duration":   durationForFrontend,
		"agent_id":   client.agentID,
		"agent_name": agent.Name,
		"timestamp":  now,
	})

	if isTerminal {
		execution := &models.TaskExecution{
			TaskID:    task.ID,
			Attempt:   update.Attempt,
			Status:    update.Status,
			StartTime: executionStartTime,
			EndTime:   now,
			Duration:  durationForFrontend,
			ExitCode:  update.ExitCode,
			ErrorMsg:  update.ErrorMsg,
		}
		models.DB.Create(execution)
	}

	// Automatic retry/failover: only when task failed and retries remain.
	// Final failure after retries will be handled by ignore_failure semantics at pipeline level.
	if update.Status == models.TaskStatusFailed && task.RetryCount < task.MaxRetries {
		nextRetry := task.RetryCount + 1
		retryUpdates := map[string]interface{}{
			"status":      models.TaskStatusPending,
			"retry_count": nextRetry,
			"start_time":  int64(0),
			"end_time":    int64(0),
			"duration":    0,
			"exit_code":   0,
			"error_msg":   "",
			"result_data": "",
		}
		if err := models.DB.Model(&task).Updates(retryUpdates).Error; err != nil {
			h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "failed to enqueue retry", nil)
			return
		}

		task.Status = models.TaskStatusPending
		task.RetryCount = nextRetry
		task.StartTime = 0
		task.EndTime = 0
		task.Duration = 0
		task.ExitCode = 0
		task.ErrorMsg = ""

		h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
			"task_id":     task.ID,
			"run_id":      task.PipelineRunID,
			"status":      models.TaskStatusPending,
			"exit_code":   0,
			"error_msg":   "",
			"duration":    0,
			"agent_id":    client.agentID,
			"agent_name":  agent.Name,
			"retry_count": task.RetryCount,
			"max_retries": task.MaxRetries,
			"retrying":    true,
			"timestamp":   time.Now().Unix(),
		})

		_ = h.sendTaskAssign(task)
		h.checkAgentStatus(client.agentID)
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, true, "", map[string]interface{}{
			"retrying":    true,
			"retry_count": task.RetryCount,
			"max_retries": task.MaxRetries,
		})
		return
	}

	h.checkAgentStatus(client.agentID)

	if isTerminal {
		var completedTasks []models.AgentTask
		models.DB.Where("pipeline_run_id = ?", task.PipelineRunID).Find(&completedTasks)
		h.triggerDownstreamTasks(task.PipelineRunID, completedTasks)
		h.checkAndUpdatePipelineStatus(task.PipelineRunID)
	}

	h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, true, "", nil)
}

func (h *WebSocketHandler) handleTaskLogChunkV2(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	logChunk := parseTaskLogChunkPayloadV2(payload)
	if logChunk.TaskID == 0 || logChunk.Chunk == "" {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "invalid task log payload", nil)
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, logChunk.TaskID).Error; err != nil {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "task not found", nil)
		return
	}
	if task.AgentID != client.agentID {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "task does not belong to agent", nil)
		return
	}

	if logChunk.Attempt <= 0 {
		logChunk.Attempt = task.RetryCount + 1
	}
	if logChunk.Seq <= 0 {
		logChunk.Seq = time.Now().UnixNano()
	}
	if logChunk.Stream == "" {
		logChunk.Stream = "stdout"
	}
	if logChunk.Level == "" {
		if logChunk.Stream == "stderr" {
			logChunk.Level = "error"
		} else {
			logChunk.Level = "info"
		}
	}
	if logChunk.Timestamp == 0 {
		logChunk.Timestamp = time.Now().Unix()
	}
	normalizedTimestamp := normalizeUnixTimestamp(logChunk.Timestamp)

	uniqueKey := buildTaskLogChunkUniqueKey(logChunk.TaskID, logChunk.Attempt, logChunk.Seq)
	if _, loaded := fileLogChunkSeen.LoadOrStore(uniqueKey, struct{}{}); loaded {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, true, "", map[string]interface{}{
			"seq":       logChunk.Seq,
			"duplicate": true,
		})
		return
	}

	if err := agentFileLogs.Append(fileLogEntry{
		TaskID:        logChunk.TaskID,
		PipelineRunID: task.PipelineRunID,
		Level:         logChunk.Level,
		Message:       logChunk.Chunk,
		Source:        logChunk.Stream,
		Timestamp:     normalizedTimestamp,
		LineNumber:    int(logChunk.Seq),
		Attempt:       logChunk.Attempt,
		Seq:           logChunk.Seq,
	}); err != nil {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "failed to persist task log chunk", nil)
		return
	}

	h.broadcastToFrontend(task.PipelineRunID, "task_log_stream", map[string]interface{}{
		"task_id":   logChunk.TaskID,
		"run_id":    task.PipelineRunID,
		"attempt":   logChunk.Attempt,
		"seq":       logChunk.Seq,
		"stream":    logChunk.Stream,
		"chunk":     logChunk.Chunk,
		"timestamp": normalizedTimestamp,
	})
	h.broadcastToFrontend(task.PipelineRunID, "task_log", map[string]interface{}{
		"task_id":     logChunk.TaskID,
		"run_id":      task.PipelineRunID,
		"level":       logChunk.Level,
		"message":     logChunk.Chunk,
		"source":      logChunk.Stream,
		"line_number": logChunk.Seq,
		"timestamp":   normalizedTimestamp,
	})

	h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, true, "", map[string]interface{}{
		"seq":       logChunk.Seq,
		"duplicate": false,
	})
}

func (h *WebSocketHandler) handleTaskLogEndV2(client *wsClient, _ *models.Agent, payload map[string]interface{}) {
	taskID := uint64(getFloat64(payload, "task_id"))
	attempt := int(getFloat64(payload, "attempt"))
	if taskID == 0 {
		h.sendAgentAck(client, "task_log_end_v2", 0, attempt, false, "invalid task id", nil)
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err != nil {
		h.sendAgentAck(client, "task_log_end_v2", taskID, attempt, false, "task not found", nil)
		return
	}
	if task.AgentID != client.agentID {
		h.sendAgentAck(client, "task_log_end_v2", taskID, attempt, false, "task does not belong to agent", nil)
		return
	}
	if attempt <= 0 {
		attempt = task.RetryCount + 1
	}

	h.sendAgentAck(client, "task_log_end_v2", taskID, attempt, true, "", map[string]interface{}{
		"final_seq": int64(getFloat64(payload, "final_seq")),
	})
}

func (h *WebSocketHandler) sendMessageToAgent(agentID uint64, msgType string, payload map[string]interface{}) bool {
	h.agentsMu.RLock()
	client, exists := h.agents[agentID]
	h.agentsMu.RUnlock()
	if !exists || client == nil || client.conn == nil {
		return false
	}

	msg := WebSocketMessage{
		Type:    msgType,
		Payload: payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	client.mu.Lock()
	err = client.conn.WriteMessage(websocket.TextMessage, data)
	client.mu.Unlock()
	return err == nil
}

func (h *WebSocketHandler) sendTaskAssign(task models.AgentTask) bool {
	if task.AgentID == 0 || task.ID == 0 {
		return false
	}

	payload := map[string]interface{}{
		"task": map[string]interface{}{
			"id":              task.ID,
			"agent_id":        task.AgentID,
			"pipeline_run_id": task.PipelineRunID,
			"node_id":         task.NodeID,
			"task_type":       task.TaskType,
			"name":            task.Name,
			"params":          task.Params,
			"script":          task.Script,
			"work_dir":        task.WorkDir,
			"env_vars":        task.EnvVars,
			"status":          task.Status,
			"priority":        task.Priority,
			"timeout":         task.Timeout,
			"retry_count":     task.RetryCount,
			"max_retries":     task.MaxRetries,
		},
		"timestamp": time.Now().Unix(),
	}

	return h.sendMessageToAgent(task.AgentID, "task_assign", payload)
}

func (h *WebSocketHandler) dispatchPendingTasks(agentID uint64) int {
	if agentID == 0 {
		return 0
	}

	var pendingTasks []models.AgentTask
	models.DB.Where("agent_id = ? AND status = ?", agentID, models.TaskStatusPending).
		Order("priority DESC, created_at ASC").
		Find(&pendingTasks)

	dispatched := 0
	for i := range pendingTasks {
		if h.sendTaskAssign(pendingTasks[i]) {
			dispatched++
		}
	}

	return dispatched
}

// broadcastToFrontend broadcasts a message to all frontend clients subscribed to a run
func (h *WebSocketHandler) broadcastToFrontend(runID uint64, msgType string, payload map[string]interface{}) {
	runIDStr := strconv.FormatUint(runID, 10)

	h.frontendsMu.RLock()
	runClients, exists := h.frontends[runIDStr]
	h.frontendsMu.RUnlock()

	if !exists || len(runClients) == 0 {
		return
	}

	msg := WebSocketMessage{
		Type:    msgType,
		Payload: payload,
	}

	data, _ := json.Marshal(msg)

	h.frontendsMu.RLock()
	defer h.frontendsMu.RUnlock()

	for clientID, client := range runClients {
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()

		if err != nil {
			fmt.Printf("Failed to send message to frontend client %s for run %s: %v\n", clientID, runIDStr, err)
		}
	}
}

// BroadcastTaskStatus broadcasts task status to all frontend clients
func (h *WebSocketHandler) BroadcastTaskStatus(runID, taskID uint64, status string, exitCode int, errorMsg, agentName string) {
	payload := map[string]interface{}{
		"task_id":    taskID,
		"run_id":     runID,
		"status":     status,
		"exit_code":  exitCode,
		"error_msg":  errorMsg,
		"agent_name": agentName,
	}
	h.broadcastToFrontend(runID, "task_status", payload)
}

// BroadcastRunStatus broadcasts pipeline run status to all frontend clients
func (h *WebSocketHandler) BroadcastRunStatus(runID uint64, status, errorMsg string, duration ...int) {
	payload := map[string]interface{}{
		"run_id":    runID,
		"status":    status,
		"error_msg": errorMsg,
		"timestamp": time.Now().Unix(),
	}
	if len(duration) > 0 {
		payload["duration"] = duration[0]
	}
	h.broadcastToFrontend(runID, "run_status", payload)
}

// checkAgentStatus updates agent status based on running tasks
func (h *WebSocketHandler) checkAgentStatus(agentID uint64) {
	var runningTasks int64
	models.DB.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agentID, models.TaskStatusRunning).Count(&runningTasks)

	var pendingTasks int64
	models.DB.Model(&models.AgentTask{}).Where("agent_id = ? AND status = ?", agentID, models.TaskStatusPending).Count(&pendingTasks)

	status := models.AgentStatusOnline
	if runningTasks > 0 || pendingTasks > 0 {
		status = models.AgentStatusBusy
	}

	models.DB.Model(&models.Agent{}).Where("id = ?", agentID).Update("status", status)
}

// checkAndUpdatePipelineStatus checks all tasks status and updates pipeline run status accordingly
// This should be called when a task completes (success/failed) to determine the overall pipeline status
func (h *WebSocketHandler) checkAndUpdatePipelineStatus(runID uint64) {
	var run models.PipelineRun
	if err := models.DB.First(&run, runID).Error; err != nil {
		return
	}
	if run.Status != "running" {
		return
	}

	var tasks []models.AgentTask
	models.DB.Where("pipeline_run_id = ?", runID).Find(&tasks)
	if len(tasks) == 0 {
		return
	}

	nodeIgnoreFailure := map[string]bool{}
	if run.Config != "" {
		var config PipelineConfig
		if err := json.Unmarshal([]byte(run.Config), &config); err == nil {
			for i := range config.Nodes {
				nodeIgnoreFailure[config.Nodes[i].ID] = config.Nodes[i].IgnoreFailure
			}
		}
	}

	allTerminal := true
	hasBlockingFailure := false
	firstErrorMsg := ""

	for i := range tasks {
		task := tasks[i]
		switch task.Status {
		case models.TaskStatusSuccess, models.TaskStatusCancelled:
			// Terminal.
		case models.TaskStatusFailed:
			ignoreFailure := nodeIgnoreFailure[task.NodeID]
			if !ignoreFailure {
				hasBlockingFailure = true
				if firstErrorMsg == "" {
					firstErrorMsg = task.ErrorMsg
				}
			}
		default:
			allTerminal = false
		}
	}

	if hasBlockingFailure {
		now := time.Now().Unix()
		duration := 0
		if run.StartTime > 0 {
			duration = int(now - run.StartTime)
		}

		updates := map[string]interface{}{
			"status":   "failed",
			"end_time": now,
			"duration": duration,
		}
		if firstErrorMsg != "" {
			if len(firstErrorMsg) > 255 {
				firstErrorMsg = firstErrorMsg[:255]
			}
			updates["error_msg"] = firstErrorMsg
		}

		models.DB.Model(&run).Updates(updates)
		h.BroadcastRunStatus(runID, "failed", firstErrorMsg, duration)
		return
	}

	if allTerminal {
		now := time.Now().Unix()
		duration := 0
		if run.StartTime > 0 {
			duration = int(now - run.StartTime)
		}

		models.DB.Model(&run).Updates(map[string]interface{}{
			"status":   "success",
			"end_time": now,
			"duration": duration,
		})
		h.BroadcastRunStatus(runID, "success", "", duration)
	}
}

// triggerDownstreamTasks checks if downstream tasks should be triggered based on completed tasks
func (h *WebSocketHandler) triggerDownstreamTasks(runID uint64, completedTasks []models.AgentTask) {
	fmt.Printf("[DEBUG] === triggerDownstreamTasks Started ===\n")
	fmt.Printf("[DEBUG] PipelineRun ID: %d\n", runID)
	fmt.Printf("[DEBUG] Total completed tasks: %d\n", len(completedTasks))

	var run models.PipelineRun
	if err := models.DB.First(&run, runID).Error; err != nil {
		fmt.Printf("[DEBUG] PipelineRun %d not found\n", runID)
		return
	}

	if run.Status != "running" {
		fmt.Printf("[DEBUG] PipelineRun %d status is %s, not running, skipping\n", runID, run.Status)
		return
	}

	// Get pipeline config
	var config PipelineConfig
	if run.Config == "" {
		fmt.Printf("[DEBUG] PipelineRun %d has empty config\n", runID)
		return
	}
	if err := json.Unmarshal([]byte(run.Config), &config); err != nil {
		fmt.Printf("[DEBUG] Failed to parse config for run %d: %v\n", runID, err)
		return
	}

	if len(config.Nodes) == 0 || len(config.Edges) == 0 {
		fmt.Printf("[DEBUG] No nodes or edges in config for run %d\n", runID)
		return
	}

	fmt.Printf("[DEBUG] Config has %d nodes, %d edges\n", len(config.Nodes), len(config.Edges))

	// Build node map
	nodeMap := make(map[string]*PipelineNode)
	for i := range config.Nodes {
		nodeMap[config.Nodes[i].ID] = &config.Nodes[i]
	}

	// Build graph with IgnoreFailure info
	graph := make(map[string][]DownstreamEdge)
	for _, node := range config.Nodes {
		graph[node.ID] = []DownstreamEdge{}
	}
	for _, edge := range config.Edges {
		graph[edge.From] = append(graph[edge.From], DownstreamEdge{
			To:            edge.To,
			IgnoreFailure: edge.IgnoreFailure,
		})
		fmt.Printf("[DEBUG] Edge: %s -> %s (ignore_failure=%v)\n", edge.From, edge.To, edge.IgnoreFailure)
	}

	// Build edge map for quick lookup
	edgeMap := make(map[string]map[string]bool)
	// Build upstream edges map for efficient dependency checking
	upstreamEdges := make(map[string][]PipelineEdge)
	for _, edge := range config.Edges {
		if edgeMap[edge.From] == nil {
			edgeMap[edge.From] = make(map[string]bool)
		}
		edgeMap[edge.From][edge.To] = edge.IgnoreFailure
		// Add to upstream edges map
		upstreamEdges[edge.To] = append(upstreamEdges[edge.To], edge)
	}

	// Mark completed tasks by status - only consider tasks that are truly complete
	completedSuccess := make(map[string]bool)
	completedFailed := make(map[string]bool)
	for _, task := range completedTasks {
		fmt.Printf("[DEBUG] Task %d: node_id=%s, status=%s\n", task.ID, task.NodeID, task.Status)
		if task.Status == models.TaskStatusSuccess {
			completedSuccess[task.NodeID] = true
			fmt.Printf("[DEBUG]   -> Node %s marked as successfully completed\n", task.NodeID)
		} else if task.Status == models.TaskStatusFailed {
			completedFailed[task.NodeID] = true
			fmt.Printf("[DEBUG]   -> Node %s marked as failed\n", task.NodeID)
		} else {
			fmt.Printf("[DEBUG]   -> Node %s is in progress (status=%s), not yet complete\n", task.NodeID, task.Status)
		}
	}

	fmt.Printf("[DEBUG] === Execution Summary ===\n")
	fmt.Printf("[DEBUG] Successfully completed nodes: %v\n", completedSuccess)
	fmt.Printf("[DEBUG] Failed completed nodes: %v\n", completedFailed)

	// For each completed task, check if downstream nodes should be triggered
	for _, task := range completedTasks {
		if task.Status != models.TaskStatusSuccess && task.Status != models.TaskStatusFailed {
			continue
		}

		fmt.Printf("[DEBUG] Checking downstream for completed task: node=%s, status=%s\n", task.NodeID, task.Status)

		// Find downstream nodes
		downstreamEdges := graph[task.NodeID]
		for _, edge := range downstreamEdges {
			downstreamID := edge.To

			fmt.Printf("[DEBUG] Checking downstream node: %s\n", downstreamID)

			// Check if this downstream task already exists
			var existingTask models.AgentTask
			if err := models.DB.Where("pipeline_run_id = ? AND node_id = ?", runID, downstreamID).First(&existingTask).Error; err == nil {
				fmt.Printf("[DEBUG] Task for node %s already exists (ID=%d), skipping\n", downstreamID, existingTask.ID)
				continue
			}

			// Check if all upstream tasks are completed
			// If upstream failed but upstream's ignore_failure=true, then downstream can still execute
			allUpstreamCompleted := true

			// Only check edges that point TO this downstream node
			for _, edge := range upstreamEdges[downstreamID] {
				upstreamID := edge.From

				// Get upstream node's IgnoreFailure setting
				upstreamNode := nodeMap[upstreamID]
				upstreamIgnoreFailure := false
				if upstreamNode != nil {
					upstreamIgnoreFailure = upstreamNode.IgnoreFailure
				}

				upstreamSuccess := completedSuccess[upstreamID]
				upstreamFailed := completedFailed[upstreamID]

				fmt.Printf("[DEBUG] Checking upstream %s: success=%v, failed=%v, upstream_ignore_failure=%v\n",
					upstreamID, upstreamSuccess, upstreamFailed, upstreamIgnoreFailure)

				if upstreamSuccess {
					// Upstream succeeded, dependency met
					continue
				} else if upstreamFailed && upstreamIgnoreFailure {
					// Upstream failed but upstream node ignores its own failure, dependency met
					continue
				} else if upstreamFailed && !upstreamIgnoreFailure {
					// Upstream failed and upstream does not ignore failure, blocking downstream
					allUpstreamCompleted = false
					fmt.Printf("[DEBUG] Upstream %s failed with ignore_failure=false, blocking downstream %s\n", upstreamID, downstreamID)
					break
				} else {
					// Upstream is still pending or running - this is NOT an error,
					// we should wait for upstream to complete before triggering downstream
					allUpstreamCompleted = false
					fmt.Printf("[DEBUG] Upstream %s not completed yet (pending/running), waiting...\n", upstreamID)
					break
				}
			}

			if !allUpstreamCompleted {
				// Not all upstream completed, skip triggering this downstream task
				// It will be triggered when upstream completes
				fmt.Printf("[DEBUG] Not all upstream completed for %s, waiting for upstream\n", downstreamID)
				continue
			}

			fmt.Printf("[DEBUG] All upstream completed for %s, triggering task\n", downstreamID)
			node := nodeMap[downstreamID]
			if node == nil {
				fmt.Printf("[DEBUG] Node %s not found in nodeMap\n", downstreamID)
				continue
			}

			// Create task for downstream node
			nodeConfig := node.getNodeConfig()
			script := ""
			taskType := node.Type
			if taskType == "agent" || taskType == "custom" {
				taskType = "shell"
			}
			if taskType == "shell" || taskType == "agent" || taskType == "custom" {
				if s, ok := nodeConfig["script"].(string); ok {
					script = s
				}
			}

			timeout := node.Timeout
			if timeout <= 0 {
				timeout = 3600
			}

			workDir := ""
			if wd, ok := nodeConfig["working_dir"].(string); ok {
				workDir = wd
			}

			envVars := ""
			if env, ok := nodeConfig["env"].(map[string]interface{}); ok {
				if envJSON, err := json.Marshal(env); err == nil {
					envVars = string(envJSON)
				}
			}

			maxRetries := 0
			if retryVal, ok := nodeConfig["retry_count"].(float64); ok && retryVal > 0 {
				maxRetries = int(retryVal)
			}

			newTask := &models.AgentTask{
				AgentID:       run.AgentID,
				PipelineRunID: runID,
				NodeID:        downstreamID,
				TaskType:      taskType,
				Name:          node.Name,
				Params:        "",
				Script:        script,
				WorkDir:       workDir,
				EnvVars:       envVars,
				Status:        models.TaskStatusPending,
				Timeout:       timeout,
				MaxRetries:    maxRetries,
			}

			if err := models.DB.Create(newTask).Error; err == nil {
				fmt.Printf("[SUCCESS] Triggered downstream task %d for node %s\n", newTask.ID, downstreamID)
				h.BroadcastTaskStatus(runID, newTask.ID, newTask.Status, 0, "", "")
				_ = h.sendTaskAssign(*newTask)
			} else {
				fmt.Printf("[ERROR] Failed to create task for node %s: %v\n", downstreamID, err)
			}
		}
	}
}

type DownstreamEdge struct {
	To            string
	IgnoreFailure bool
}

func (h *WebSocketHandler) sendAgentAck(client *wsClient, event string, taskID uint64, attempt int, ok bool, errMsg string, extra map[string]interface{}) {
	if client == nil || client.conn == nil {
		return
	}

	payload := map[string]interface{}{
		"event":     event,
		"task_id":   taskID,
		"attempt":   attempt,
		"ok":        ok,
		"error_msg": errMsg,
		"timestamp": time.Now().Unix(),
	}
	for k, v := range extra {
		payload[k] = v
	}

	msg := WebSocketMessage{
		Type:    "ack_v2",
		Payload: payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	client.mu.Lock()
	_ = client.conn.WriteMessage(websocket.TextMessage, data)
	client.mu.Unlock()
}

func parseTaskUpdatePayloadV2(payload map[string]interface{}) taskUpdatePayloadV2 {
	update := taskUpdatePayloadV2{
		TaskID:         uint64(getFloat64(payload, "task_id")),
		Attempt:        int(getFloat64(payload, "attempt")),
		Status:         getString(payload, "status"),
		ExitCode:       int(getFloat64(payload, "exit_code")),
		ErrorMsg:       getString(payload, "error_msg"),
		DurationMs:     int64(getFloat64(payload, "duration_ms")),
		IdempotencyKey: getString(payload, "idempotency_key"),
		Timestamp:      getInt64(payload, "timestamp"),
	}

	if update.DurationMs <= 0 {
		if sec := int64(getFloat64(payload, "duration")); sec > 0 {
			update.DurationMs = sec * 1000
		}
	}
	if result, ok := payload["result"].(map[string]interface{}); ok {
		update.Result = result
	}
	if update.Timestamp == 0 {
		update.Timestamp = time.Now().Unix()
	}

	return update
}

func parseTaskLogChunkPayloadV2(payload map[string]interface{}) taskLogChunkPayloadV2 {
	chunk := taskLogChunkPayloadV2{
		TaskID:    uint64(getFloat64(payload, "task_id")),
		Attempt:   int(getFloat64(payload, "attempt")),
		Seq:       int64(getFloat64(payload, "seq")),
		Level:     getString(payload, "level"),
		Stream:    getString(payload, "stream"),
		Chunk:     getString(payload, "chunk"),
		Timestamp: getInt64(payload, "timestamp"),
	}
	if chunk.Timestamp == 0 {
		chunk.Timestamp = time.Now().Unix()
	}
	return chunk
}

func isTerminalTaskStatus(status string) bool {
	return status == models.TaskStatusSuccess || status == models.TaskStatusFailed || status == models.TaskStatusCancelled
}

func isValidTaskStatusTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		models.TaskStatusPending: {
			models.TaskStatusRunning:   true,
			models.TaskStatusCancelled: true,
		},
		models.TaskStatusRunning: {
			models.TaskStatusSuccess:   true,
			models.TaskStatusFailed:    true,
			models.TaskStatusCancelled: true,
		},
		models.TaskStatusSuccess:   {},
		models.TaskStatusFailed:    {},
		models.TaskStatusCancelled: {},
	}

	transitions, ok := allowed[from]
	if !ok {
		return false
	}
	return transitions[to]
}

func buildTaskUpdateIdempotencyKey(taskID uint64, attempt int, status string, exitCode int) string {
	return fmt.Sprintf("%d:%d:%s:%d", taskID, attempt, status, exitCode)
}

func buildTaskLogChunkUniqueKey(taskID uint64, attempt int, seq int64) string {
	return fmt.Sprintf("%d:%d:%d", taskID, attempt, seq)
}

func normalizeUnixTimestamp(ts int64) int64 {
	// Convert milliseconds to seconds when needed.
	if ts > 1e12 {
		return ts / 1000
	}
	return ts
}

// IsAgentOnline checks if an agent is connected via WebSocket
func (h *WebSocketHandler) IsAgentOnline(agentID uint64) bool {
	h.agentsMu.RLock()
	defer h.agentsMu.RUnlock()
	_, exists := h.agents[agentID]
	return exists
}

// heartbeatHistory stores heartbeat records in memory (last 50 per agent)
var heartbeatHistory map[uint64][]models.AgentHeartbeat
var heartbeatMu sync.RWMutex
var fileLogChunkSeen sync.Map

func init() {
	heartbeatHistory = make(map[uint64][]models.AgentHeartbeat)
}

func (h *WebSocketHandler) storeHeartbeat(agentID uint64, heartbeat models.AgentHeartbeat) {
	heartbeatMu.Lock()
	defer heartbeatMu.Unlock()

	history := heartbeatHistory[agentID]
	history = append(history, heartbeat)
	if len(history) > 50 {
		history = history[len(history)-50:]
	}
	heartbeatHistory[agentID] = history
}

// GetHeartbeats returns heartbeat history for an agent (from memory)
func (h *WebSocketHandler) GetHeartbeats(agentID uint64, page, pageSize int) ([]models.AgentHeartbeat, int64) {
	heartbeatMu.RLock()
	allHeartbeats := heartbeatHistory[agentID]
	heartbeatMu.RUnlock()

	total := len(allHeartbeats)
	heartbeats := make([]models.AgentHeartbeat, 0, pageSize)

	startIdx := total - (page * pageSize)
	if startIdx < 0 {
		startIdx = 0
	}

	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}

	for i := endIdx - 1; i >= startIdx && len(heartbeats) < pageSize; i-- {
		heartbeats = append(heartbeats, allHeartbeats[i])
	}

	return heartbeats, int64(total)
}
