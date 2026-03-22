package agent

import (
	"context"
	"sync"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"github.com/sirupsen/logrus"
)

// Heartbeat handles agent heartbeat
type Heartbeat struct {
	client            *client.HTTPClient
	cfg               *config.Config
	tokenMgr          *TokenManager
	agentID           uint64
	token             string
	heartbeatInterval int
	log               *logrus.Logger
	mu                sync.RWMutex
	running           bool
	stopChan          chan struct{}
	intervalChan      chan int // Channel to signal interval change
}

// HeartbeatResponse represents the heartbeat response
type HeartbeatResponse struct {
	Status            string `json:"status"`
	ServerTime        int64  `json:"server_time"`
	PendingTasks      int    `json:"pending_tasks"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
}

// NewHeartbeat creates a new heartbeat handler
func NewHeartbeat(client *client.HTTPClient, cfg *config.Config, tokenMgr *TokenManager, agentID uint64, log *logrus.Logger) *Heartbeat {
	return &Heartbeat{
		client:            client,
		cfg:               cfg,
		tokenMgr:          tokenMgr,
		agentID:           agentID,
		log:               log,
		heartbeatInterval: cfg.GetHeartbeatInterval(),
		stopChan:          make(chan struct{}),
		intervalChan:      make(chan int, 1), // Buffer size 1 to avoid blocking
	}
}

// SetToken sets the agent token
func (h *Heartbeat) SetToken(token string) {
	h.mu.Lock()
	h.token = token
	h.mu.Unlock()
}

// SetAgentID sets the agent ID
func (h *Heartbeat) SetAgentID(agentID uint64) {
	h.mu.Lock()
	h.agentID = agentID
	h.mu.Unlock()
}

// Start starts the heartbeat loop
func (h *Heartbeat) Start(ctx context.Context) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()

	h.log.Infof("Starting heartbeat with interval %d seconds", h.heartbeatInterval)

	go h.run(ctx)
}

// Stop stops the heartbeat loop
func (h *Heartbeat) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	h.mu.Unlock()

	close(h.stopChan)
}

// run is the main heartbeat loop
func (h *Heartbeat) run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(h.heartbeatInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case newInterval := <-h.intervalChan:
			// Reset ticker with new interval
			ticker.Reset(time.Duration(newInterval) * time.Second)
			h.log.Infof("Heartbeat ticker reset to %d seconds", newInterval)
		case <-ticker.C:
			pendingTasks, err := h.sendHeartbeat(ctx)
			if err != nil {
				h.log.Warnf("Heartbeat failed: %v", err)
				continue
			}

			// Update heartbeat interval from server response
			if pendingTasks > 0 {
				// If there are pending tasks, we might want to check more frequently
				h.log.Debugf("Pending tasks: %d", pendingTasks)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat to the server
func (h *Heartbeat) sendHeartbeat(ctx context.Context) (int, error) {
	h.mu.RLock()
	agentID := h.agentID
	token := h.token
	h.mu.RUnlock()

	if agentID == 0 {
		h.log.Debug("Heartbeat skipped: agent not registered yet (agentID=0)")
		return 0, nil
	}

	heartbeatReq := map[string]interface{}{
		"agent_id":  agentID,
		"timestamp": time.Now().Unix(),
	}

	// Add token if we have one (required for approved agents)
	h.log.Debugf("DEBUG: token value before check: [%s], length=%d", token, len(token))
	if token != "" {
		heartbeatReq["token"] = token
		h.log.Debugf("Token set in heartbeat request: length=%d", len(token))
	} else {
		h.log.Debugf("No token available for heartbeat request")
	}

	h.log.Debugf("Sending heartbeat for agent %d at %d", agentID, heartbeatReq["timestamp"])

	resp, err := h.client.Post(ctx, "/api/agents/heartbeat", heartbeatReq)
	if err != nil {
		h.log.Warnf("Heartbeat request failed for agent %d: %v", agentID, err)
		return 0, err
	}

	if resp.StatusCode == 401 {
		// Token invalid, need to re-register
		h.log.Warn("Heartbeat authentication failed (401): token may be invalid, agent may need re-registration")
		return 0, nil
	}

	if resp.StatusCode == 403 {
		// Agent not approved
		if token == "" {
			h.log.Warn("Heartbeat rejected (403): agent is pending approval, waiting for admin approval")
		} else {
			h.log.Warn("Heartbeat rejected (403): token invalid despite being approved agent")
		}
		return 0, nil
	}

	if resp.StatusCode != 200 {
		h.log.Warnf("Heartbeat failed with status %d for agent %d, will retry on next tick", resp.StatusCode, agentID)
		return 0, nil
	}

	// Parse response
	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		h.log.Warnf("Heartbeat response invalid for agent %d", agentID)
		return 0, nil
	}

	// Update heartbeat interval from server response
	if interval, ok := data["heartbeat_interval"].(float64); ok {
		newInterval := int(interval)
		if newInterval > 0 && newInterval != h.heartbeatInterval {
			h.mu.Lock()
			h.heartbeatInterval = newInterval
			h.mu.Unlock()
			// Signal the run loop to reset the ticker with new interval
			select {
			case h.intervalChan <- newInterval:
			default:
				// Channel full, skip (next heartbeat will pick up the change)
			}
			h.log.Infof("Heartbeat interval updated to %d seconds", newInterval)
		}
	}

	pendingTasks := 0
	if pt, ok := data["pending_tasks"].(float64); ok {
		pendingTasks = int(pt)
	}

	h.log.Infof("Heartbeat successful for agent %d: pending_tasks=%d, interval=%d seconds",
		agentID, pendingTasks, h.heartbeatInterval)

	return pendingTasks, nil
}

// SendOneShot sends a single heartbeat (for initial registration)
func (h *Heartbeat) SendOneShot(ctx context.Context) (status string, pendingTasks int, err error) {
	h.mu.RLock()
	agentID := h.agentID
	h.mu.RUnlock()

	if agentID == 0 {
		return "", 0, nil
	}

	heartbeatReq := map[string]interface{}{
		"agent_id": agentID,
	}

	resp, err := h.client.Post(ctx, "/api/agents/heartbeat", heartbeatReq)
	if err != nil {
		return "", 0, err
	}

	if resp.StatusCode != 200 {
		return "", 0, nil
	}

	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return "", 0, nil
	}

	if s, ok := data["status"].(string); ok {
		status = s
	}

	if pt, ok := data["pending_tasks"].(float64); ok {
		pendingTasks = int(pt)
	}

	return status, pendingTasks, nil
}

// GetInterval returns the current heartbeat interval
func (h *Heartbeat) GetInterval() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.heartbeatInterval
}
