package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// proxyClient represents an outgoing WebSocket connection from this server (Server_1)
// to a remote server (Server_2), where this server acts as a "frontend" to receive
// logs that need to be forwarded to actual frontends connected to Server_1.
//
// Key concepts:
// - One proxyClient per target remote server
// - Multiple tasks (task_ids) are multiplexed over the same connection
// - Connection is kept alive as long as there are active subscriptions
// - When all subscriptions for a remote server are gone, connection is delayed-closed
type proxyClient struct {
	targetServerID   string                        // e.g. "srv-abc123" - the remote server we're connected to
	targetServerURL  string                        // e.g. "http://192.168.1.100:8080" - URL of remote server
	selfID           string                        // ServerID of this server (for identification to remote)
	selfURL          string                        // ServerURL of this server (for identification to remote)
	handlers         *WebSocketHandler            // back-reference to access frontend routing
	conn             *websocket.Conn               // the actual WebSocket connection
	mu               sync.Mutex                    // protects conn
	subscriptions    map[uint64]map[string]bool   // taskID -> set of frontendClientIDs subscribed (for routing)
	subMu            sync.RWMutex                  // protects subscriptions
	refCount         int                           // number of active subscriptions across all tasks
	refCountMu       sync.Mutex                    // protects refCount
	closeTimer       *time.Timer                   // delayed close when refCount reaches 0
	closeTimerMu     sync.Mutex                    // protects closeTimer
	onDisconnect     func(serverID string)         // callback when connection closes
	isClosed         bool
}

// proxyClientPool manages all outgoing proxy connections to remote servers.
// Each remote server has at most one connection; multiple tasks are multiplexed.
type proxyClientPool struct {
	handlers *WebSocketHandler                // back-reference to access frontend routing
	clients  map[string]*proxyClient          // targetServerID -> proxyClient
	mu       sync.RWMutex                     // protects clients
	selfID   string                           // this server's ID
	selfURL  string                           // this server's internal URL
}

// newProxyClientPool creates a new pool with a back-reference to the handler
// for routing received logs to frontend clients.
func newProxyClientPool(handler *WebSocketHandler) *proxyClientPool {
	return &proxyClientPool{
		handlers:      handler,
		clients:       make(map[string]*proxyClient),
		selfID:        utils.ServerID(),
		selfURL:       utils.ServerInternalURL(),
	}
}

// GetOrCreateProxyClient returns an existing proxy client for targetServerID,
// or creates a new one if none exists. The connection is established lazily.
func (p *proxyClientPool) GetOrCreateProxyClient(ctx context.Context, targetServerID, targetServerURL string) (*proxyClient, error) {
	// Quick path - existing client
	p.mu.RLock()
	if client, exists := p.clients[targetServerID]; exists && !client.isClosed {
		p.mu.RUnlock()
		// Reset any pending close timer
		client.cancelCloseTimer()
		return client, nil
	}
	p.mu.RUnlock()

	// Slow path - create new client
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := p.clients[targetServerID]; exists && !client.isClosed {
		client.cancelCloseTimer()
		return client, nil
	}

	// Create new proxy client
	client := &proxyClient{
		targetServerID:   targetServerID,
		targetServerURL: targetServerURL,
		selfID:          p.selfID,
		selfURL:         p.selfURL,
		handlers:       p.handlers,
		subscriptions:  make(map[uint64]map[string]bool),
		onDisconnect: func(serverID string) {
			p.mu.Lock()
			delete(p.clients, serverID)
			p.mu.Unlock()
		},
	}

	// Establish WebSocket connection to remote server
	if err := client.dial(ctx, p.selfID, p.selfURL); err != nil {
		return nil, fmt.Errorf("failed to dial proxy client to %s: %w", targetServerURL, err)
	}

	p.clients[targetServerID] = client
	go client.readLoop()

	return client, nil
}

// Subscribe adds a subscription for a task to this proxy client.
// frontendClientID is the ID of the frontend client that wants this task's logs.
func (c *proxyClient) Subscribe(ctx context.Context, taskID uint64, frontendClientID string) error {
	c.subMu.Lock()
	if c.subscriptions[taskID] == nil {
		c.subscriptions[taskID] = make(map[string]bool)
	}
	c.subscriptions[taskID][frontendClientID] = true
	c.subMu.Unlock()

	// Increment ref count and cancel any pending close
	c.refCountMu.Lock()
	c.refCount++
	c.cancelCloseTimer()
	c.refCountMu.Unlock()

	// Send subscribe message to remote server
	return c.sendSubscribe(ctx, taskID)
}

// Unsubscribe removes a subscription. When all subscriptions for a task are gone,
// a message is sent to the remote server to unsubscribe.
func (c *proxyClient) Unsubscribe(ctx context.Context, taskID uint64, frontendClientID string) {
	c.subMu.Lock()
	if c.subscriptions[taskID] != nil {
		delete(c.subscriptions[taskID], frontendClientID)
		if len(c.subscriptions[taskID]) == 0 {
			delete(c.subscriptions, taskID)
			// All subscriptions for this task are gone, notify remote
			go c.sendUnsubscribe(ctx, taskID)
		}
	}
	c.subMu.Unlock()

	// Decrement ref count; if 0, start delayed close
	c.refCountMu.Lock()
	c.refCount--
	if c.refCount <= 0 {
		c.refCount = 0
		c.startCloseTimer()
	}
	c.refCountMu.Unlock()
}

// cancelCloseTimer cancels any pending close timer
func (c *proxyClient) cancelCloseTimer() {
	c.closeTimerMu.Lock()
	if c.closeTimer != nil {
		c.closeTimer.Stop()
		c.closeTimer = nil
	}
	c.closeTimerMu.Unlock()
}

// startCloseTimer starts a timer to close the connection after grace period
func (c *proxyClient) startCloseTimer() {
	c.closeTimerMu.Lock()
	if c.closeTimer != nil {
		return // already have a timer
	}
	c.closeTimer = time.AfterFunc(30*time.Second, func() {
		c.close()
	})
	c.closeTimerMu.Unlock()
}

// close closes the underlying WebSocket connection
func (c *proxyClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return
	}
	c.isClosed = true
	if c.conn != nil {
		c.conn.Close()
	}
	if c.onDisconnect != nil {
		c.onDisconnect(c.targetServerID)
	}
}

// sendSubscribe sends a SUBSCRIBE message to the remote server for a task
func (c *proxyClient) sendSubscribe(ctx context.Context, taskID uint64) error {
	msg := WebSocketMessage{
		Type: "proxy_subscribe",
		Payload: map[string]interface{}{
			"task_id":       taskID,
			"origin_server": c.selfID,
			"origin_url":    c.selfURL,
		},
	}
	return c.writeJSON(msg)
}

// sendUnsubscribe sends an UNSUBSCRIBE message to the remote server for a task
func (c *proxyClient) sendUnsubscribe(ctx context.Context, taskID uint64) error {
	msg := WebSocketMessage{
		Type: "proxy_unsubscribe",
		Payload: map[string]interface{}{
			"task_id":       taskID,
			"origin_server": c.selfID,
			"origin_url":    c.selfURL,
		},
	}
	return c.writeJSON(msg)
}

// writeJSON writes a JSON message to the WebSocket connection
func (c *proxyClient) writeJSON(msg WebSocketMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.isClosed {
		return fmt.Errorf("connection closed")
	}
	return c.conn.WriteJSON(msg)
}

// dial establishes the WebSocket connection to the remote server.
// It connects to the remote server's /ws/proxy endpoint, acting as a "frontend"
// to receive logs that need to be forwarded to actual frontends on this server.
func (c *proxyClient) dial(ctx context.Context, selfID, selfURL string) error {
	// Build URL for remote server's proxy endpoint
	// Remote server expects: ws://targetServerURL/ws/proxy?proxy=true&origin_server=xxx&origin_url=xxx
	proxyURL := fmt.Sprintf("%s/ws/proxy?proxy=true&origin_server=%s&origin_url=%s",
		c.targetServerURL,
		url.QueryEscape(selfID),
		url.QueryEscape(selfURL),
	)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, proxyURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	c.conn = conn

	fmt.Printf("[proxy] Connected to remote server %s at %s\n", c.targetServerID, c.targetServerURL)
	return nil
}

// readLoop reads messages from the remote server and routes them to frontend clients
// This loop runs in a goroutine until the connection closes.
func (c *proxyClient) readLoop() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[proxy] Connection to %s read error: %v\n", c.targetServerID, err)
			}
			c.close()
			return
		}

		// Parse the message
		var msg WebSocketMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			fmt.Printf("[proxy] Failed to parse message from %s: %v\n", c.targetServerID, err)
			continue
		}

		// Route task_log messages to appropriate frontend clients
		if msg.Type == "task_log" {
			c.routeTaskLog(msg.Payload)
		}
	}
}

// routeTaskLog extracts task_id from the payload and forwards the log
// to all frontend clients subscribed to this task via this proxy.
func (c *proxyClient) routeTaskLog(payload map[string]interface{}) {
	taskIDFloat, ok := payload["task_id"].(float64)
	if !ok {
		return
	}
	taskID := uint64(taskIDFloat)

	// Get frontend client IDs subscribed to this task
	c.subMu.RLock()
	frontendClientIDs, exists := c.subscriptions[taskID]
	if !exists || len(frontendClientIDs) == 0 {
		c.subMu.RUnlock()
		return
	}
	// Copy the set to avoid holding lock while routing
	clientIDs := make([]string, 0, len(frontendClientIDs))
	for id := range frontendClientIDs {
		clientIDs = append(clientIDs, id)
	}
	c.subMu.RUnlock()

	// Get run_id for routing
	runIDFloat, _ := payload["run_id"].(float64)
	runIDStr := strconv.FormatUint(uint64(runIDFloat), 10)

	// Route to each subscribed frontend client
	// We access the handler's frontends map to find the actual frontend connection
	c.handlers.frontendsMu.RLock()
	for _, clientID := range clientIDs {
		runClients, runExists := c.handlers.frontends[runIDStr]
		if !runExists {
			continue
		}
		client, clientExists := runClients[clientID]
		if !clientExists {
			continue
		}

		// Forward the message to this frontend client
		msgBytes, _ := json.Marshal(WebSocketMessage{Type: "task_log", Payload: payload})
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, msgBytes)
		client.mu.Unlock()
		if err != nil {
			fmt.Printf("[proxy] Failed to forward log to frontend client %s: %v\n", clientID, err)
		}
	}
	c.handlers.frontendsMu.RUnlock()
}

// HandleProxyConnection handles incoming WebSocket connections from other servers
// that want to receive logs through this server as a proxy.
//
// This is the "server-side" endpoint - it accepts connections from remote servers
// that are acting as proxies to this server.
func (h *WebSocketHandler) HandleProxyConnection(c *gin.Context) {
	originServer := c.Query("origin_server")
	originURL := c.Query("origin_url")
	proxyStr := c.Query("proxy")

	// Only accept proxy connections
	if proxyStr != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a proxy connection"})
		return
	}

	if originServer == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "origin_server required"})
		return
	}

	// Verify this is an internal server connection (could add token check here)
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		fmt.Printf("[proxy] Failed to upgrade connection from %s: %v\n", originServer, err)
		return
	}

	// Create a proxy frontend client to represent this incoming proxy connection
	client := &proxyFrontendClient{
		conn:         conn,
		originServer: originServer,
		originURL:    originURL,
		tasks:        make(map[uint64]bool),
	}

	// Register in the handler's proxy frontends map
	h.proxyFrontendsMu.Lock()
	if h.proxyFrontends == nil {
		h.proxyFrontends = make(map[string]map[string]*proxyFrontendClient)
	}
	if h.proxyFrontends[originServer] == nil {
		h.proxyFrontends[originServer] = make(map[string]*proxyFrontendClient)
	}
	clientID := fmt.Sprintf("proxy_%s_%d", originServer, time.Now().UnixNano())
	h.proxyFrontends[originServer][clientID] = client
	h.proxyFrontendsMu.Unlock()

	fmt.Printf("[proxy] Incoming proxy connection from server %s (clientID: %s)\n", originServer, clientID)

	// Handle messages from the proxy
	h.handleProxyMessages(client, clientID, originServer)
}

// proxyFrontendClient represents an incoming proxy connection from a remote server.
// The remote server is acting as a frontend to receive logs from this server.
type proxyFrontendClient struct {
	conn         *websocket.Conn
	originServer string                        // ServerID of the origin server
	originURL    string                        // URL of the origin server
	tasks        map[uint64]bool               // taskIDs this proxy is subscribed to
	tasksMu      sync.RWMutex                  // protects tasks
	clientID     string                        // unique ID for this client
}

// handleProxyMessages handles incoming messages from a proxy (server acting as frontend)
func (h *WebSocketHandler) handleProxyMessages(client *proxyFrontendClient, clientID, originServer string) {
	defer func() {
		// Cleanup on disconnect
		h.proxyFrontendsMu.Lock()
		if servers, exists := h.proxyFrontends[originServer]; exists {
			delete(servers, clientID)
			if len(servers) == 0 {
				delete(h.proxyFrontends, originServer)
			}
		}
		h.proxyFrontendsMu.Unlock()

		client.conn.Close()
		fmt.Printf("[proxy] Proxy client %s from %s disconnected\n", clientID, originServer)
	}()

	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[proxy] Proxy client %s read error: %v\n", clientID, err)
			}
			return
		}

		var msg WebSocketMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			fmt.Printf("[proxy] Failed to parse message from proxy %s: %v\n", clientID, err)
			continue
		}

		switch msg.Type {
		case "proxy_subscribe":
			taskID := uint64(getFloat64(msg.Payload, "task_id"))
			if taskID > 0 {
				client.tasksMu.Lock()
				client.tasks[taskID] = true
				client.tasksMu.Unlock()
				fmt.Printf("[proxy] Proxy %s subscribed to task %d\n", clientID, taskID)
			}
		case "proxy_unsubscribe":
			taskID := uint64(getFloat64(msg.Payload, "task_id"))
			if taskID > 0 {
				client.tasksMu.Lock()
				delete(client.tasks, taskID)
				client.tasksMu.Unlock()
				fmt.Printf("[proxy] Proxy %s unsubscribed from task %d\n", clientID, taskID)
			}
		}
	}
}

// broadcastToProxyFrontends sends a task_log message to all proxy frontend clients
// that are subscribed to the given task or run. This is called from broadcastToFrontend
// when this server is the origin of the log and needs to forward to other servers
// whose frontends are watching this run.
//
// Parameters:
//   - taskID: the task that produced the log
//   - runID: the pipeline run this task belongs to (for run-based routing)
//   - payload: the log message payload
func (h *WebSocketHandler) broadcastToProxyFrontends(taskID, runID uint64, payload map[string]interface{}) {
	// For cross-server routing, we forward task_log messages to all proxy frontends.
	// Each proxy frontend has its own subscription tracking for tasks.
	// The runID is used for logging and future per-run subscription filtering.

	h.proxyFrontendsMu.RLock()
	defer h.proxyFrontendsMu.RUnlock()

	for serverID, clients := range h.proxyFrontends {
		for clientID, client := range clients {
			// For now, we forward all task_log messages to all proxy frontends
			// watching this run. The proxy will then forward to its local frontends.
			// In a more refined approach, we could track per-run subscriptions too.
			msg := WebSocketMessage{Type: "task_log", Payload: payload}
			msgBytes, _ := json.Marshal(msg)
			err := client.conn.WriteMessage(websocket.TextMessage, msgBytes)
			if err != nil {
				fmt.Printf("[proxy] Failed to forward task_log for task %d to proxy client %s on server %s: %v\n", taskID, clientID, serverID, err)
			} else {
				fmt.Printf("[proxy] Forwarded task_log for task %d to proxy client %s on server %s (run %d)\n", taskID, clientID, serverID, runID)
			}
		}
	}
}

// FindAgentServer looks up which server an agent is connected to via Redis.
// Returns (serverID, serverURL, error).
func FindAgentServer(ctx context.Context, agentID uint64) (string, string, error) {
	presence, err := utils.GetAgentPresence(ctx, agentID)
	if err != nil {
		return "", "", fmt.Errorf("agent %d not found in presence store: %w", agentID, err)
	}
	return presence.ServerID, presence.ServerURL, nil
}

// GetProxyPool returns the proxy client pool, creating it if necessary
func (h *WebSocketHandler) GetProxyPool() *proxyClientPool {
	if h.proxyPool == nil {
		h.proxyPool = newProxyClientPool(h)
	}
	return h.proxyPool
}

// subscribeToRemoteTaskLogs establishes proxy connections to remote servers for all tasks
// in a pipeline run that are owned by agents on those remote servers.
//
// This is called when a frontend connects to this server (Server_1) for a specific run.
// The frontend wants logs from ALL tasks in that run, regardless of which server
// the agent is actually running on. This method sets up the necessary proxy subscriptions.
//
// Flow:
// 1. Query all tasks for the run
// 2. For each task, find which server has the agent (via Redis AgentPresence)
// 3. If the agent is on a remote server (not self), create/get proxy connection to that server
// 4. Subscribe to the task on the remote server, so we receive its logs
//
// The frontendClientID parameter is used to track which local frontend wants the logs;
// when logs arrive via proxy, they'll be forwarded to this frontend client.
func (h *WebSocketHandler) subscribeToRemoteTaskLogs(runID uint64, frontendClientID string) {
	ctx := context.Background()
	selfServerID := utils.ServerID()

	// Get all tasks for this run
	var tasks []models.AgentTask
	if err := models.DB.Where("pipeline_run_id = ?", runID).Find(&tasks).Error; err != nil {
		fmt.Printf("[proxy] Failed to query tasks for run %d: %v\n", runID, err)
		return
	}

	fmt.Printf("[proxy] Setting up remote subscriptions for run %d, %d tasks found, frontend %s\n",
		runID, len(tasks), frontendClientID)

	// For each task, check if agent is on a remote server
	taskServers := make(map[uint64]string) // taskID -> targetServerID
	for _, task := range tasks {
		if task.AgentID == 0 {
			continue
		}

		// Find which server has this agent
		serverID, serverURL, err := FindAgentServer(ctx, task.AgentID)
		if err != nil {
			// Agent not connected or not found - skip
			fmt.Printf("[proxy] Agent %d for task %d not on any server: %v\n", task.AgentID, task.ID, err)
			continue
		}

		// Skip if agent is on this server (we don't need proxy for local agents)
		if serverID == selfServerID {
			continue
		}

		// Agent is on a remote server - need to subscribe
		fmt.Printf("[proxy] Task %d (agent %d) is on remote server %s, need proxy subscription\n",
			task.ID, task.AgentID, serverID)
		taskServers[task.ID] = serverID

		// Get or create proxy client for this target server
		pool := h.GetProxyPool()
		proxyClient, err := pool.GetOrCreateProxyClient(ctx, serverID, serverURL)
		if err != nil {
			fmt.Printf("[proxy] Failed to create proxy client for server %s: %v\n", serverID, err)
			continue
		}

		// Subscribe to this task on the remote server
		if err := proxyClient.Subscribe(ctx, task.ID, frontendClientID); err != nil {
			fmt.Printf("[proxy] Failed to subscribe to task %d on server %s: %v\n", task.ID, serverID, err)
			continue
		}

		fmt.Printf("[proxy] Successfully subscribed to task %d via proxy to server %s\n", task.ID, serverID)
	}
}

// unsubscribeFromRemoteTaskLogs removes subscriptions for a frontend client that is disconnecting.
// This is called from handleFrontendMessages when a frontend client disconnects.
func (h *WebSocketHandler) unsubscribeFromRemoteTaskLogs(runID uint64, frontendClientID string) {
	ctx := context.Background()

	// Get all tasks for this run
	var tasks []models.AgentTask
	if err := models.DB.Where("pipeline_run_id = ?", runID).Find(&tasks).Error; err != nil {
		fmt.Printf("[proxy] Failed to query tasks for run %d during unsubscribe: %v\n", runID, err)
		return
	}

	selfServerID := utils.ServerID()

	for _, task := range tasks {
		if task.AgentID == 0 {
			continue
		}

		// Find which server has this agent
		serverID, _, err := FindAgentServer(ctx, task.AgentID)
		if err != nil {
			continue
		}

		// Skip if agent is on this server
		if serverID == selfServerID {
			continue
		}

		// Get proxy client for this target server
		pool := h.GetProxyPool()
		pool.mu.RLock()
		proxyClient, exists := pool.clients[serverID]
		pool.mu.RUnlock()

		if !exists || proxyClient == nil {
			continue
		}

		// Unsubscribe from this task
		proxyClient.Unsubscribe(ctx, task.ID, frontendClientID)
		fmt.Printf("[proxy] Unsubscribed from task %d on server %s for frontend %s\n",
			task.ID, serverID, frontendClientID)
	}
}

