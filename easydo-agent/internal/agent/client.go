package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/system"
	agenttask "easydo-agent/internal/task"
	"github.com/sirupsen/logrus"
)

const (
	workspaceSweepRetention = 24 * time.Hour
	workspaceSweepInterval  = time.Hour
)

// Client is the main agent client that orchestrates all components
type Client struct {
	cfg       *config.Config
	sysInfo   *system.Info
	agentName string
	log       *logrus.Logger

	httpClient       *client.HTTPClient
	wsClient         *client.WebSocketClient
	tokenMgr         *TokenManager
	register         *Register
	heartbeat        *Heartbeat
	taskHandler      *TaskHandler
	workspaceSweeper *agenttask.WorkspaceSweeper
	terminalHandler  *TerminalHandler

	mu          sync.RWMutex
	agentID     uint64
	token       string
	registerKey string
	running     bool
}

// NewClient creates a new agent client
func NewClient(cfg *config.Config, sysInfo *system.Info, agentName string, log *logrus.Logger) *Client {
	httpClient := client.NewHTTPClient(cfg.ServerURL, 30*time.Second)
	tokenMgr := NewTokenManager(cfg.Agent.TokenFile)
	taskHandler := NewTaskHandler(httpClient, nil, cfg, tokenMgr, sysInfo.Runtime, log)

	return &Client{
		cfg:       cfg,
		sysInfo:   sysInfo,
		agentName: agentName,
		log:       log,

		httpClient:       httpClient,
		tokenMgr:         tokenMgr,
		register:         NewRegister(httpClient, tokenMgr, cfg, sysInfo, agentName, log),
		heartbeat:        NewHeartbeat(httpClient, cfg, tokenMgr, 0, log),
		taskHandler:      taskHandler,
		workspaceSweeper: agenttask.NewWorkspaceSweeper(taskHandler.WorkspaceManager(), log, workspaceSweepRetention, workspaceSweepInterval),
		terminalHandler:  NewTerminalHandler(log),
	}
}

// initWebSocket initializes and connects the WebSocket client
func (c *Client) initWebSocket(ctx context.Context) error {
	c.mu.RLock()
	agentID := c.agentID
	token := c.token
	c.mu.RUnlock()

	if agentID == 0 || token == "" {
		c.mu.RLock()
		registerKey := c.registerKey
		c.mu.RUnlock()
		if registerKey == "" {
			return fmt.Errorf("agent not registered")
		}
	}

	// Create WebSocket client
	c.wsClient = client.NewWebSocketClient(c.cfg.ServerURL, agentID, token, c.register.GetRegisterKey())
	c.wsClient.SetHeartbeatAckHandler(c.handleWebSocketHeartbeatAck)
	c.wsClient.SetAgentConfigHandler(c.handleWebSocketRuntimeAgentConfig)

	// Set task handler for WebSocket messages
	c.wsClient.SetTaskHandler(c.taskHandler)
	c.wsClient.SetTerminalHandler(c.terminalHandler)
	c.terminalHandler.SetWebSocketClient(c.wsClient)

	// Connect to WebSocket
	if err := c.wsClient.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect WebSocket: %w", err)
	}

	// Set WebSocket client in task handler for reporting
	c.taskHandler.SetWebSocketClient(c.wsClient)

	c.log.Info("WebSocket client initialized and connected")
	return nil
}

// Start starts the agent
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.mu.Unlock()

	fmt.Println("[DEBUG] Starting registration...")
	agentID, registerKey, status, err := c.register.Execute(ctx)
	fmt.Printf("[DEBUG] Registration completed: id=%d, status=%s, err=%v\n", agentID, status, err)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	c.mu.Lock()
	c.agentID = agentID
	c.registerKey = registerKey
	c.mu.Unlock()

	c.heartbeat.SetAgentID(agentID)
	c.taskHandler.SetAgentID(agentID)

	tokenFromFile, hasToken, _ := c.tokenMgr.GetToken()
	if hasToken && tokenFromFile != "" {
		c.mu.Lock()
		c.token = tokenFromFile
		c.mu.Unlock()
		c.heartbeat.SetToken(tokenFromFile)
		c.taskHandler.SetToken(tokenFromFile)
	}

	if status == StatusPending {
		c.log.Info("Agent is pending approval, entering websocket bootstrap mode")
	}

	// Initialize WebSocket for real-time task handling
	if err := c.initWebSocket(ctx); err != nil {
		return fmt.Errorf("websocket initialization failed: %w", err)
	}

	// Runtime communication (task exchange/status/log/heartbeat) is websocket-only.

	c.taskHandler.Start(ctx)
	if c.workspaceSweeper != nil {
		c.workspaceSweeper.Start(ctx)
	}

	c.log.Infof("Agent started successfully: id=%d, name=%s", c.agentID, c.agentName)
	return nil
}

func (c *Client) handleWebSocketRuntimeAgentConfig(payload map[string]interface{}) {
	agentCfg := client.AgentConfig{}
	if data, err := json.Marshal(payload); err == nil {
		_ = json.Unmarshal(data, &agentCfg)
	}
	if c.taskHandler != nil {
		c.taskHandler.updateTaskConcurrency(agentCfg)
	}
}

func (c *Client) handleWebSocketHeartbeatAck(payload map[string]interface{}) {
	c.handleWebSocketRuntimeAgentConfig(payload)

	regStatus, _ := payload["registration_status"].(string)
	token, _ := payload["token"].(string)
	if regStatus != string(StatusApproved) || token == "" {
		return
	}

	if err := c.tokenMgr.SaveToken(token); err != nil {
		c.log.Warnf("failed to persist token from websocket ack: %v", err)
	}
	if err := c.tokenMgr.DeleteRegisterKey(); err != nil {
		c.log.Warnf("failed to delete register key after websocket approval: %v", err)
	}

	c.mu.Lock()
	c.token = token
	c.registerKey = ""
	c.mu.Unlock()

	c.register.SetStatus(StatusApproved)
	c.heartbeat.SetToken(token)
	c.taskHandler.SetToken(token)
	if c.wsClient != nil {
		c.wsClient.SetToken(token)
		c.wsClient.SetRegisterKey("")
	}
}

// Shutdown stops the agent
func (c *Client) Shutdown(ctx context.Context) error {
	c.log.Info("Shutting down agent...")

	c.taskHandler.Stop()
	c.heartbeat.Stop()
	if c.workspaceSweeper != nil {
		c.workspaceSweeper.Stop()
	}

	// Close WebSocket connection
	if c.wsClient != nil {
		if c.terminalHandler != nil {
			c.terminalHandler.CloseAll("agent_shutdown")
		}
		if err := c.wsClient.Close(); err != nil {
			c.log.Warnf("Error closing WebSocket: %v", err)
		}
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	return nil
}

// GetAgentID returns the current agent ID
func (c *Client) GetAgentID() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentID
}

// GetToken returns the current token
func (c *Client) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}
