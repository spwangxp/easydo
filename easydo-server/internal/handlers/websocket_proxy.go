package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	proxyHeartbeatPongWait     = 60 * time.Second
	proxyHeartbeatPingInterval = (proxyHeartbeatPongWait * 9) / 10
	proxyHeartbeatWriteWait    = 10 * time.Second
)

// proxyClient represents an outgoing WebSocket connection from this server (Server_1)
// to a remote server (Server_2), where this server acts as a "frontend" to receive
// logs that need to be forwarded to actual frontends connected to Server_1.
//
// Key concepts:
// - One proxyClient per target remote server
// - Multiple runs (run_ids) are multiplexed over the same connection
// - Connection is kept alive as long as there are active subscriptions
// - When all subscriptions for a remote server are gone, connection is delayed-closed
type proxyClient struct {
	targetServerID  string                     // e.g. "srv-abc123" - the remote server we're connected to
	targetServerURL string                     // e.g. "http://192.168.1.100:8080" - URL of remote server
	selfID          string                     // ServerID of this server (for identification to remote)
	selfURL         string                     // ServerURL of this server (for identification to remote)
	handlers        *WebSocketHandler          // back-reference to access frontend routing
	conn            *websocket.Conn            // the actual WebSocket connection
	mu              sync.Mutex                 // protects conn
	subscriptions   map[uint64]map[string]bool // runID -> set of frontendClientIDs subscribed (for routing)
	subMu           sync.RWMutex               // protects subscriptions
	refCount        int                        // number of active subscriptions across all runs
	refCountMu      sync.Mutex                 // protects refCount
	closeTimer      *time.Timer                // delayed close when refCount reaches 0
	closeTimerMu    sync.Mutex                 // protects closeTimer
	onDisconnect    func(serverID string)      // callback when connection closes
	isClosed        bool
	heartbeatStopCh chan struct{}
	heartbeatOnce   sync.Once
}

// proxyClientPool manages all outgoing proxy connections to remote servers.
// Each remote server has at most one connection; multiple runs are multiplexed.
type proxyClientPool struct {
	handlers *WebSocketHandler       // back-reference to access frontend routing
	clients  map[string]*proxyClient // targetServerID -> proxyClient
	mu       sync.RWMutex            // protects clients
	selfID   string                  // this server's ID
	selfURL  string                  // this server's internal URL
}

func configureProxyHeartbeat(conn *websocket.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(proxyHeartbeatPongWait)); err != nil {
		return err
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(proxyHeartbeatPongWait))
	})
	conn.SetPingHandler(func(appData string) error {
		if err := conn.SetReadDeadline(time.Now().Add(proxyHeartbeatPongWait)); err != nil {
			return err
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(proxyHeartbeatWriteWait))
	})
	return nil
}

func startProxyHeartbeat(conn *websocket.Conn, writeMu *sync.Mutex, stopCh <-chan struct{}) {
	ticker := time.NewTicker(proxyHeartbeatPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			writeMu.Lock()
			err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(proxyHeartbeatWriteWait))
			writeMu.Unlock()
			if err != nil {
				return
			}
		case <-stopCh:
			return
		}
	}
}

func isRunScopedProxyMessageType(msgType string) bool {
	switch msgType {
	case "task_log", "task_status", "run_status":
		return true
	default:
		return false
	}
}

// newProxyClientPool creates a new pool with a back-reference to the handler
// for routing received logs to frontend clients.
func newProxyClientPool(handler *WebSocketHandler) *proxyClientPool {
	return &proxyClientPool{
		handlers: handler,
		clients:  make(map[string]*proxyClient),
		selfID:   utils.ServerID(),
		selfURL:  utils.ServerInternalURL(),
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
		targetServerID:  targetServerID,
		targetServerURL: targetServerURL,
		selfID:          p.selfID,
		selfURL:         p.selfURL,
		handlers:        p.handlers,
		subscriptions:   make(map[uint64]map[string]bool),
		heartbeatStopCh: make(chan struct{}),
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

// Subscribe adds a subscription for a run to this proxy client.
// frontendClientID is the ID of the frontend client that wants this run's realtime events.
func (c *proxyClient) Subscribe(ctx context.Context, runID uint64, frontendClientID string) error {
	if runID == 0 || frontendClientID == "" {
		return nil
	}

	notifyRemote := false
	refDelta := 0
	c.subMu.Lock()
	if c.subscriptions[runID] == nil {
		c.subscriptions[runID] = make(map[string]bool)
		notifyRemote = true
	}
	if !c.subscriptions[runID][frontendClientID] {
		c.subscriptions[runID][frontendClientID] = true
		refDelta = 1
	}
	c.subMu.Unlock()

	if refDelta > 0 {
		// Increment ref count and cancel any pending close
		c.refCountMu.Lock()
		c.refCount += refDelta
		c.cancelCloseTimer()
		c.refCountMu.Unlock()
	}

	if c.conn == nil {
		return nil
	}

	// Only notify the remote server when this run becomes newly tracked on this connection.
	if notifyRemote {
		return c.sendSubscribe(ctx, runID)
	}
	return nil
}

// Unsubscribe removes a subscription. When all subscriptions for a run are gone,
// a message is sent to the remote server to unsubscribe.
func (c *proxyClient) Unsubscribe(ctx context.Context, runID uint64, frontendClientID string) {
	if runID == 0 || frontendClientID == "" {
		return
	}

	removed := false
	notifyRemote := false
	c.subMu.Lock()
	if c.subscriptions[runID] != nil && c.subscriptions[runID][frontendClientID] {
		delete(c.subscriptions[runID], frontendClientID)
		removed = true
		if len(c.subscriptions[runID]) == 0 {
			delete(c.subscriptions, runID)
			notifyRemote = true
		}
	}
	c.subMu.Unlock()

	if removed {
		// Decrement ref count; if 0, start delayed close
		c.refCountMu.Lock()
		c.refCount--
		if c.refCount <= 0 {
			c.refCount = 0
			c.startCloseTimer()
		}
		c.refCountMu.Unlock()
	}

	if notifyRemote {
		go c.sendUnsubscribe(ctx, runID)
	}
}

func (c *proxyClient) HasSubscription(runID uint64, frontendClientID string) bool {
	if runID == 0 || frontendClientID == "" {
		return false
	}
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	return c.subscriptions[runID] != nil && c.subscriptions[runID][frontendClientID]
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
		c.closeTimerMu.Unlock()
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
	c.heartbeatOnce.Do(func() {
		if c.heartbeatStopCh != nil {
			close(c.heartbeatStopCh)
		}
	})
	if c.conn != nil {
		c.conn.Close()
	}
	if c.onDisconnect != nil {
		c.onDisconnect(c.targetServerID)
	}
}

// sendSubscribe sends a SUBSCRIBE message to the remote server for a run.
func (c *proxyClient) sendSubscribe(_ context.Context, runID uint64) error {
	msg := WebSocketMessage{
		Type: "proxy_subscribe",
		Payload: map[string]any{
			"run_id":        runID,
			"origin_server": c.selfID,
			"origin_url":    c.selfURL,
		},
	}
	return c.writeJSON(msg)
}

// sendUnsubscribe sends an UNSUBSCRIBE message to the remote server for a run.
func (c *proxyClient) sendUnsubscribe(_ context.Context, runID uint64) error {
	msg := WebSocketMessage{
		Type: "proxy_unsubscribe",
		Payload: map[string]any{
			"run_id":        runID,
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
	if err := c.conn.SetWriteDeadline(time.Now().Add(proxyHeartbeatWriteWait)); err != nil {
		return err
	}
	return c.conn.WriteJSON(msg)
}

// dial establishes the WebSocket connection to the remote server.
// It connects to the remote server's /ws/proxy endpoint, acting as a "frontend"
// to receive run-scoped realtime events that need to be forwarded to actual
// frontends on this server.
func (c *proxyClient) dial(ctx context.Context, selfID, selfURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(c.targetServerURL))
	if err != nil {
		return fmt.Errorf("parse target server url: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return fmt.Errorf("unsupported target server scheme: %s", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ws/proxy"
	query := parsed.Query()
	query.Set("proxy", "true")
	query.Set("origin_server", selfID)
	query.Set("origin_url", selfURL)
	parsed.RawQuery = query.Encode()
	proxyURL := parsed.String()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, proxyURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	if err := configureProxyHeartbeat(conn); err != nil {
		_ = conn.Close()
		return fmt.Errorf("configure proxy heartbeat: %w", err)
	}
	c.conn = conn
	go startProxyHeartbeat(c.conn, &c.mu, c.heartbeatStopCh)

	fmt.Printf("[proxy] Connected to remote server %s at %s\n", c.targetServerID, c.targetServerURL)
	return nil
}

// readLoop reads messages from the remote server and routes them to frontend clients.
// This loop runs in a goroutine until the connection closes.
func (c *proxyClient) readLoop() {
	for {
		c.mu.Lock()
		if c.conn == nil || c.isClosed {
			c.mu.Unlock()
			return
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(proxyHeartbeatPongWait)); err != nil {
			c.mu.Unlock()
			fmt.Printf("[proxy] Failed to refresh read deadline for %s: %v\n", c.targetServerID, err)
			c.close()
			return
		}
		c.mu.Unlock()
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

		// Route run-scoped realtime messages to appropriate frontend clients.
		if isRunScopedProxyMessageType(msg.Type) {
			c.routeRunScopedMessage(msg.Type, msg.Payload)
		}
	}
}

// routeRunScopedMessage extracts run_id from the payload and forwards the event
// to all frontend clients subscribed to this run via this proxy.
func (c *proxyClient) routeRunScopedMessage(msgType string, payload map[string]any) {
	runID := uint64(getInt64(payload, "run_id"))
	if runID == 0 {
		return
	}

	// Get frontend client IDs subscribed to this run.
	c.subMu.RLock()
	frontendClientIDs, exists := c.subscriptions[runID]
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

	runIDStr := strconv.FormatUint(runID, 10)
	msgBytes, _ := json.Marshal(WebSocketMessage{Type: msgType, Payload: payload})

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

		// Forward the message to this frontend client.
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, msgBytes)
		client.mu.Unlock()
		if err != nil {
			fmt.Printf("[proxy] Failed to forward %s to frontend client %s: %v\n", msgType, clientID, err)
		}
	}
	c.handlers.frontendsMu.RUnlock()
}

// HandleProxyConnection handles incoming WebSocket connections from other servers
// that want to receive realtime run events through this server as a proxy.
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

	// Create a proxy frontend client to represent this incoming proxy connection.
	client := &proxyFrontendClient{
		conn:            conn,
		originServer:    originServer,
		originURL:       originURL,
		runs:            make(map[uint64]bool),
		heartbeatStopCh: make(chan struct{}),
	}
	if err := configureProxyHeartbeat(conn); err != nil {
		_ = conn.Close()
		fmt.Printf("[proxy] Failed to configure heartbeat from %s: %v\n", originServer, err)
		return
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
	client.clientID = clientID
	h.proxyFrontends[originServer][clientID] = client
	h.proxyFrontendsMu.Unlock()

	fmt.Printf("[proxy] Incoming proxy connection from server %s (clientID: %s)\n", originServer, clientID)

	// Handle messages from the proxy
	go startProxyHeartbeat(client.conn, &client.writeMu, client.heartbeatStopCh)
	h.handleProxyMessages(client, clientID, originServer)
}

// proxyFrontendClient represents an incoming proxy connection from a remote server.
// The remote server is acting as a frontend to receive run-scoped realtime events
// from this server.
type proxyFrontendClient struct {
	conn            *websocket.Conn
	originServer    string          // ServerID of the origin server
	originURL       string          // URL of the origin server
	runs            map[uint64]bool // runIDs this proxy is subscribed to
	runsMu          sync.RWMutex    // protects runs
	clientID        string          // unique ID for this client
	writeMu         sync.Mutex
	heartbeatStopCh chan struct{}
	heartbeatOnce   sync.Once
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

		client.heartbeatOnce.Do(func() {
			if client.heartbeatStopCh != nil {
				close(client.heartbeatStopCh)
			}
		})
		client.conn.Close()
		fmt.Printf("[proxy] Proxy client %s from %s disconnected\n", clientID, originServer)
	}()

	for {
		if err := client.conn.SetReadDeadline(time.Now().Add(proxyHeartbeatPongWait)); err != nil {
			fmt.Printf("[proxy] Failed to refresh read deadline for proxy client %s: %v\n", clientID, err)
			return
		}
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
			runID := uint64(getInt64(msg.Payload, "run_id"))
			if runID > 0 {
				client.runsMu.Lock()
				client.runs[runID] = true
				client.runsMu.Unlock()
				fmt.Printf("[proxy] Proxy %s subscribed to run %d\n", clientID, runID)
			}
		case "proxy_unsubscribe":
			runID := uint64(getInt64(msg.Payload, "run_id"))
			if runID > 0 {
				client.runsMu.Lock()
				delete(client.runs, runID)
				client.runsMu.Unlock()
				fmt.Printf("[proxy] Proxy %s unsubscribed from run %d\n", clientID, runID)
			}
		}
	}
}

// broadcastToProxyFrontends sends a run-scoped realtime event to all proxy
// frontend clients that are subscribed to the given run. This is called from
// broadcastToFrontend when this server is the origin of the event and needs to
// forward it to other servers whose frontends are watching this run.
func (h *WebSocketHandler) broadcastToProxyFrontends(runID uint64, msgType string, payload map[string]any) {
	if runID == 0 || !isRunScopedProxyMessageType(msgType) {
		return
	}

	h.proxyFrontendsMu.RLock()
	defer h.proxyFrontendsMu.RUnlock()

	for serverID, clients := range h.proxyFrontends {
		for clientID, client := range clients {
			client.runsMu.RLock()
			subscribed := client.runs[runID]
			client.runsMu.RUnlock()
			if !subscribed {
				continue
			}

			msg := WebSocketMessage{Type: msgType, Payload: payload}
			msgBytes, _ := json.Marshal(msg)
			client.writeMu.Lock()
			_ = client.conn.SetWriteDeadline(time.Now().Add(proxyHeartbeatWriteWait))
			err := client.conn.WriteMessage(websocket.TextMessage, msgBytes)
			client.writeMu.Unlock()
			if err != nil {
				fmt.Printf("[proxy] Failed to forward %s for run %d to proxy client %s on server %s: %v\n", msgType, runID, clientID, serverID, err)
			} else {
				fmt.Printf("[proxy] Forwarded %s for run %d to proxy client %s on server %s\n", msgType, runID, clientID, serverID)
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

// collectRemoteRunServers resolves which remote servers may emit realtime events
// for a given run. It includes the run's scheduled agent and all currently created
// task agents so the subscription stays closed-loop before and after downstream
// tasks are materialized.
func (h *WebSocketHandler) collectRemoteRunServers(ctx context.Context, runID uint64, selfServerID string) map[string]string {
	targets := make(map[string]string)

	var run models.PipelineRun
	if err := models.DB.Select("id", "agent_id").First(&run, runID).Error; err != nil {
		fmt.Printf("[proxy] Failed to query run %d: %v\n", runID, err)
		return targets
	}

	if run.AgentID > 0 {
		serverID, serverURL, err := FindAgentServer(ctx, run.AgentID)
		if err != nil {
			fmt.Printf("[proxy] Run %d agent %d not on any server: %v\n", runID, run.AgentID, err)
		} else if serverID != selfServerID {
			targets[serverID] = serverURL
		}
	}

	var tasks []models.AgentTask
	if err := models.DB.Select("id", "agent_id").Where("pipeline_run_id = ?", runID).Find(&tasks).Error; err != nil {
		fmt.Printf("[proxy] Failed to query tasks for run %d: %v\n", runID, err)
		return targets
	}

	for _, task := range tasks {
		if task.AgentID == 0 {
			continue
		}
		serverID, serverURL, err := FindAgentServer(ctx, task.AgentID)
		if err != nil {
			fmt.Printf("[proxy] Agent %d for task %d not on any server: %v\n", task.AgentID, task.ID, err)
			continue
		}
		if serverID == selfServerID {
			continue
		}
		targets[serverID] = serverURL
	}

	return targets
}

func (h *WebSocketHandler) watchRemoteRunEvents(ctx context.Context, runID uint64, frontendClientID string) {
	if runID == 0 || frontendClientID == "" {
		return
	}

	h.subscribeToRemoteRunEvents(runID, frontendClientID)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.subscribeToRemoteRunEvents(runID, frontendClientID)
		}
	}
}

// subscribeToRemoteRunEvents establishes proxy connections to remote servers for
// all realtime events in a pipeline run that may originate from those remote servers.
//
// This is called when a frontend connects to this server (Server_1) for a specific run.
// The frontend wants realtime events from the whole run, regardless of which server
// actually owns the agent connections. This method sets up the necessary proxy
// subscriptions before tasks exist and keeps downstream task events covered.
func (h *WebSocketHandler) subscribeToRemoteRunEvents(runID uint64, frontendClientID string) {
	ctx := context.Background()
	selfServerID := utils.ServerID()

	targetServers := h.collectRemoteRunServers(ctx, runID, selfServerID)
	if len(targetServers) == 0 {
		return
	}

	fmt.Printf("[proxy] Setting up remote run subscriptions for run %d across %d servers, frontend %s\n",
		runID, len(targetServers), frontendClientID)

	for serverID, serverURL := range targetServers {
		pool := h.GetProxyPool()
		proxyClient, err := pool.GetOrCreateProxyClient(ctx, serverID, serverURL)
		if err != nil {
			fmt.Printf("[proxy] Failed to create proxy client for server %s: %v\n", serverID, err)
			continue
		}

		if proxyClient.HasSubscription(runID, frontendClientID) {
			continue
		}

		if err := proxyClient.Subscribe(ctx, runID, frontendClientID); err != nil {
			fmt.Printf("[proxy] Failed to subscribe to run %d on server %s: %v\n", runID, serverID, err)
			continue
		}

		fmt.Printf("[proxy] Successfully subscribed to run %d via proxy to server %s\n", runID, serverID)
	}
}

// unsubscribeFromRemoteRunEvents removes subscriptions for a frontend client that is disconnecting.
// This is called from handleFrontendMessages when a frontend client disconnects.
func (h *WebSocketHandler) unsubscribeFromRemoteRunEvents(runID uint64, frontendClientID string) {
	ctx := context.Background()
	pool := h.GetProxyPool()

	pool.mu.RLock()
	clients := make([]*proxyClient, 0, len(pool.clients))
	for _, client := range pool.clients {
		clients = append(clients, client)
	}
	pool.mu.RUnlock()

	for _, client := range clients {
		if client == nil {
			continue
		}
		client.Unsubscribe(ctx, runID, frontendClientID)
		fmt.Printf("[proxy] Unsubscribed run %d on server %s for frontend %s\n",
			runID, client.targetServerID, frontendClientID)
	}
}
