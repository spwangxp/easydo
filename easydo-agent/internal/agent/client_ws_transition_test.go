package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easydo-agent/internal/config"
	"easydo-agent/internal/system"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

func TestClientStart_PendingApprovalTransitionsViaWebSocketWithoutHTTPPollingOrHeartbeat(t *testing.T) {
	var heartbeatCalls atomic.Int32
	var selfCalls atomic.Int32

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/register":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"name":"agent-a","registration_status":"pending","register_key":"rk-abc"}}`))
		case "/api/agents/self":
			selfCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":1,"registration_status":"pending"}}`))
		case "/api/agents/heartbeat":
			heartbeatCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"data":{"status":"ok","heartbeat_interval":10}}`))
		case "/ws/agent/heartbeat":
			if r.URL.Query().Get("register_key") != "rk-abc" {
				http.Error(w, "missing register_key", http.StatusUnauthorized)
				return
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			ack := map[string]interface{}{
				"type": "heartbeat_ack",
				"payload": map[string]interface{}{
					"status":              "ok",
					"registration_status": "approved",
					"token":               "tok-from-ws",
					"agent_session_id":    "session-1",
				},
			}
			b, _ := json.Marshal(ack)
			_ = conn.WriteMessage(websocket.TextMessage, b)

			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	tokenFile := filepath.Join(t.TempDir(), "agent.token")
	cfg := &config.Config{
		ServerURL: ts.URL,
		Agent: config.AgentConfig{
			TokenFile: tokenFile,
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cli := NewClient(cfg, &system.Info{
		Hostname:    "host-a",
		IPAddress:   "127.0.0.1",
		OS:          "linux",
		Arch:        "amd64",
		CPUCores:    2,
		MemoryTotal: 1024,
		DiskTotal:   1024,
	}, "agent-a", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := cli.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer cli.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(tokenFile); err == nil && string(data) == "tok-from-ws" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("expected token file to exist: %v", err)
	}
	if got := string(data); got != "tok-from-ws" {
		t.Fatalf("token file=%q, want=%q", got, "tok-from-ws")
	}
	if got := cli.GetToken(); got != "tok-from-ws" {
		t.Fatalf("client token=%q, want=%q", got, "tok-from-ws")
	}

	if got := selfCalls.Load(); got != 0 {
		t.Fatalf("/api/agents/self calls=%d, want=0", got)
	}
	if got := heartbeatCalls.Load(); got != 0 {
		t.Fatalf("/api/agents/heartbeat calls=%d, want=0", got)
	}
}
