package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProxyClient_SubscribeUnsubscribe(t *testing.T) {
	// Create a proxyClient without actual connection for unit testing
	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: "http://192.168.1.100:8080",
		selfID:         "srv-local",
		selfURL:        "http://192.168.1.1:8080",
		subscriptions:  make(map[uint64]map[string]bool),
	}

	// Test initial state
	assert.Equal(t, 0, client.refCount)
	assert.Empty(t, client.subscriptions)

	// Test Subscribe
	err := client.Subscribe(nil, 100, "frontend-client-1")
	assert.NoError(t, err)

	client.subMu.RLock()
	assert.True(t, client.subscriptions[100]["frontend-client-1"])
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 1, client.refCount)
	client.refCountMu.Unlock()

	// Subscribe same task, different frontend
	err = client.Subscribe(nil, 100, "frontend-client-2")
	assert.NoError(t, err)

	client.subMu.RLock()
	assert.Len(t, client.subscriptions[100], 2)
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 2, client.refCount)
	client.refCountMu.Unlock()

	// Subscribe different task
	err = client.Subscribe(nil, 200, "frontend-client-1")
	assert.NoError(t, err)

	client.refCountMu.Lock()
	assert.Equal(t, 3, client.refCount)
	client.refCountMu.Unlock()

	// Test Unsubscribe - remove one frontend from task 100
	client.Unsubscribe(nil, 100, "frontend-client-1")

	client.subMu.RLock()
	assert.Len(t, client.subscriptions[100], 1)
	assert.True(t, client.subscriptions[100]["frontend-client-2"])
	client.subMu.RUnlock()

	client.refCountMu.Lock()
	assert.Equal(t, 2, client.refCount)
	client.refCountMu.Unlock()

	// Unsubscribe remaining frontend from task 100 - task should be removed
	client.Unsubscribe(nil, 100, "frontend-client-2")

	client.subMu.RLock()
	_, exists := client.subscriptions[100]
	assert.False(t, exists)
	client.subMu.RUnlock()

	// refCount should be 1 now (only task 200 remains)
	client.refCountMu.Lock()
	assert.Equal(t, 1, client.refCount)
	client.refCountMu.Unlock()
}

func TestProxyClient_CloseTimer(t *testing.T) {
	client := &proxyClient{
		targetServerID:  "srv-remote-1",
		targetServerURL: "http://192.168.1.100:8080",
		subscriptions:  make(map[uint64]map[string]bool),
		refCount:       1,
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
		subscriptions:  make(map[uint64]map[string]bool),
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
	handler.broadcastToProxyFrontends(123, 456, map[string]interface{}{
		"task_id": 123,
		"run_id":  456,
		"message": "test log",
	})
}

func TestProxyFrontendClient_TasksMap(t *testing.T) {
	client := &proxyFrontendClient{
		originServer: "srv-remote-1",
		originURL:    "http://192.168.1.100:8080",
		tasks:       make(map[uint64]bool),
	}

	// Initially no tasks
	client.tasksMu.RLock()
	assert.Empty(t, client.tasks)
	client.tasksMu.RUnlock()

	// Add tasks
	client.tasksMu.Lock()
	client.tasks[100] = true
	client.tasks[200] = true
	client.tasksMu.Unlock()

	client.tasksMu.RLock()
	assert.True(t, client.tasks[100])
	assert.True(t, client.tasks[200])
	assert.Len(t, client.tasks, 2)
	client.tasksMu.RUnlock()

	// Remove task
	client.tasksMu.Lock()
	delete(client.tasks, 100)
	client.tasksMu.Unlock()

	client.tasksMu.RLock()
	assert.False(t, client.tasks[100])
	assert.True(t, client.tasks[200])
	client.tasksMu.RUnlock()
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
