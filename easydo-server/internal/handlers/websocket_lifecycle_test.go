package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"

	"github.com/gorilla/websocket"
)

func TestNewWebSocketHandlerSnapshotsTaskCancelConfig(t *testing.T) {
	previousConfig := config.Config
	config.Init()
	t.Cleanup(func() {
		config.Config = previousConfig
	})

	config.Config.Set("task.cancel_ack_timeout", 7*time.Second)
	config.Config.Set("task.cancel_max_retries", 6)
	config.Config.Set("task.dispatch_timeout", 45*time.Second)
	handler := NewWebSocketHandler()

	// A handler owns one immutable runtime snapshot. Later config mutations must
	// not change an already-running retry worker.
	config.Config.Set("task.cancel_ack_timeout", time.Millisecond)
	config.Config.Set("task.cancel_max_retries", 1)
	config.Config.Set("task.dispatch_timeout", time.Millisecond)

	if got := handler.taskCancelAckTimeout(); got != 7*time.Second {
		t.Fatalf("task cancel ack timeout=%s, want construction-time value %s", got, 7*time.Second)
	}
	if got := handler.taskCancelMaxRetries(); got != 6 {
		t.Fatalf("task cancel max retries=%d, want construction-time value %d", got, 6)
	}
	if got := handler.taskDispatchTimeout(); got != 45*time.Second {
		t.Fatalf("task dispatch timeout=%s, want construction-time value %s", got, 45*time.Second)
	}
}

func TestWebSocketHandlerShutdownCancelsTaskCancelRetry(t *testing.T) {
	handler := NewWebSocketHandlerWithRuntimeConfig(WebSocketRuntimeConfig{
		TaskCancelAckTimeout: time.Hour,
		TaskCancelMaxRetries: 3,
	})
	task := models.AgentTask{
		BaseModel: models.BaseModel{ID: 91},
		AgentID:   42,
	}

	if ok := handler.sendTaskCancel(task); !ok {
		t.Fatal("expected cancel retry worker to start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := handler.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown did not join cancel retry worker: %v", err)
	}

	handler.taskCancelAcksMu.Lock()
	remainingTrackers := len(handler.taskCancelAcks)
	handler.taskCancelAcksMu.Unlock()
	if remainingTrackers != 0 {
		t.Fatalf("task cancel trackers remaining after shutdown=%d, want 0", remainingTrackers)
	}
	if ok := handler.sendTaskCancel(task); ok {
		t.Fatal("shutdown handler must reject new background work")
	}
}

func TestWebSocketHandlerShutdownClosesActiveAgentConnections(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-r.Context().Done()
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatalf("dial websocket failed: %v", err)
	}
	defer conn.Close()

	select {
	case serverConn := <-serverConnCh:
		defer serverConn.Close()
	case <-time.After(time.Second):
		t.Fatal("server websocket did not connect")
	}

	handler := NewWebSocketHandler()
	handler.agents[42] = &wsClient{agentID: 42, sessionID: "session-1", serverID: "server-1", conn: conn}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := handler.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"heartbeat"}`)); err == nil {
		t.Fatal("expected agent websocket write to fail after shutdown closed active connections")
	}
}
