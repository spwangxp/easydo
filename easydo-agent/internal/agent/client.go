package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

// Client is the main agent client that orchestrates all components
type Client struct {
	cfg       *config.Config
	sysInfo   *system.Info
	agentName string
	log       *logrus.Logger

	httpClient  *client.HTTPClient
	wsClient    *client.WebSocketClient
	tokenMgr    *TokenManager
	register    *Register
	heartbeat   *Heartbeat
	taskHandler *TaskHandler

	mu      sync.RWMutex
	agentID uint64
	token   string
	running bool
}

// NewClient creates a new agent client
func NewClient(cfg *config.Config, sysInfo *system.Info, agentName string, log *logrus.Logger) *Client {
	httpClient := client.NewHTTPClient(cfg.ServerURL, 30*time.Second)
	tokenMgr := NewTokenManager(cfg.Agent.TokenFile)

	return &Client{
		cfg:       cfg,
		sysInfo:   sysInfo,
		agentName: agentName,
		log:       log,

		httpClient: httpClient,
		tokenMgr:   tokenMgr,
		register:   NewRegister(httpClient, tokenMgr, cfg, sysInfo, agentName, log),
		heartbeat:  NewHeartbeat(httpClient, cfg, tokenMgr, 0, log),
		taskHandler: NewTaskHandler(httpClient, nil, cfg, tokenMgr, log),
	}
}

// initWebSocket initializes and connects the WebSocket client
func (c *Client) initWebSocket(ctx context.Context) error {
	c.mu.RLock()
	agentID := c.agentID
	token := c.token
	c.mu.RUnlock()

	if agentID == 0 || token == "" {
		return fmt.Errorf("agent not registered")
	}

	// Create WebSocket client
	c.wsClient = client.NewWebSocketClient(c.cfg.ServerURL, agentID, token)

	// Set task handler for WebSocket messages
	c.wsClient.SetTaskHandler(c.taskHandler)

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
	agentID, _, status, err := c.register.Execute(ctx)
	fmt.Printf("[DEBUG] Registration completed: id=%d, status=%s, err=%v\n", agentID, status, err)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	c.mu.Lock()
	c.agentID = agentID
	c.mu.Unlock()

	c.heartbeat.SetAgentID(agentID)
	c.taskHandler.SetAgentID(agentID)

	if status == StatusApproved {
		tokenFromFile, hasToken, _ := c.tokenMgr.GetToken()
		if hasToken && tokenFromFile != "" {
			c.mu.Lock()
			c.token = tokenFromFile
			c.mu.Unlock()
			c.heartbeat.SetToken(tokenFromFile)
			c.taskHandler.SetToken(tokenFromFile)
		}
	}

	c.heartbeat.Start(ctx)

	if status == StatusPending {
		c.log.Info("Agent is pending approval, waiting...")

		if err := c.register.WaitForApproval(ctx, 10*time.Second); err != nil {
			return fmt.Errorf("waiting for approval failed: %w", err)
		}

		newStatus := c.register.GetStatus()
		if newStatus == StatusApproved {
			tokenFromFile, hasToken, _ := c.tokenMgr.GetToken()
			if !hasToken || tokenFromFile == "" {
				c.log.Error("Agent approved but no token available")
				return fmt.Errorf("agent approved but no token available")
			}

			c.mu.Lock()
			c.token = tokenFromFile
			c.mu.Unlock()

			c.heartbeat.SetToken(tokenFromFile)
			c.taskHandler.SetToken(tokenFromFile)
			c.log.Infof("Agent approved and token configured: len=%d", len(tokenFromFile))
		}
	}

	if _, _, err := c.heartbeat.SendOneShot(ctx); err != nil {
		c.log.Warnf("Initial heartbeat failed: %v", err)
	}

	// Initialize WebSocket for real-time task handling
	if err := c.initWebSocket(ctx); err != nil {
		c.log.Warnf("WebSocket initialization failed: %v", err)
		c.log.Info("Falling back to HTTP polling mode")
	}

	c.taskHandler.Start(ctx)

	c.log.Infof("Agent started successfully: id=%d, name=%s", c.agentID, c.agentName)
	return nil
}

// Shutdown stops the agent
func (c *Client) Shutdown(ctx context.Context) error {
	c.log.Info("Shutting down agent...")

	c.taskHandler.Stop()
	c.heartbeat.Stop()

	// Close WebSocket connection
	if c.wsClient != nil {
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
