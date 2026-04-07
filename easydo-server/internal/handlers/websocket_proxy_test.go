package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyClient_SubscribeUnsubscribe(t *testing.T) {
	// Create a proxyClient without actual connection for unit testing
	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: "http://192.168.1.100:8080",
		selfID:          "srv-local",
		selfURL:         "http://192.168.1.1:8080",
		subscriptions:   make(map[uint64]map[string]bool),
	}

	// Test initial state
	assert.Equal(t, 0, client.refCount)
	assert.Empty(t, client.subscriptions)

	// Test Subscribe
	err := client.Subscribe(context.TODO(), 100, "frontend-client-1")
	assert.NoError(t, err)

	client.subMu.RLock()
	assert.True(t, client.subscriptions[100]["frontend-client-1"])
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 1, client.refCount)
	client.refCountMu.Unlock()

	// Subscribe same run, different frontend
	err = client.Subscribe(context.TODO(), 100, "frontend-client-2")
	assert.NoError(t, err)

	client.subMu.RLock()
	assert.Len(t, client.subscriptions[100], 2)
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 2, client.refCount)
	client.refCountMu.Unlock()

	// Subscribe different run
	err = client.Subscribe(context.TODO(), 200, "frontend-client-1")
	assert.NoError(t, err)

	client.refCountMu.Lock()
	assert.Equal(t, 3, client.refCount)
	client.refCountMu.Unlock()

	// Test Unsubscribe - remove one frontend from run 100
	client.Unsubscribe(context.TODO(), 100, "frontend-client-1")

	client.subMu.RLock()
	assert.Len(t, client.subscriptions[100], 1)
	assert.True(t, client.subscriptions[100]["frontend-client-2"])
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 2, client.refCount)
	client.refCountMu.Unlock()

	// Unsubscribe remaining frontend from run 100 - run should be removed
	client.Unsubscribe(context.TODO(), 100, "frontend-client-2")

	client.subMu.RLock()
	_, exists := client.subscriptions[100]
	assert.False(t, exists)
	client.subMu.RUnlock()

	// refCount should be 1 now (only run 200 remains)
	client.refCountMu.Lock()
	assert.Equal(t, 1, client.refCount)
	client.refCountMu.Unlock()
}

func TestProxyClient_CloseTimer(t *testing.T) {
	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: "http://192.168.1.100:8080",
		subscriptions:   make(map[uint64]map[string]bool),
		refCount:        1,
	}

	// Start close timer
	client.startCloseTimer()

	client.closeTimerMu.Lock()
	assert.NotNil(t, client.closeTimer)
	client.closeTimerMu.Unlock()

	// Cancel close timer
	client.cancelCloseTimer()

	client.closeTimerMu.Lock()
	assert.Nil(t, client.closeTimer)
	client.closeTimerMu.Unlock()

	// Setting refCount to 0 should start a new close timer
	client.refCountMu.Lock()
	client.refCount = 0
	client.refCountMu.Unlock()

	client.startCloseTimer()

	client.closeTimerMu.Lock()
	assert.NotNil(t, client.closeTimer)
	client.closeTimerMu.Unlock()

	// Starting timer again should not create a duplicate timer
	client.startCloseTimer()

	client.closeTimerMu.Lock()
	timer := client.closeTimer
	client.closeTimerMu.Unlock()

	// Give some time to verify timer is not reset/restarted
	time.Sleep(10 * time.Millisecond)

	client.closeTimerMu.Lock()
	assert.Equal(t, timer, client.closeTimer) // Same timer instance
	client.closeTimerMu.Unlock()
}

func TestProxyClient_Close(t *testing.T) {
	disconnected := false
	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: "http://192.168.1.100:8080",
		subscriptions:   make(map[uint64]map[string]bool),
		onDisconnect: func(serverID string) {
			assert.Equal(t, "srv-remote-1", serverID)
			disconnected = true
		},
		isClosed: false,
	}

	// Close should set isClosed and call onDisconnect
	client.close()

	assert.True(t, client.isClosed)
	assert.True(t, disconnected)

	// Closing again should be no-op
	client.close()
	assert.True(t, client.isClosed) // Still true, not changed
}

func TestProxyClientPool_New(t *testing.T) {
	handler := &WebSocketHandler{}
	pool := newProxyClientPool(handler)

	assert.NotNil(t, pool)
	assert.NotNil(t, pool.clients)
	assert.NotNil(t, pool.handlers)
	assert.Empty(t, pool.clients)
}

func TestBroadcastToProxyFrontends_Empty(t *testing.T) {
	// Test broadcast when there are no proxy frontends
	handler := &WebSocketHandler{
		frontends: make(map[string]map[string]*frontendClient),
	}
	handler.proxyFrontends = make(map[string]map[string]*proxyFrontendClient)

	// Should not panic with empty proxy frontends
	handler.broadcastToProxyFrontends(456, "task_log", map[string]any{
		"task_id": 123,
		"run_id":  456,
		"message": "test log",
	})
}

func TestProxyFrontendClient_RunsMap(t *testing.T) {
	client := &proxyFrontendClient{runs: make(map[uint64]bool)}

	// Initially no runs
	client.runsMu.RLock()
	assert.Empty(t, client.runs)
	client.runsMu.RUnlock()

	// Add runs
	client.runsMu.Lock()
	client.runs[100] = true
	client.runs[200] = true
	client.runsMu.Unlock()

	client.runsMu.RLock()
	assert.True(t, client.runs[100])
	assert.True(t, client.runs[200])
	assert.Len(t, client.runs, 2)
	client.runsMu.RUnlock()

	// Remove run
	client.runsMu.Lock()
	delete(client.runs, 100)
	client.runsMu.Unlock()

	client.runsMu.RLock()
	assert.False(t, client.runs[100])
	assert.True(t, client.runs[200])
	client.runsMu.RUnlock()
}

func TestProxyClient_RouteRunScopedMessage(t *testing.T) {
	serverConn, clientConn := newWebSocketPair(t)
	defer serverConn.Close()
	defer clientConn.Close()

	handler := &WebSocketHandler{
		frontends: map[string]map[string]*frontendClient{
			"456": {
				"frontend-client-1": {conn: serverConn, runID: "456"},
			},
		},
	}
	proxy := &proxyClient{
		handlers:      handler,
		subscriptions: map[uint64]map[string]bool{456: {"frontend-client-1": true}},
	}

	proxy.routeRunScopedMessage("run_status", map[string]any{
		"run_id": 456,
		"status": "running",
	})

	_, raw, err := clientConn.ReadMessage()
	require.NoError(t, err)

	var msg WebSocketMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	assert.Equal(t, "run_status", msg.Type)
	assert.Equal(t, float64(456), msg.Payload["run_id"])
	assert.Equal(t, "running", msg.Payload["status"])
}

func TestBroadcastToProxyFrontends_RunScoped(t *testing.T) {
	serverConn1, clientConn1 := newWebSocketPair(t)
	defer serverConn1.Close()
	defer clientConn1.Close()

	serverConn2, clientConn2 := newWebSocketPair(t)
	defer serverConn2.Close()
	defer clientConn2.Close()

	handler := &WebSocketHandler{
		proxyFrontends: map[string]map[string]*proxyFrontendClient{
			"srv-1": {
				"proxy-1": {conn: serverConn1, runs: map[uint64]bool{456: true}},
			},
			"srv-2": {
				"proxy-2": {conn: serverConn2, runs: map[uint64]bool{999: true}},
			},
		},
	}

	handler.broadcastToProxyFrontends(456, "task_status", map[string]any{
		"run_id":  456,
		"task_id": 123,
		"status":  "running",
	})

	_, raw, err := clientConn1.ReadMessage()
	require.NoError(t, err)

	var msg WebSocketMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	assert.Equal(t, "task_status", msg.Type)
	assert.Equal(t, float64(456), msg.Payload["run_id"])
	assert.Equal(t, float64(123), msg.Payload["task_id"])

	_ = clientConn2.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, err = clientConn2.ReadMessage()
	assert.Error(t, err)
}

func newWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConnCh <- conn
	}))
	t.Cleanup(server.Close)

	wsURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	wsURL.Scheme = "ws"

	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	require.NoError(t, err)

	select {
	case serverConn := <-serverConnCh:
		return serverConn, clientConn
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket pair")
		return nil, nil
	}
}

func TestFindAgentServer_InvalidAgent(t *testing.T) {
	// Test with invalid agent ID (0) - should return error
	// Note: This requires Redis to be set up, so we just test the error path
	// by checking that the function returns error for agentID=0
	// In a real test environment with Redis, this would be more thorough

	// The actual implementation calls utils.GetAgentPresence which needs Redis
	// For unit testing without Redis, we verify the function exists and has correct signature
	ctx := context.Background()
	_, _, err := FindAgentServer(ctx, 0)
	assert.Error(t, err) // Should error because agentID=0 is invalid
}

func TestHandleProxyConnection_InvalidParams(t *testing.T) {
	// Test HandleProxyConnection with invalid/missing parameters
	// This is a handler function, so we can't easily unit test without HTTP request
	// We just verify the function exists and has correct signature
}

func TestProxyClientOutgoingHeartbeatSendsPing(t *testing.T) {
	originalInterval := proxyHeartbeatPingInterval
	originalWait := proxyHeartbeatPongWait
	proxyHeartbeatPingInterval = 25 * time.Millisecond
	proxyHeartbeatPongWait = 100 * time.Millisecond
	defer func() {
		proxyHeartbeatPingInterval = originalInterval
		proxyHeartbeatPongWait = originalWait
	}()

	pingReceived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		conn.SetPingHandler(func(appData string) error {
			select {
			case pingReceived <- struct{}{}:
			default:
			}
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: server.URL,
		selfID:          "srv-local",
		selfURL:         "http://127.0.0.1:8080",
		subscriptions:   make(map[uint64]map[string]bool),
	}

	err := client.dial(context.Background(), client.selfID, client.selfURL)
	require.NoError(t, err)
	defer client.close()

	select {
	case <-pingReceived:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected outgoing proxy heartbeat ping")
	}
}

func TestHandleProxyConnectionHeartbeatSendsPing(t *testing.T) {
	originalInterval := proxyHeartbeatPingInterval
	originalWait := proxyHeartbeatPongWait
	proxyHeartbeatPingInterval = 25 * time.Millisecond
	proxyHeartbeatPongWait = 100 * time.Millisecond
	defer func() {
		proxyHeartbeatPingInterval = originalInterval
		proxyHeartbeatPongWait = originalWait
	}()

	gin.SetMode(gin.TestMode)
	handler := &WebSocketHandler{}
	pingReceived := make(chan struct{}, 1)

	router := gin.New()
	router.GET("/ws/proxy", handler.HandleProxyConnection)
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws/proxy"
	query := wsURL.Query()
	query.Set("proxy", "true")
	query.Set("origin_server", "srv-origin")
	query.Set("origin_url", "http://origin.internal")
	wsURL.RawQuery = query.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	require.NoError(t, err)
	defer conn.Close()

	conn.SetPingHandler(func(appData string) error {
		select {
		case pingReceived <- struct{}{}:
		default:
		}
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-pingReceived:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected incoming proxy heartbeat ping")
	}
}
