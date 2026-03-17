package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WebSocketHandler owns the process-local realtime runtime.
//
// It is intentionally responsible only for live socket/session coordination and
// short-lived watchers. Durable/shared truth still lives in MySQL and Redis.
// The distinction matters in multi-replica mode: a process may die at any time,
// so anything stored here must be reconstructable from shared state.
type WebSocketHandler struct {
	agents          map[uint64]*wsClient
	agentsMu        sync.RWMutex
	frontends       map[string]map[string]*frontendClient // key: runID, value: map of clientID->client
	frontendsMu     sync.RWMutex
	runWatchers     map[uint64]*runWatcher
	runWatchersMu   sync.Mutex
	clientIDCounter uint64
	clientIDMu      sync.Mutex
}

type runWatcher struct {
	runID      uint64
	cancel     context.CancelFunc
	runStatus  string
	errorMsg   string
	duration   int
	taskStates map[uint64]taskRealtimeState
	mu         sync.Mutex
}

type taskRealtimeState struct {
	Status    string
	ExitCode  int
	ErrorMsg  string
	Duration  int64
	StartTime int64
	AgentName string
}

var (
	sharedWebSocketHandler     *WebSocketHandler
	sharedWebSocketHandlerOnce sync.Once
)

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		agents:      make(map[uint64]*wsClient),
		frontends:   make(map[string]map[string]*frontendClient),
		runWatchers: make(map[uint64]*runWatcher),
	}
}

// SharedWebSocketHandler returns the singleton realtime runtime for this server
// process.
//
// Runtime handlers, schedulers, and routers must all use the same instance.
// Creating ad-hoc handlers would split the in-memory socket maps and break
// dispatch, frontend fan-out, and reconnect recovery semantics.
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
	sessionID   string
	serverID    string
	lastHeartAt int64
	cancel      context.CancelFunc
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
	registerKey := c.Query("register_key")

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

	switch agent.RegistrationStatus {
	case models.AgentRegistrationStatusApproved:
		if token != "" && agent.Token == token {
			break
		}
		if registerKey != "" && agent.RegisterKey != "" && agent.RegisterKey == registerKey {
			break
		}
		if token == "" && registerKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "missing recovery credential",
			})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "invalid token",
		})
		return
	case models.AgentRegistrationStatusPending:
		if registerKey == "" || agent.RegisterKey == "" || agent.RegisterKey != registerKey {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "invalid register_key",
			})
			return
		}
	default:
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "agent registration status not allowed",
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade failed: %v\n", err)
		return
	}

	streamCtx, streamCancel := context.WithCancel(context.Background())

	client := &wsClient{
		conn:        conn,
		agentID:     agentID,
		sessionID:   uuid.NewString(),
		serverID:    utils.ServerID(),
		lastHeartAt: time.Now().Unix(),
		cancel:      streamCancel,
	}

	h.agentsMu.Lock()
	h.agents[agentID] = client
	h.agentsMu.Unlock()

	fmt.Printf("Agent %d connected via WebSocket\n", agentID)
	h.rebindExecutionTasksForReconnect(client, &agent)

	models.DB.Model(agent).Updates(map[string]interface{}{
		"status":        models.AgentStatusOnline,
		"last_heart_at": time.Now().Unix(),
	})

	_ = utils.PutAgentPresence(streamCtx, utils.AgentPresence{
		AgentID:           agentID,
		AgentSessionID:    client.sessionID,
		ServerID:          client.serverID,
		ServerURL:         utils.ServerInternalURL(),
		Status:            agent.Status,
		LastHeartbeatAt:   time.Now().Unix(),
		HeartbeatInterval: agent.HeartbeatInterval,
	})
	go h.consumeAgentStream(streamCtx, client)
	h.redrivePendingTasksForConnectedAgent(client)

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

	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing token"})
		return
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid token"})
		return
	}
	if err := middleware.ValidateTokenSession(c.Request.Context(), claims); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "session expired"})
		return
	}
	userID := claims.UserID
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid user"})
		return
	}
	runIDNum, err := strconv.ParseUint(runID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid run_id"})
		return
	}
	var run models.PipelineRun
	if err := models.DB.First(&run, runIDNum).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "run not found"})
		return
	}
	var memberCount int64
	models.DB.Model(&models.WorkspaceMember{}).
		Where("workspace_id = ? AND user_id = ? AND status = ?", run.WorkspaceID, userID, models.WorkspaceMemberStatusActive).
		Count(&memberCount)
	if memberCount == 0 && !isAdminRole(claims.Role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "forbidden"})
		return
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
	h.ensureRunWatcher(runIDNum)

	fmt.Printf("Frontend client %s connected for run %s\n", clientID, runID)

	h.handleFrontendMessages(client, runID, clientID)
}

// handleAgentMessages handles incoming messages from an agent
func (h *WebSocketHandler) handleAgentMessages(client *wsClient, agent *models.Agent) {
	defer func() {
		h.cleanupAgentConnection(client, agent)
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
		case "pull_task":
			h.handleAgentPullTask(client, agent, msg.Payload)
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
				h.stopRunWatcher(runID)
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

func (h *WebSocketHandler) ensureRunWatcher(runID uint64) {
	if runID == 0 {
		return
	}
	h.runWatchersMu.Lock()
	defer h.runWatchersMu.Unlock()
	if _, exists := h.runWatchers[runID]; exists {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	watcher := &runWatcher{
		runID:      runID,
		cancel:     cancel,
		taskStates: make(map[uint64]taskRealtimeState),
	}
	h.runWatchers[runID] = watcher
	go h.runWatcherLoop(ctx, watcher)
}

func (h *WebSocketHandler) stopRunWatcher(runID string) {
	runIDNum, err := strconv.ParseUint(runID, 10, 64)
	if err != nil || runIDNum == 0 {
		return
	}
	h.runWatchersMu.Lock()
	watcher, exists := h.runWatchers[runIDNum]
	if exists {
		delete(h.runWatchers, runIDNum)
	}
	h.runWatchersMu.Unlock()
	if exists && watcher.cancel != nil {
		watcher.cancel()
	}
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case models.PipelineRunStatusSuccess, models.PipelineRunStatusFailed, models.PipelineRunStatusCancelled:
		return true
	default:
		return false
	}
}

func syncLiveTaskStateFromTask(task *models.AgentTask, agentName string) {
	if task == nil || task.ID == 0 || utils.RedisClient == nil {
		return
	}
	seq, err := utils.NextLiveTaskSeq(context.Background(), task.ID)
	if err != nil {
		return
	}
	_ = utils.SaveLiveTaskState(context.Background(), utils.LiveTaskState{
		TaskID:          task.ID,
		RunID:           task.PipelineRunID,
		NodeID:          task.NodeID,
		Status:          task.Status,
		Seq:             seq,
		OwnerServerID:   task.OwnerServerID,
		AgentSessionID:  task.AgentSessionID,
		DispatchAttempt: task.DispatchAttempt,
		RetryCount:      task.RetryCount,
		StartTime:       task.StartTime,
		EndTime:         task.EndTime,
		Duration:        task.Duration,
		ExitCode:        task.ExitCode,
		ErrorMsg:        task.ErrorMsg,
		AgentName:       agentName,
		UpdatedAt:       time.Now().Unix(),
	}, models.IsTerminalTaskStatus(task.Status))
}

func syncLiveRunStateFromRun(run *models.PipelineRun) {
	if run == nil || run.ID == 0 || utils.RedisClient == nil {
		return
	}
	seq, err := utils.NextLiveRunSeq(context.Background(), run.ID)
	if err != nil {
		return
	}
	_ = utils.SaveLiveRunState(context.Background(), utils.LiveRunState{
		RunID:         run.ID,
		Status:        run.Status,
		Seq:           seq,
		OwnerServerID: utils.ServerID(),
		Duration:      run.Duration,
		ErrorMsg:      run.ErrorMsg,
		UpdatedAt:     time.Now().Unix(),
	}, isTerminalRunStatus(run.Status))
}

func (h *WebSocketHandler) runWatcherLoop(ctx context.Context, watcher *runWatcher) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	h.pollRunWatcherState(watcher)
	for {
		select {
		case <-ticker.C:
			h.pollRunWatcherState(watcher)
		case <-ctx.Done():
			return
		}
	}
}

func (h *WebSocketHandler) pollRunWatcherState(watcher *runWatcher) {
	if watcher == nil || watcher.runID == 0 {
		return
	}
	if liveRunState, err := utils.GetLiveRunState(context.Background(), watcher.runID); err == nil && liveRunState != nil {
		watcher.mu.Lock()
		runChanged := watcher.runStatus != liveRunState.Status || watcher.errorMsg != liveRunState.ErrorMsg || watcher.duration != liveRunState.Duration
		watcher.runStatus = liveRunState.Status
		watcher.errorMsg = liveRunState.ErrorMsg
		watcher.duration = liveRunState.Duration
		watcher.mu.Unlock()
		if runChanged {
			h.broadcastToFrontend(watcher.runID, "run_status", map[string]interface{}{
				"run_id":    watcher.runID,
				"status":    liveRunState.Status,
				"error_msg": liveRunState.ErrorMsg,
				"duration":  liveRunState.Duration,
				"timestamp": time.Now().Unix(),
			})
		}
	} else {
		var run models.PipelineRun
		if err := models.DB.First(&run, watcher.runID).Error; err == nil {
			watcher.mu.Lock()
			runChanged := watcher.runStatus != run.Status || watcher.errorMsg != run.ErrorMsg || watcher.duration != int(run.Duration)
			watcher.runStatus = run.Status
			watcher.errorMsg = run.ErrorMsg
			watcher.duration = int(run.Duration)
			watcher.mu.Unlock()
			if runChanged {
				h.broadcastToFrontend(watcher.runID, "run_status", map[string]interface{}{
					"run_id":    watcher.runID,
					"status":    run.Status,
					"error_msg": run.ErrorMsg,
					"duration":  run.Duration,
					"timestamp": time.Now().Unix(),
				})
			}
		}
	}

	liveTaskStates, complete, err := utils.GetLiveTaskStatesForRun(context.Background(), watcher.runID)
	if err == nil && complete && len(liveTaskStates) > 0 {
		for _, taskState := range liveTaskStates {
			state := taskRealtimeState{
				Status:    taskState.Status,
				ExitCode:  taskState.ExitCode,
				ErrorMsg:  taskState.ErrorMsg,
				Duration:  int64(taskState.Duration),
				StartTime: taskState.StartTime,
				AgentName: taskState.AgentName,
			}
			watcher.mu.Lock()
			previous, exists := watcher.taskStates[taskState.TaskID]
			changed := !exists || previous != state
			watcher.taskStates[taskState.TaskID] = state
			watcher.mu.Unlock()
			if changed {
				h.broadcastToFrontend(watcher.runID, "task_status", map[string]interface{}{
					"task_id":    taskState.TaskID,
					"node_id":    taskState.NodeID,
					"run_id":     watcher.runID,
					"status":     taskState.Status,
					"exit_code":  taskState.ExitCode,
					"error_msg":  taskState.ErrorMsg,
					"duration":   taskState.Duration,
					"start_time": taskState.StartTime,
					"agent_name": taskState.AgentName,
					"timestamp":  time.Now().Unix(),
				})
			}
		}
		return
	}

	var tasks []models.AgentTask
	if err := models.DB.Preload("Agent").Where("pipeline_run_id = ?", watcher.runID).Order("id ASC").Find(&tasks).Error; err != nil {
		return
	}
	for _, task := range tasks {
		state := taskRealtimeState{Status: task.Status, ExitCode: task.ExitCode, ErrorMsg: task.ErrorMsg, Duration: int64(task.Duration), StartTime: task.StartTime}
		if task.Agent != nil {
			state.AgentName = task.Agent.Name
		}
		watcher.mu.Lock()
		previous, exists := watcher.taskStates[task.ID]
		changed := !exists || previous != state
		watcher.taskStates[task.ID] = state
		watcher.mu.Unlock()
		if changed {
			h.broadcastToFrontend(watcher.runID, "task_status", map[string]interface{}{
				"task_id":    task.ID,
				"node_id":    task.NodeID,
				"run_id":     watcher.runID,
				"status":     task.Status,
				"exit_code":  task.ExitCode,
				"error_msg":  task.ErrorMsg,
				"duration":   task.Duration,
				"start_time": task.StartTime,
				"agent_name": state.AgentName,
				"timestamp":  time.Now().Unix(),
			})
		}
	}
}

// handleAgentHeartbeat processes a heartbeat message from an agent
func (h *WebSocketHandler) handleAgentHeartbeat(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	var latest models.Agent
	if err := models.DB.Where("id = ?", client.agentID).First(&latest).Error; err != nil {
		return
	}

	agentTimestamp := getInt64(payload, "timestamp")
	if agentTimestamp == 0 {
		agentTimestamp = time.Now().Unix()
	}

	client.lastHeartAt = agentTimestamp

	newSuccessCount := latest.ConsecutiveSuccess + 1
	if newSuccessCount > 3 {
		newSuccessCount = 3
	}

	updates := map[string]interface{}{
		"last_heart_at":        agentTimestamp,
		"consecutive_success":  newSuccessCount,
		"consecutive_failures": 0,
	}

	if latest.Status != models.AgentStatusOnline && newSuccessCount >= 3 {
		updates["status"] = models.AgentStatusOnline
		fmt.Printf("Agent %d status updated to online\n", client.agentID)
	}

	models.DB.Model(&latest).Updates(updates)
	if status, ok := updates["status"].(string); ok {
		latest.Status = status
	}
	latest.ConsecutiveSuccess = newSuccessCount
	latest.LastHeartAt = agentTimestamp
	if agent != nil {
		*agent = latest
	}
	_, _ = h.reconcileDispatchTimeouts(models.DB, time.Now().Unix())
	_ = utils.PutAgentPresence(context.Background(), utils.AgentPresence{
		AgentID:           client.agentID,
		AgentSessionID:    client.sessionID,
		ServerID:          client.serverID,
		ServerURL:         utils.ServerInternalURL(),
		Status:            latest.Status,
		LastHeartbeatAt:   agentTimestamp,
		HeartbeatInterval: latest.HeartbeatInterval,
		CPUUsage:          getFloat64(payload, "cpu_usage"),
		MemoryUsage:       getFloat64(payload, "memory_usage"),
		DiskUsage:         getFloat64(payload, "disk_usage"),
		TasksRunning:      int(getFloat64(payload, "tasks_running")),
	})
	h.checkAgentStatus(client.agentID)

	recordAgentHeartbeat(models.DB, h, models.AgentHeartbeat{
		AgentID:      client.agentID,
		Timestamp:    agentTimestamp,
		CPUUsage:     getFloat64(payload, "cpu_usage"),
		MemoryUsage:  getFloat64(payload, "memory_usage"),
		DiskUsage:    getFloat64(payload, "disk_usage"),
		LoadAvg:      getString(payload, "load_avg"),
		TasksRunning: int(getFloat64(payload, "tasks_running")),
	})

	// Get pending tasks
	var pendingTasks []models.AgentTask
	if latest.RegistrationStatus == models.AgentRegistrationStatusApproved {
		models.DB.Where("agent_id = ? AND status IN ?", client.agentID, []string{models.TaskStatusAssigned, models.TaskStatusDispatching, models.TaskStatusPulling}).Find(&pendingTasks)
	}

	response := WebSocketMessage{Type: "heartbeat_ack", Payload: h.buildHeartbeatAckPayload(client, &latest, len(pendingTasks))}

	responseData, _ := json.Marshal(response)
	client.mu.Lock()
	client.conn.WriteMessage(websocket.TextMessage, responseData)
	client.mu.Unlock()

	h.redrivePendingTasksForConnectedAgent(client)
	go NewPipelineHandler().scheduleQueuedPipelineRuns(models.DB)
}

func (h *WebSocketHandler) buildHeartbeatAckPayload(client *wsClient, agent *models.Agent, pendingTasks int) map[string]interface{} {
	payload := map[string]interface{}{
		"status":              "ok",
		"server_time":         time.Now().Unix(),
		"pending_tasks":       pendingTasks,
		"heartbeat_interval":  agent.HeartbeatInterval,
		"agent_session_id":    client.sessionID,
		"server_id":           client.serverID,
		"registration_status": agent.RegistrationStatus,
	}
	if agent.RegistrationStatus == models.AgentRegistrationStatusApproved && agent.Token != "" {
		payload["token"] = agent.Token
	}
	return payload
}

func (h *WebSocketHandler) cleanupAgentConnection(client *wsClient, agent *models.Agent) {
	if client == nil {
		return
	}
	if client.cancel != nil {
		client.cancel()
	}

	isCurrentSession := false
	h.agentsMu.Lock()
	if existing, ok := h.agents[client.agentID]; ok && existing != nil && existing.sessionID == client.sessionID {
		delete(h.agents, client.agentID)
		isCurrentSession = true
	}
	h.agentsMu.Unlock()

	_ = utils.DeleteAgentPresence(context.Background(), client.agentID, client.sessionID)

	shouldOffline := isCurrentSession
	if presence, err := utils.GetAgentPresence(context.Background(), client.agentID); err == nil {
		shouldOffline = presence.AgentSessionID == client.sessionID
	} else if err == redis.Nil {
		shouldOffline = isCurrentSession
	}

	if shouldOffline {
		agentRef := &models.Agent{}
		if agent != nil && agent.ID != 0 {
			agentRef = agent
		}
		models.DB.Model(agentRef).Where("id = ?", client.agentID).Updates(map[string]interface{}{
			"status": models.AgentStatusOffline,
		})
	}

	client.mu.Lock()
	if client.conn != nil {
		_ = client.conn.Close()
	}
	client.mu.Unlock()

	fmt.Printf("Agent %d disconnected\n", client.agentID)
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
	if task.AgentSessionID != "" && task.AgentSessionID != client.sessionID && task.Status != models.TaskStatusAssigned && task.Status != models.TaskStatusDispatching && task.Status != models.TaskStatusPulling {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "stale agent session", nil)
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

	if !models.IsTaskStatusTransitionAllowed(task.Status, update.Status) {
		h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "invalid status transition", nil)
		return
	}

	event := &models.AgentTaskEvent{
		TaskID:         update.TaskID,
		PipelineRunID:  task.PipelineRunID,
		AgentID:        client.agentID,
		AgentSessionID: client.sessionID,
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

	isTerminal := models.IsTerminalTaskStatus(update.Status)
	willRetry := update.Status == models.TaskStatusExecuteFailed && task.RetryCount < task.MaxRetries
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
	startTimeForFrontend := task.StartTime
	if v, ok := updates["start_time"].(int64); ok {
		startTimeForFrontend = v
	}
	task.Status = update.Status
	task.ExitCode = update.ExitCode
	task.ErrorMsg = update.ErrorMsg
	task.StartTime = startTimeForFrontend
	if v, ok := updates["end_time"].(int64); ok {
		task.EndTime = v
	}
	task.Duration = durationForFrontend
	if !willRetry {
		syncLiveTaskStateFromTask(&task, agent.Name)
	}

	h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
		"task_id":    update.TaskID,
		"node_id":    task.NodeID,
		"run_id":     task.PipelineRunID,
		"status":     update.Status,
		"start_time": startTimeForFrontend,
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
	if willRetry {
		nextRetry := task.RetryCount + 1
		retryUpdates := map[string]interface{}{
			"status":           models.TaskStatusQueued,
			"retry_count":      nextRetry,
			"start_time":       int64(0),
			"end_time":         int64(0),
			"duration":         0,
			"exit_code":        0,
			"error_msg":        "",
			"result_data":      "",
			"dispatch_token":   "",
			"dispatch_attempt": 0,
			"lease_expire_at":  int64(0),
			"agent_session_id": "",
			"owner_server_id":  "",
		}
		if err := models.DB.Model(&task).Updates(retryUpdates).Error; err != nil {
			h.sendAgentAck(client, "task_update_v2", update.TaskID, update.Attempt, false, "failed to enqueue retry", nil)
			return
		}

		task.Status = models.TaskStatusQueued
		task.RetryCount = nextRetry
		task.StartTime = 0
		task.EndTime = 0
		task.Duration = 0
		task.ExitCode = 0
		task.ErrorMsg = ""
		task.DispatchAttempt = 0
		task.LeaseExpireAt = 0
		task.AgentSessionID = ""
		task.OwnerServerID = ""
		syncLiveTaskStateFromTask(&task, agent.Name)

		h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
			"task_id":     task.ID,
			"node_id":     task.NodeID,
			"run_id":      task.PipelineRunID,
			"status":      models.TaskStatusQueued,
			"start_time":  int64(0),
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

		go NewPipelineHandler().scheduleQueuedPipelineRuns(models.DB)
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
	if task.AgentSessionID != "" && task.AgentSessionID != client.sessionID {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "stale agent session", nil)
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
	created, err := appendTaskLogChunk(h, task, logChunk, client.agentID, client.sessionID)
	if err != nil {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, false, "failed to persist task log chunk", nil)
		return
	}
	if !created {
		h.sendAgentAck(client, "task_log_chunk_v2", logChunk.TaskID, logChunk.Attempt, true, "", map[string]interface{}{
			"seq":       logChunk.Seq,
			"duplicate": true,
		})
		return
	}

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
	if task.AgentSessionID != "" && task.AgentSessionID != client.sessionID {
		h.sendAgentAck(client, "task_log_end_v2", taskID, attempt, false, "stale agent session", nil)
		return
	}
	if attempt <= 0 {
		attempt = task.RetryCount + 1
	}
	_ = agentFileLogs.FinishTask(taskID, attempt)

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
	dispatchToken := task.DispatchToken
	if dispatchToken == "" {
		dispatchToken = uuid.NewString()
	}
	dispatchAttempt := task.DispatchAttempt + 1
	now := time.Now().Unix()
	leaseTTL := int64(120)
	updates := map[string]interface{}{
		"status":           models.TaskStatusDispatching,
		"dispatch_token":   dispatchToken,
		"dispatch_attempt": dispatchAttempt,
		"lease_expire_at":  now + leaseTTL,
	}
	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.AgentTask{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return err
		}
		_, err := utils.PublishAgentStreamEvent(context.Background(), task.AgentID, utils.AgentStreamEvent{
			TaskID:          task.ID,
			DispatchToken:   dispatchToken,
			DispatchAttempt: dispatchAttempt,
			CreatedAt:       now,
		})
		return err
	}); err != nil {
		return false
	}
	task.Status = models.TaskStatusDispatching
	task.DispatchToken = dispatchToken
	task.DispatchAttempt = dispatchAttempt
	task.LeaseExpireAt = now + leaseTTL
	syncLiveTaskStateFromTask(&task, "")
	h.BroadcastTaskStatus(task.PipelineRunID, task.ID, task.NodeID, task.Status, 0, "", "")
	return true
}

func (h *WebSocketHandler) dispatchPendingTasks(agentID uint64) int {
	if agentID == 0 {
		return 0
	}

	var pendingTasks []models.AgentTask
	models.DB.Where("agent_id = ? AND status IN ?", agentID, []string{models.TaskStatusQueued, models.TaskStatusAssigned, models.TaskStatusDispatchTimeout}).
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

// consumeAgentStream reads the per-agent Redis stream and turns valid stream
// events into live WS wakeups.
//
// Redis stream delivery is intentionally treated as at-least-once. That means a
// stream message alone is never trusted as sufficient proof that a task should
// still be woken up. Every message is revalidated against current DB task state
// and current Redis presence before it can drive a pull_task_now message.
func (h *WebSocketHandler) consumeAgentStream(ctx context.Context, client *wsClient) {
	if client == nil || client.agentID == 0 {
		return
	}
	lastID := "0"
	stream := utils.AgentStreamKey(client.agentID)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		streams, err := utils.RedisClient.XRead(ctx, &redis.XReadArgs{
			Streams: []string{stream, lastID},
			Block:   3 * time.Second,
			Count:   16,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, result := range streams {
			for _, msg := range result.Messages {
				lastID = msg.ID
				event, parseErr := utils.ParseAgentStreamEvent(msg)
				if parseErr != nil {
					continue
				}
				var task models.AgentTask
				if err := models.DB.First(&task, event.TaskID).Error; err != nil {
					continue
				}
				if !shouldDispatchAgentStreamEvent(&task, event) {
					continue
				}
				presence, presenceErr := utils.GetAgentPresence(ctx, client.agentID)
				if presenceErr != nil || presence == nil {
					continue
				}
				if presence.ServerID != client.serverID || presence.AgentSessionID != client.sessionID {
					continue
				}
				_ = h.sendPullTaskNow(client, event.TaskID, event.DispatchToken, event.DispatchAttempt)
			}
		}
	}
}

// sendPullTaskNow performs the owner-server -> agent wakeup step for a task that
// is already in the dispatch chain.
//
// The conditional status update is the important safety fence here. Only tasks
// still in dispatch-stage states may move to `pulling`; execution-stage tasks
// must never be downgraded back into dispatch flow during reconnect/failover.
func (h *WebSocketHandler) sendPullTaskNow(client *wsClient, taskID uint64, dispatchToken string, dispatchAttempt int) bool {
	if client == nil || client.conn == nil || taskID == 0 || dispatchToken == "" {
		return false
	}
	result := models.DB.Model(&models.AgentTask{}).Where("id = ? AND dispatch_token = ? AND status IN ?", taskID, dispatchToken, []string{models.TaskStatusAssigned, models.TaskStatusDispatching, models.TaskStatusPulling}).Updates(map[string]interface{}{
		"status":           models.TaskStatusPulling,
		"agent_session_id": client.sessionID,
		"owner_server_id":  client.serverID,
	})
	if result.Error != nil || result.RowsAffected == 0 {
		return false
	}
	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err == nil {
		syncLiveTaskStateFromTask(&task, "")
	}
	msg := WebSocketMessage{
		Type: "pull_task_now",
		Payload: map[string]interface{}{
			"task_id":          taskID,
			"dispatch_token":   dispatchToken,
			"dispatch_attempt": dispatchAttempt,
			"timestamp":        time.Now().Unix(),
		},
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

// redrivePendingTasksForConnectedAgent repairs missed dispatch wakeups for an
// agent that is currently connected.
//
// This is deliberately status-sensitive:
// - assigned / dispatch_timeout => rebuild dispatch from the start
// - dispatching / pulling       => continue the existing dispatch attempt
//
// The goal is forward progress without violating the single-owner state machine.
func (h *WebSocketHandler) redrivePendingTasksForConnectedAgent(client *wsClient) {
	if client == nil || client.agentID == 0 {
		return
	}
	var tasks []models.AgentTask
	if err := models.DB.Where("agent_id = ? AND status IN ?", client.agentID, []string{models.TaskStatusAssigned, models.TaskStatusDispatching, models.TaskStatusPulling, models.TaskStatusDispatchTimeout}).Order("priority DESC, created_at ASC").Find(&tasks).Error; err != nil {
		return
	}
	for i := range tasks {
		task := tasks[i]
		switch task.Status {
		case models.TaskStatusAssigned, models.TaskStatusDispatchTimeout:
			_ = h.sendTaskAssign(task)
		case models.TaskStatusDispatching, models.TaskStatusPulling:
			if task.DispatchToken == "" || task.DispatchAttempt <= 0 {
				continue
			}
			_ = h.sendPullTaskNow(client, task.ID, task.DispatchToken, task.DispatchAttempt)
		}
	}
}

// reconcileDispatchTimeouts turns stale dispatch-stage work into
// `dispatch_timeout` so the scheduler can safely re-drive it.
//
// We intentionally allow two timeout signals:
// - explicit lease expiry (`lease_expire_at`)
// - stale last update age
//
// That dual check lets the system recover both cleanly leased-but-expired work
// and tasks that got stranded because some intermediate wakeup step never
// completed.
func (h *WebSocketHandler) reconcileDispatchTimeouts(db *gorm.DB, now int64) (int, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return 0, nil
	}
	timeout := config.Config.GetDuration("task.dispatch_timeout")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cutoff := time.Unix(now, 0).Add(-timeout)
	var tasks []models.AgentTask
	if err := db.Where("status IN ?", []string{models.TaskStatusDispatching, models.TaskStatusPulling}).Find(&tasks).Error; err != nil {
		return 0, err
	}
	updated := 0
	for i := range tasks {
		task := &tasks[i]
		staleByLease := task.LeaseExpireAt > 0 && task.LeaseExpireAt <= now
		staleByAge := !task.UpdatedAt.IsZero() && !task.UpdatedAt.After(cutoff)
		if !staleByLease && !staleByAge {
			continue
		}
		if err := db.Model(&models.AgentTask{}).Where("id = ? AND status = ?", task.ID, task.Status).Updates(map[string]interface{}{
			"status":           models.TaskStatusDispatchTimeout,
			"lease_expire_at":  int64(0),
			"agent_session_id": "",
			"owner_server_id":  "",
		}).Error; err != nil {
			return updated, err
		}
		task.Status = models.TaskStatusDispatchTimeout
		task.LeaseExpireAt = 0
		task.AgentSessionID = ""
		task.OwnerServerID = ""
		syncLiveTaskStateFromTask(task, "")
		updated++
	}
	return updated, nil
}

// rebindExecutionTasksForReconnect transfers execution-stage ownership to the
// new `(agent_session_id, owner_server_id)` after reconnect.
//
// Only `acked` and `running` are rebound here. Those states mean the agent has
// already accepted or started the task, so reconnect must preserve execution
// continuity instead of re-dispatching it like brand new work.
func (h *WebSocketHandler) rebindExecutionTasksForReconnect(client *wsClient, agent *models.Agent) {
	if client == nil || client.agentID == 0 {
		return
	}
	var tasks []models.AgentTask
	if err := models.DB.Where("agent_id = ? AND status IN ?", client.agentID, []string{models.TaskStatusAcked, models.TaskStatusRunning}).Find(&tasks).Error; err != nil {
		return
	}
	for i := range tasks {
		task := &tasks[i]
		if task.AgentSessionID == client.sessionID && task.OwnerServerID == client.serverID {
			continue
		}
		if err := models.DB.Model(&models.AgentTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
			"agent_session_id": client.sessionID,
			"owner_server_id":  client.serverID,
		}).Error; err != nil {
			continue
		}
		task.AgentSessionID = client.sessionID
		task.OwnerServerID = client.serverID
		agentName := ""
		if agent != nil {
			agentName = agent.Name
		}
		syncLiveTaskStateFromTask(task, agentName)
	}
}

// shouldDispatchAgentStreamEvent decides whether a Redis stream wakeup event is
// still valid for the task row we see *now*.
//
// We require both dispatch identity match (token/attempt) and dispatch-stage
// status membership. This is what prevents stale stream replay from regressing
// already-running work after reconnect or owner failover.
func shouldDispatchAgentStreamEvent(task *models.AgentTask, event utils.AgentStreamEvent) bool {
	if task == nil || task.ID == 0 || event.TaskID == 0 {
		return false
	}
	if task.ID != event.TaskID {
		return false
	}
	if task.DispatchToken != event.DispatchToken || task.DispatchAttempt != event.DispatchAttempt {
		return false
	}
	switch task.Status {
	case models.TaskStatusDispatching, models.TaskStatusPulling:
		return true
	default:
		return false
	}
}

func (h *WebSocketHandler) handleAgentPullTask(client *wsClient, agent *models.Agent, payload map[string]interface{}) {
	taskID := uint64(getFloat64(payload, "task_id"))
	dispatchToken := getString(payload, "dispatch_token")
	if taskID == 0 || dispatchToken == "" {
		h.sendAgentAck(client, "pull_task", taskID, 0, false, "invalid pull task payload", nil)
		return
	}
	var task models.AgentTask
	if err := models.DB.First(&task, taskID).Error; err != nil {
		h.sendAgentAck(client, "pull_task", taskID, 0, false, "task not found", nil)
		return
	}
	if task.AgentID != client.agentID || task.DispatchToken != dispatchToken {
		h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, false, "task routing mismatch", nil)
		return
	}
	if task.Status != models.TaskStatusAssigned && task.Status != models.TaskStatusDispatching && task.Status != models.TaskStatusPulling {
		h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, false, "task is not pullable", nil)
		return
	}
	now := time.Now().Unix()
	updates := map[string]interface{}{
		"status":           models.TaskStatusAcked,
		"agent_session_id": client.sessionID,
		"owner_server_id":  client.serverID,
	}
	if err := models.DB.Model(&task).Updates(updates).Error; err != nil {
		h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, false, "failed to ack task", nil)
		return
	}
	task.Status = models.TaskStatusAcked
	task.AgentSessionID = client.sessionID
	task.OwnerServerID = client.serverID
	syncLiveTaskStateFromTask(&task, agent.Name)
	msg := WebSocketMessage{
		Type: "task_payload",
		Payload: map[string]interface{}{
			"task": map[string]interface{}{
				"id":               task.ID,
				"agent_id":         task.AgentID,
				"pipeline_run_id":  task.PipelineRunID,
				"node_id":          task.NodeID,
				"task_type":        task.TaskType,
				"name":             task.Name,
				"params":           task.Params,
				"script":           task.Script,
				"work_dir":         task.WorkDir,
				"env_vars":         task.EnvVars,
				"status":           task.Status,
				"priority":         task.Priority,
				"timeout":          task.Timeout,
				"retry_count":      task.RetryCount,
				"max_retries":      task.MaxRetries,
				"dispatch_token":   task.DispatchToken,
				"dispatch_attempt": task.DispatchAttempt,
			},
			"timestamp": now,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, false, "failed to build task payload", nil)
		return
	}
	client.mu.Lock()
	err = client.conn.WriteMessage(websocket.TextMessage, data)
	client.mu.Unlock()
	if err != nil {
		h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, false, "failed to send task payload", nil)
		return
	}
	h.broadcastToFrontend(task.PipelineRunID, "task_status", map[string]interface{}{
		"task_id":          task.ID,
		"node_id":          task.NodeID,
		"run_id":           task.PipelineRunID,
		"status":           task.Status,
		"agent_id":         client.agentID,
		"agent_name":       agent.Name,
		"dispatch_attempt": task.DispatchAttempt,
		"timestamp":        now,
	})
	h.sendAgentAck(client, "pull_task", taskID, task.DispatchAttempt, true, "", nil)
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
func (h *WebSocketHandler) BroadcastTaskStatus(runID, taskID uint64, nodeID, status string, exitCode int, errorMsg, agentName string) {
	payload := map[string]interface{}{
		"task_id":    taskID,
		"node_id":    nodeID,
		"run_id":     runID,
		"status":     status,
		"exit_code":  exitCode,
		"error_msg":  errorMsg,
		"agent_name": agentName,
		"timestamp":  time.Now().Unix(),
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
	updateAgentStatusByPipelineConcurrency(models.DB, agentID)
}

// checkAndUpdatePipelineStatus checks all tasks status and updates pipeline run status accordingly
// This should be called when a task completes (success/failed) to determine the overall pipeline status
func (h *WebSocketHandler) checkAndUpdatePipelineStatus(runID uint64) {
	var run models.PipelineRun
	if err := models.DB.First(&run, runID).Error; err != nil {
		return
	}
	if run.Status != models.PipelineRunStatusRunning {
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
		case models.TaskStatusExecuteSuccess, models.TaskStatusCancelled:
			// Terminal.
		case models.TaskStatusExecuteFailed, models.TaskStatusScheduleFailed:
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
			"status":   models.PipelineRunStatusFailed,
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
		run.Status = models.PipelineRunStatusFailed
		run.EndTime = now
		run.Duration = duration
		run.ErrorMsg = firstErrorMsg
		syncLiveRunStateFromRun(&run)
		h.BroadcastRunStatus(runID, models.PipelineRunStatusFailed, firstErrorMsg, duration)
		updateAgentStatusByPipelineConcurrency(models.DB, run.AgentID)
		go NewPipelineHandler().scheduleQueuedPipelineRuns(models.DB)
		return
	}

	if allTerminal {
		now := time.Now().Unix()
		duration := 0
		if run.StartTime > 0 {
			duration = int(now - run.StartTime)
		}

		models.DB.Model(&run).Updates(map[string]interface{}{
			"status":   models.PipelineRunStatusSuccess,
			"end_time": now,
			"duration": duration,
		})
		run.Status = models.PipelineRunStatusSuccess
		run.EndTime = now
		run.Duration = duration
		run.ErrorMsg = ""
		syncLiveRunStateFromRun(&run)
		h.BroadcastRunStatus(runID, models.PipelineRunStatusSuccess, "", duration)
		updateAgentStatusByPipelineConcurrency(models.DB, run.AgentID)
		go NewPipelineHandler().scheduleQueuedPipelineRuns(models.DB)
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

	if run.Status != models.PipelineRunStatusRunning {
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

	edges := config.getEdges()
	if len(config.Nodes) == 0 || len(edges) == 0 {
		fmt.Printf("[DEBUG] No nodes or edges in config for run %d\n", runID)
		return
	}

	fmt.Printf("[DEBUG] Config has %d nodes, %d edges\n", len(config.Nodes), len(edges))

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
	for _, edge := range edges {
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
	for _, edge := range edges {
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
		if task.Status == models.TaskStatusExecuteSuccess {
			completedSuccess[task.NodeID] = true
			fmt.Printf("[DEBUG]   -> Node %s marked as successfully completed\n", task.NodeID)
		} else if task.Status == models.TaskStatusExecuteFailed || task.Status == models.TaskStatusScheduleFailed {
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
	serverTasksExecuted := false
	for _, task := range completedTasks {
		if task.Status != models.TaskStatusExecuteSuccess && task.Status != models.TaskStatusExecuteFailed && task.Status != models.TaskStatusScheduleFailed {
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
			canonicalType, def, ok := getPipelineTaskDefinition(node.Type)
			if !ok {
				fmt.Printf("[ERROR] Unsupported task type for downstream node %s: %s\n", downstreamID, node.Type)
				continue
			}
			nodeConfig := normalizePipelineNodeConfig(node.Type, canonicalType, node.getNodeConfig())

			timeout := node.Timeout
			if timeout <= 0 {
				if configured := toInt(nodeConfig["timeout"]); configured > 0 {
					timeout = configured
				}
			}
			if timeout <= 0 {
				timeout = 3600
			}

			pipelineHandler := NewPipelineHandler()
			if err := pipelineHandler.injectCredentialEnv(models.DB, canonicalType, def, nodeConfig, &run, run.TriggerUserID, run.TriggerUserRole); err != nil {
				fmt.Printf("[ERROR] Failed to inject credential for downstream node %s: %v\n", downstreamID, err)
				failedTask := &models.AgentTask{
					WorkspaceID:   run.WorkspaceID,
					AgentID:       run.AgentID,
					PipelineRunID: runID,
					NodeID:        downstreamID,
					TaskType:      canonicalType,
					Name:          node.Name,
					Status:        models.TaskStatusScheduleFailed,
					ErrorMsg:      "凭据注入失败: " + err.Error(),
					StartTime:     time.Now().Unix(),
					EndTime:       time.Now().Unix(),
					Timeout:       timeout,
				}
				_ = models.DB.Create(failedTask).Error
				h.BroadcastTaskStatus(runID, failedTask.ID, failedTask.NodeID, failedTask.Status, 0, failedTask.ErrorMsg, "")
				continue
			}

			if def.ExecMode == taskExecModeServer {
				success, errMsg := pipelineHandler.executeServerTask(models.DB, &run, node, canonicalType, nodeConfig, timeout)
				if success {
					fmt.Printf("[SUCCESS] Executed server downstream task: node=%s type=%s\n", downstreamID, canonicalType)
				} else {
					fmt.Printf("[ERROR] Failed server downstream task: node=%s type=%s err=%s\n", downstreamID, canonicalType, errMsg)
				}
				serverTasksExecuted = true
				continue
			}

			_, script, err := renderPipelineAgentScript(canonicalType, nodeConfig)
			if err != nil {
				fmt.Printf("[ERROR] Failed to render script for downstream node %s: %v\n", downstreamID, err)
				failedTask := &models.AgentTask{
					WorkspaceID:   run.WorkspaceID,
					AgentID:       run.AgentID,
					PipelineRunID: runID,
					NodeID:        downstreamID,
					TaskType:      canonicalType,
					Name:          node.Name,
					Status:        models.TaskStatusScheduleFailed,
					ErrorMsg:      "脚本渲染失败: " + err.Error(),
					StartTime:     time.Now().Unix(),
					EndTime:       time.Now().Unix(),
					Timeout:       timeout,
				}
				_ = models.DB.Create(failedTask).Error
				h.BroadcastTaskStatus(runID, failedTask.ID, failedTask.NodeID, failedTask.Status, 0, failedTask.ErrorMsg, "")
				continue
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

			maxRetries := resolveTaskMaxRetries(nodeConfig)

			paramsJSON, _ := json.Marshal(nodeConfig)
			repoURL, repoBranch, repoCommit, repoPath := "", "", "", ""
			if canonicalType == "git_clone" {
				if repo, ok := nodeConfig["repository"].(map[string]interface{}); ok {
					if v, ok := repo["url"].(string); ok {
						repoURL = v
					}
					if v, ok := repo["branch"].(string); ok {
						repoBranch = v
					}
					if v, ok := repo["commit_id"].(string); ok {
						repoCommit = v
					}
					if v, ok := repo["target_dir"].(string); ok {
						repoPath = v
					}
				}
			}

			newTask := &models.AgentTask{
				WorkspaceID:   run.WorkspaceID,
				AgentID:       run.AgentID,
				PipelineRunID: runID,
				NodeID:        downstreamID,
				TaskType:      "shell",
				Name:          node.Name,
				Params:        string(paramsJSON),
				Script:        script,
				WorkDir:       workDir,
				EnvVars:       envVars,
				Status:        models.TaskStatusQueued,
				Timeout:       timeout,
				MaxRetries:    maxRetries,
				RepoURL:       repoURL,
				RepoBranch:    repoBranch,
				RepoCommit:    repoCommit,
				RepoPath:      repoPath,
			}

			if err := createAgentTaskWithExplicitMaxRetries(models.DB, newTask); err == nil {
				fmt.Printf("[SUCCESS] Triggered downstream task %d for node %s\n", newTask.ID, downstreamID)
				h.BroadcastTaskStatus(runID, newTask.ID, newTask.NodeID, newTask.Status, 0, "", "")
				_ = h.sendTaskAssign(*newTask)
			} else {
				fmt.Printf("[ERROR] Failed to create task for node %s: %v\n", downstreamID, err)
			}
		}
	}

	if serverTasksExecuted {
		var latestCompleted []models.AgentTask
		models.DB.Where("pipeline_run_id = ?", runID).Find(&latestCompleted)
		h.triggerDownstreamTasks(runID, latestCompleted)
		h.checkAndUpdatePipelineStatus(runID)
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
	return models.IsTerminalTaskStatus(status)
}

func isValidTaskStatusTransition(from, to string) bool {
	return models.IsTaskStatusTransitionAllowed(from, to)
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
