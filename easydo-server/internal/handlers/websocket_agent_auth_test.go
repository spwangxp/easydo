package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

func newAgentWSTestServer(t *testing.T, handler *WebSocketHandler) *httptest.Server {
	t.Helper()
	if config.Config == nil {
		config.Init()
	}
	config.Config.Set("server.id", "ws-test-server")
	config.Config.Set("server.internal_url", "http://127.0.0.1:8080")
	g := gin.New()
	g.GET("/ws/agent/heartbeat", handler.HandleAgentConnection)
	return httptest.NewServer(g)
}

func setupAgentWSTestRuntime(t *testing.T) {
	t.Helper()
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	previousRedis := utils.RedisClient
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		if utils.RedisClient != nil {
			_ = utils.RedisClient.Close()
		}
		utils.RedisClient = previousRedis
		mini.Close()
	})
}

func wsURL(httpURL string, query string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws/agent/heartbeat" + query
}

func TestHandleAgentConnection_PendingRequiresValidRegisterKey(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	agent := models.Agent{
		Name:               "pending-agent",
		Host:               "host-a",
		Port:               1,
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusPending,
		RegisterKey:        "rk-ok",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	h := NewWebSocketHandler()
	ts := newAgentWSTestServer(t, h)
	defer ts.Close()

	goodConn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&register_key=rk-ok", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial with valid register key failed: %v", err)
	}
	_ = goodConn.Close()

	badConn, badResp, badErr := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&register_key=bad", agent.ID)), nil)
	if badConn != nil {
		_ = badConn.Close()
	}
	if badErr == nil {
		t.Fatal("expected invalid register_key websocket auth to fail")
	}
	if badResp == nil || badResp.StatusCode != 401 {
		t.Fatalf("invalid register_key status=%v, want=401", badResp)
	}
}

func TestHandleAgentConnection_ApprovedRequiresToken(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	agent := models.Agent{
		Name:               "approved-agent",
		Host:               "host-b",
		Port:               1,
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-ok",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	h := NewWebSocketHandler()
	ts := newAgentWSTestServer(t, h)
	defer ts.Close()

	goodConn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&token=tok-ok", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial with valid token failed: %v", err)
	}
	_ = goodConn.Close()

	badConn, badResp, badErr := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d", agent.ID)), nil)
	if badConn != nil {
		_ = badConn.Close()
	}
	if badErr == nil {
		t.Fatal("expected approved agent without token to fail")
	}
	if badResp == nil || badResp.StatusCode != 401 {
		t.Fatalf("missing token status=%v, want=401", badResp)
	}
}

func TestHandleAgentConnection_ApprovedAllowsRegisterKeyRecovery(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	agent := models.Agent{
		Name:               "approved-agent-register-key",
		Host:               "host-recovery",
		Port:               1,
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-approved",
		RegisterKey:        "rk-approved",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	h := NewWebSocketHandler()
	ts := newAgentWSTestServer(t, h)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&register_key=rk-approved", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial with approved register key recovery failed: %v", err)
	}
	_ = conn.Close()
}

func TestHandleAgentHeartbeat_AckIncludesApprovalTokenTransition(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	agent := models.Agent{
		Name:               "pending-agent",
		Host:               "host-c",
		Port:               1,
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusPending,
		RegisterKey:        "rk-transition",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	h := NewWebSocketHandler()
	ts := newAgentWSTestServer(t, h)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&register_key=rk-transition", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial pending agent failed: %v", err)
	}
	defer conn.Close()

	if err := db.Model(&models.Agent{}).Where("id = ?", agent.ID).Updates(map[string]interface{}{
		"registration_status": models.AgentRegistrationStatusApproved,
		"token":               "tok-from-ack",
		"status":              models.AgentStatusOnline,
	}).Error; err != nil {
		t.Fatalf("promote agent failed: %v", err)
	}

	msg := WebSocketMessage{Type: "heartbeat", Payload: map[string]interface{}{"timestamp": time.Now().Unix()}}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write heartbeat failed: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline failed: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack failed: %v", err)
	}

	var ack WebSocketMessage
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal ack failed: %v", err)
	}
	if ack.Type != "heartbeat_ack" {
		t.Fatalf("ack type=%s, want=heartbeat_ack", ack.Type)
	}
	if got := ack.Payload["registration_status"]; got != models.AgentRegistrationStatusApproved {
		t.Fatalf("registration_status=%v, want=%s", got, models.AgentRegistrationStatusApproved)
	}
	if got := ack.Payload["token"]; got != "tok-from-ack" {
		t.Fatalf("ack token=%v, want=tok-from-ack", got)
	}
}

func TestHandleAgentConnection_StaleDisconnectDoesNotOfflineNewerSession(t *testing.T) {
	setupAgentWSTestRuntime(t)

	db := openHandlerTestDB(t)
	previousDB := models.DB
	models.DB = db
	t.Cleanup(func() { models.DB = previousDB })

	agent := models.Agent{
		Name:               "reconnect-agent",
		Host:               "host-d",
		Port:               1,
		Status:             models.AgentStatusOffline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		Token:              "tok-reconnect",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	h := NewWebSocketHandler()
	ts := newAgentWSTestServer(t, h)
	defer ts.Close()

	oldConn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&token=tok-reconnect", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial old session failed: %v", err)
	}
	defer oldConn.Close()

	newConn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL, fmt.Sprintf("?agent_id=%d&token=tok-reconnect", agent.ID)), nil)
	if err != nil {
		t.Fatalf("dial new session failed: %v", err)
	}
	defer newConn.Close()

	if !h.IsAgentOnline(agent.ID) {
		t.Fatal("agent should be online after second websocket connection")
	}

	_ = oldConn.Close()
	time.Sleep(200 * time.Millisecond)

	if !h.IsAgentOnline(agent.ID) {
		t.Fatal("closing stale websocket session incorrectly made newer session offline")
	}

	var reloaded models.Agent
	if err := db.First(&reloaded, agent.ID).Error; err != nil {
		t.Fatalf("reload agent failed: %v", err)
	}
	if reloaded.Status == models.AgentStatusOffline {
		t.Fatalf("stale disconnect set agent offline unexpectedly")
	}
}
