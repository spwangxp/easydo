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

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		agents:    make(map[uint64]*wsClient),
		frontends: make(map[string]map[string]*frontendClient),
	}
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
}

// handleTaskStatus processes a task status message from an agent
func (h *WebSocketHandler) handleTaskStatus(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	taskID := uint64(getFloat64(payload, "task_id"))
	status := getString(payload, "status")
	exitCode := int(getFloat64(payload, "exit_code"))
	errorMsg := getString(payload, "error_msg")

	// Duration is nested in the result object from agent
	duration := 0
	if result, ok := payload["result"].(map[string]interface{}); ok {
		duration = int(getFloat64(result, "duration"))
	}
	// Fallback to top-level duration if result.duration is not available
	if duration == 0 {
		duration = int(getFloat64(payload, "duration"))
	}

	if taskID == 0 {
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err != nil {
		return
	}

	now := time.Now().Unix()
	task.Status = status
	task.EndTime = now
	// 使用 agent 传递的 duration，如果为0则使用服务器计算的值
	if duration > 0 {
		task.Duration = duration
	} else if task.StartTime > 0 {
		task.Duration = int(now - task.StartTime)
	}
	task.ExitCode = exitCode
	task.ErrorMsg = errorMsg
	models.DB.Save(&task)

	// Update agent status
	h.checkAgentStatus(client.agentID)

	// Create execution record
	execution := &models.TaskExecution{
		TaskID:    taskID,
		Attempt:   task.RetryCount + 1,
		Status:    status,
		StartTime: task.StartTime,
		EndTime:   now,
		Duration:  task.Duration,
		ExitCode:  exitCode,
		ErrorMsg:  errorMsg,
	}
	models.DB.Create(execution)

	// Check and update pipeline status if task is completed
	if status == models.TaskStatusSuccess || status == models.TaskStatusFailed {
		var completedTasks []models.AgentTask
		models.DB.Where("pipeline_run_id = ?", task.PipelineRunID).Find(&completedTasks)
		h.triggerDownstreamTasks(task.PipelineRunID, completedTasks)
		h.checkAndUpdatePipelineStatus(task.PipelineRunID)
	}

	// Broadcast to frontend clients subscribed to this run
	h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
		"task_id":    taskID,
		"run_id":     task.PipelineRunID,
		"status":     status,
		"exit_code":  exitCode,
		"error_msg":  errorMsg,
		"duration":   task.Duration,
		"agent_id":   client.agentID,
		"agent_name": agent.Name,
		"timestamp":  now,
	})
}

// handleTaskLog processes a task log message from an agent
func (h *WebSocketHandler) handleTaskLog(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	taskID := uint64(getFloat64(payload, "task_id"))
	level := getString(payload, "level")
	message := getString(payload, "message")
	source := getString(payload, "source")
	lineNumber := int(getFloat64(payload, "line_number"))
	timestamp := getInt64(payload, "timestamp")
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	if taskID == 0 {
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err != nil {
		return
	}

	log := &models.AgentLog{
		TaskID:    taskID,
		Level:     level,
		Message:   message,
		Timestamp: timestamp,
		Source:    source,
	}
	models.DB.Create(log)

	// Broadcast to frontend clients subscribed to this run
	h.broadcastToFrontend(task.PipelineRunID, "task_log", map[string]interface{}{
		"log_id":      log.ID,
		"task_id":     taskID,
		"run_id":      task.PipelineRunID,
		"level":       level,
		"message":     message,
		"source":      source,
		"line_number": lineNumber,
		"timestamp":   timestamp,
	})
}

// handleTaskLogStream handles streaming task log chunks from an agent
func (h *WebSocketHandler) handleTaskLogStream(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	taskID := uint64(getFloat64(payload, "task_id"))
	chunk := getString(payload, "chunk")
	timestamp := getInt64(payload, "timestamp")
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	if taskID == 0 {
		return
	}

	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err != nil {
		return
	}

	h.broadcastToFrontend(task.PipelineRunID, "task_log_stream", map[string]interface{}{
		"task_id":   taskID,
		"run_id":    task.PipelineRunID,
		"chunk":     chunk,
		"timestamp": timestamp,
	})
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
	fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus called for runID=%d\n", runID)

	var run models.PipelineRun
	if err := models.DB.First(&run, runID).Error; err != nil {
		fmt.Printf("[DEBUG] PipelineRun %d not found\n", runID)
		return
	}

	// Only check if run is still running
	if run.Status != "running" {
		fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: run %d status is %s, not running, skipping\n", runID, run.Status)
		return
	}

	// Get all tasks for this run
	var tasks []models.AgentTask
	models.DB.Where("pipeline_run_id = ?", runID).Find(&tasks)

	fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: found %d tasks\n", len(tasks))

	if len(tasks) == 0 {
		return
	}

	// Parse pipeline config to check IgnoreFailure settings
	var config PipelineConfig
	configParsed := false
	if err := json.Unmarshal([]byte(run.Config), &config); err == nil {
		configParsed = true
	}

	// Collect all failed tasks (not just the first one)
	var failedTasks []models.AgentTask
	for i := range tasks {
		if tasks[i].Status == models.TaskStatusFailed {
			failedTasks = append(failedTasks, tasks[i])
		}
	}

	hasFailed := len(failedTasks) > 0

	// Build node map for quick lookup
	nodeMap := make(map[string]*PipelineNode)
	for i := range config.Nodes {
		nodeMap[config.Nodes[i].ID] = &config.Nodes[i]
	}

	// If any task failed, check if the failure should block the pipeline
	if hasFailed && configParsed {
		// Check all failed tasks to determine if any blocks execution
		hasBlockingFailure := false

		// Map downstream nodes affected by failed tasks
		// If upstream (failed) node has ignore_failure=false, downstream will be blocked
		blockedDownstream := make(map[string]bool)
		for _, failedTask := range failedTasks {
			fmt.Printf("[DEBUG] Checking failed task: %s\n", failedTask.NodeID)
			// Get the failed task node's IgnoreFailure setting
			failedNode := nodeMap[failedTask.NodeID]
			failedNodeIgnoreFailure := false
			if failedNode != nil {
				failedNodeIgnoreFailure = failedNode.IgnoreFailure
			}

			// If failed node does NOT ignore its own failure, then downstream is blocked
			if !failedNodeIgnoreFailure {
				for _, edge := range config.Edges {
					if edge.From == failedTask.NodeID {
						downstreamNodeID := edge.To
						blockedDownstream[downstreamNodeID] = true
						fmt.Printf("[DEBUG] Upstream task %s failed with ignore_failure=false, downstream %s will be blocked\n",
							failedTask.NodeID, downstreamNodeID)
					}
				}
			}
		}

		// Check if any downstream node was blocked due to upstream failure
		for nodeID, isBlocked := range blockedDownstream {
			if isBlocked {
				found := false
				for _, task := range tasks {
					if task.NodeID == nodeID {
						found = true
						if task.Status == models.TaskStatusPending || task.Status == models.TaskStatusRunning {
							hasBlockingFailure = true
							fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: downstream task %s was not triggered due to upstream failure (ignore_failure=false)\n", nodeID)
						}
						break
					}
				}
				if !found {
					hasBlockingFailure = true
					fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: downstream task %s was never created due to upstream failure (ignore_failure=false)\n", nodeID)
				}
			}
		}

		// Check for pending/running tasks that depend on failed tasks
		// If upstream (failed) node has ignore_failure=false, running downstream should be cancelled
		for _, failedTask := range failedTasks {
			failedNode := nodeMap[failedTask.NodeID]
			failedNodeIgnoreFailure := false
			if failedNode != nil {
				failedNodeIgnoreFailure = failedNode.IgnoreFailure
			}

			if !failedNodeIgnoreFailure {
				for _, edge := range config.Edges {
					if edge.From == failedTask.NodeID {
						downstreamNodeID := edge.To
						for _, task := range tasks {
							if task.NodeID == downstreamNodeID && (task.Status == models.TaskStatusPending || task.Status == models.TaskStatusRunning) {
								hasBlockingFailure = true
								fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: upstream %s failed (ignore_failure=false), cancelling downstream %s\n",
									failedTask.NodeID, task.NodeID)

								if task.Status == models.TaskStatusRunning {
									task.Status = models.TaskStatusCancelled
									task.ErrorMsg = fmt.Sprintf("Cancelled due to upstream task %s failure (upstream does not ignore failure)", failedTask.NodeID)
									models.DB.Save(&task)
									fmt.Printf("[DEBUG] Cancelled running task %s due to upstream failure\n", task.NodeID)
								}
							}
						}
					}
				}
			}
		}

		if !hasBlockingFailure {
			// No blocking failures, check if all tasks are completed
			allCompleted := true
			for _, task := range tasks {
				if task.Status != models.TaskStatusSuccess && task.Status != models.TaskStatusFailed && task.Status != models.TaskStatusCancelled {
					allCompleted = false
					break
				}
			}

			if allCompleted {
				// All tasks completed, mark as success (failed tasks had IgnoreFailure=true)
				now := time.Now().Unix()
				updates := map[string]interface{}{
					"status":   "success",
					"end_time": now,
				}
				if run.StartTime > 0 {
					updates["duration"] = int(now - run.StartTime)
				}
				models.DB.Model(&run).Updates(updates)
				h.BroadcastRunStatus(runID, "success", "", int(now-run.StartTime))
				fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: run %d marked as success (all tasks completed, failures ignored)\n", runID)
				return
			}
			// If not all completed, keep as running (some tasks may be waiting on IgnoreFailure edges)
			fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: run %d has failures but no blocking failures, keeping as running\n", runID)
			return
		}
		// hasBlockingFailure is true, fall through to mark as failed
	}

	// If any task failed (and it wasn't ignored), mark the entire run as failed
	if hasFailed {
		now := time.Now().Unix()
		updates := map[string]interface{}{
			"status":   "failed",
			"end_time": now,
		}

		// Calculate duration
		var duration int
		if run.StartTime > 0 {
			duration = int(now - run.StartTime)
		}
		updates["duration"] = duration

		// Store error message from the first failed task
		if len(failedTasks) > 0 && failedTasks[0].ErrorMsg != "" {
			errorMsg := failedTasks[0].ErrorMsg
			if len(errorMsg) > 255 {
				errorMsg = errorMsg[:255]
			}
			updates["error_msg"] = errorMsg
		}

		models.DB.Model(&run).Updates(updates)

		// Broadcast run status to frontend with duration
		h.BroadcastRunStatus(runID, "failed", "", duration)
		fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: run %d marked as failed\n", runID)
	} else {
		// Check if all tasks are completed (success or failed)
		allCompleted := true
		completedCount := 0
		for _, task := range tasks {
			fmt.Printf("[DEBUG] Task %d: node=%s, status=%s\n", task.ID, task.NodeID, task.Status)
			if task.Status != models.TaskStatusSuccess && task.Status != models.TaskStatusFailed {
				allCompleted = false
			} else {
				completedCount++
			}
		}

		fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: allCompleted=%v, completedCount=%d, total=%d\n", allCompleted, completedCount, len(tasks))

		// If all tasks completed and none failed, mark run as success
		if allCompleted {
			now := time.Now().Unix()
			updates := map[string]interface{}{
				"status":   "success",
				"end_time": now,
			}

			// Calculate duration
			var duration int
			if run.StartTime > 0 {
				duration = int(now - run.StartTime)
			}
			updates["duration"] = duration

			models.DB.Model(&run).Updates(updates)

			// Broadcast run status to frontend with duration
			h.BroadcastRunStatus(runID, "success", "", duration)
			fmt.Printf("[DEBUG] checkAndUpdatePipelineStatus: run %d marked as success\n", runID)
		}
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
			}

			if err := models.DB.Create(newTask).Error; err == nil {
				fmt.Printf("[SUCCESS] Triggered downstream task %d for node %s\n", newTask.ID, downstreamID)
				h.BroadcastTaskStatus(runID, newTask.ID, newTask.Status, 0, "", "")
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
