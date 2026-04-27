package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketClient_HandleAgentConfigInvokesRuntimeConfigHandler(t *testing.T) {
	client := NewWebSocketClient("http://example.com", 1, "", "")
	called := false
	client.SetAgentConfigHandler(func(payload map[string]interface{}) {
		called = true
		if payload["task_concurrency"] != 7.0 {
			t.Fatalf("task_concurrency=%v, want 7", payload["task_concurrency"])
		}
	})

	client.handleAgentConfig(map[string]interface{}{
		"task_concurrency": 7.0,
	})

	if !called {
		t.Fatal("expected agent config handler to be invoked")
	}
}

func TestWebSocketClient_ReconnectsWithRegisterKeyAfterApprovalBeforeTokenAck(t *testing.T) {
	var approved atomic.Bool
	var reconnects atomic.Int32
	var gotApprovedToken atomic.Bool

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/agent/heartbeat" {
			http.NotFound(w, r)
			return
		}

		q := r.URL.Query()
		if !approved.Load() {
			if q.Get("register_key") != "rk-reconnect" {
				http.Error(w, "missing register key", http.StatusUnauthorized)
				return
			}
		} else {
			if q.Get("token") != "tok-reconnect" && q.Get("register_key") != "rk-reconnect" {
				http.Error(w, "missing recovery credential", http.StatusUnauthorized)
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg WebSocketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return
		}

		if !approved.Load() {
			approved.Store(true)
			reconnects.Add(1)
			_ = conn.Close()
			return
		}

		ack := WebSocketMessage{Type: "heartbeat_ack", Payload: map[string]interface{}{
			"status":              "ok",
			"registration_status": "approved",
			"token":               "tok-reconnect",
			"agent_session_id":    "session-reconnect",
		}}
		payload, _ := json.Marshal(ack)
		_ = conn.WriteMessage(websocket.TextMessage, payload)
		if q.Get("register_key") == "rk-reconnect" {
			gotApprovedToken.Store(true)
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	client := NewWebSocketClient(ts.URL, 1, "", "rk-reconnect")
	client.SetConfig(&websocketConfig{heartbeatInterval: 1, reconnectDelay: 50 * time.Millisecond, maxReconnectAttempts: 5})
	client.SetHeartbeatAckHandler(func(payload map[string]interface{}) {
		if token, _ := payload["token"].(string); token != "" {
			client.SetToken(token)
			client.SetRegisterKey("")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("initial connect failed: %v", err)
	}
	defer client.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gotApprovedToken.Load() && client.GetSessionID() == "session-reconnect" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if reconnects.Load() == 0 {
		t.Fatal("expected server to force a reconnect during approval transition")
	}
	if !gotApprovedToken.Load() {
		t.Fatal("expected reconnect path to recover approved token via websocket")
	}
	if !client.IsConnected() {
		t.Fatal("client should remain connected after reconnect recovery")
	}
}
