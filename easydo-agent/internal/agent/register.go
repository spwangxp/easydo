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
	"github.com/sirupsen/logrus"
)

// RegistrationStatus represents the agent registration status
type RegistrationStatus string

const (
	StatusPending  RegistrationStatus = "pending"
	StatusApproved RegistrationStatus = "approved"
	StatusRejected RegistrationStatus = "rejected"
	StatusUnknown  RegistrationStatus = "unknown"
)

// RegisterResponse represents the registration response from server
type RegisterResponse struct {
	AgentID            uint64 `json:"agent_id"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	RegistrationStatus string `json:"registration_status"`
	RegisterKey        string `json:"register_key,omitempty"`
}

func runtimeLabelsJSON(info *system.Info) string {
	if info == nil {
		return ""
	}
	labels := info.Runtime.Labels()
	if len(labels) == 0 {
		return ""
	}
	data, err := json.Marshal(labels)
	if err != nil {
		return ""
	}
	return string(data)
}

// SelfResponse represents the response from /api/agents/self endpoint
type SelfResponse struct {
	ID                 uint64 `json:"id"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	RegistrationStatus string `json:"registration_status"`
	Token              string `json:"token,omitempty"`
	HeartbeatInterval  int    `json:"heartbeat_interval"`
}

// Register handles agent registration
type Register struct {
	client      *client.HTTPClient
	tokenMgr    *TokenManager
	cfg         *config.Config
	sysInfo     *system.Info
	agentName   string
	agentID     uint64
	registerKey string
	status      RegistrationStatus
	log         *logrus.Logger
	mu          sync.RWMutex
}

// NewRegister creates a new register handler
func NewRegister(client *client.HTTPClient, tokenMgr *TokenManager, cfg *config.Config, sysInfo *system.Info, agentName string, log *logrus.Logger) *Register {
	return &Register{
		client:    client,
		tokenMgr:  tokenMgr,
		cfg:       cfg,
		sysInfo:   sysInfo,
		agentName: agentName,
		log:       log,
		status:    StatusUnknown,
	}
}

// Execute performs the registration process
func (r *Register) Execute(ctx context.Context) (agentID uint64, registerKey string, status RegistrationStatus, err error) {
	r.log.Info("Starting agent registration...")

	// Check if we already have a token (re-registration)
	token, hasToken, _ := r.tokenMgr.GetToken()
	if hasToken && token != "" {
		r.log.Info("Existing token found, attempting re-registration...")
		return r.reRegister(ctx, token)
	}

	// Get register key if we have one (agent was approved but restarted)
	if agentID, registerKey, hasKey, _ := r.tokenMgr.GetRegisterKey(); hasKey {
		r.mu.Lock()
		r.agentID = agentID
		r.registerKey = registerKey
		r.mu.Unlock()
		r.log.Info("Register key found, attempting to get token...")
		return r.getToken(ctx, agentID, registerKey)
	}

	// New registration
	return r.newRegistration(ctx)
}

// newRegistration performs a new agent registration
func (r *Register) newRegistration(ctx context.Context) (uint64, string, RegistrationStatus, error) {
	registerReq := map[string]interface{}{
		"name":                   r.agentName,
		"host":                   r.sysInfo.Hostname,
		"port":                   0,
		"labels":                 runtimeLabelsJSON(r.sysInfo),
		"tags":                   "",
		"os":                     r.sysInfo.OS,
		"arch":                   r.sysInfo.Arch,
		"hostname":               r.sysInfo.Hostname,
		"ip_address":             r.sysInfo.IPAddress,
		"cpu_cores":              r.sysInfo.CPUCores,
		"memory_total":           r.sysInfo.MemoryTotal,
		"disk_total":             r.sysInfo.DiskTotal,
		"base_info":              r.sysInfo.BaseInfo,
		"base_info_collected_at": r.sysInfo.BaseInfoCollectedAt,
	}
	if r.cfg != nil && r.cfg.Agent.WorkspaceID > 0 {
		registerReq["workspace_id"] = r.cfg.Agent.WorkspaceID
	}

	resp, err := r.client.Post(ctx, "/api/agents/register", registerReq)
	if err != nil {
		return 0, "", StatusPending, fmt.Errorf("registration request failed: %w", err)
	}

	if resp.StatusCode != 200 {
		return 0, "", StatusPending, fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	// Parse response
	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return 0, "", StatusPending, fmt.Errorf("invalid registration response")
	}

	agentID := uint64(data["agent_id"].(float64))
	statusStr := data["registration_status"].(string)

	r.mu.Lock()
	r.agentID = agentID
	r.status = RegistrationStatus(statusStr)
	r.mu.Unlock()

	// Save register key if provided
	if key, ok := data["register_key"].(string); ok && key != "" {
		r.registerKey = key
		r.tokenMgr.SaveRegisterKey(key, agentID)
	}

	r.log.Infof("Registration submitted: agent_id=%d, status=%s", agentID, statusStr)

	return agentID, r.registerKey, r.status, nil
}

// reRegister re-registers an existing agent with its token
func (r *Register) reRegister(ctx context.Context, token string) (uint64, string, RegistrationStatus, error) {
	registerReq := map[string]interface{}{
		"name":                   r.agentName,
		"host":                   r.sysInfo.Hostname,
		"port":                   0,
		"labels":                 runtimeLabelsJSON(r.sysInfo),
		"tags":                   "",
		"os":                     r.sysInfo.OS,
		"arch":                   r.sysInfo.Arch,
		"hostname":               r.sysInfo.Hostname,
		"ip_address":             r.sysInfo.IPAddress,
		"cpu_cores":              r.sysInfo.CPUCores,
		"memory_total":           r.sysInfo.MemoryTotal,
		"disk_total":             r.sysInfo.DiskTotal,
		"base_info":              r.sysInfo.BaseInfo,
		"base_info_collected_at": r.sysInfo.BaseInfoCollectedAt,
		"token":                  token,
	}

	resp, err := r.client.Post(ctx, "/api/agents/register", registerReq)
	if err != nil {
		return 0, "", StatusUnknown, fmt.Errorf("re-registration failed: %w", err)
	}

	if resp.StatusCode != 200 {
		// Token might be invalid, clear it
		r.tokenMgr.DeleteToken()
		return 0, "", StatusUnknown, fmt.Errorf("re-registration failed with status %d", resp.StatusCode)
	}

	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return 0, "", StatusUnknown, fmt.Errorf("invalid re-registration response")
	}

	agentID := uint64(data["agent_id"].(float64))
	statusStr := data["status"].(string)
	regStatusStr := data["registration_status"].(string)

	r.mu.Lock()
	r.agentID = agentID
	r.status = RegistrationStatus(regStatusStr)
	r.mu.Unlock()

	r.log.Infof("Re-registration successful: agent_id=%d, status=%s", agentID, statusStr)

	return agentID, token, r.status, nil
}

// getToken retrieves the token using the register key
func (r *Register) getToken(ctx context.Context, agentID uint64, registerKey string) (uint64, string, RegistrationStatus, error) {
	selfReq := map[string]interface{}{
		"agent_id":     agentID,
		"register_key": registerKey,
	}

	resp, err := r.client.Post(ctx, "/api/agents/self", selfReq)
	if err != nil {
		return 0, "", StatusPending, fmt.Errorf("failed to get token: %w", err)
	}

	if resp.StatusCode == 403 {
		// Agent not approved yet - this is expected, wait for approval
		r.mu.Lock()
		r.status = StatusPending
		r.mu.Unlock()
		r.log.Infof("Agent not approved yet, waiting for approval... (agent_id=%d)", agentID)
		return agentID, registerKey, StatusPending, nil
	}

	if resp.StatusCode == 404 {
		// Agent not found, need to re-register
		r.mu.Lock()
		r.status = StatusUnknown
		r.mu.Unlock()
		r.log.Warnf("Agent not found on server (agent_id=%d), need to re-register", agentID)
		r.tokenMgr.DeleteRegisterKey()
		return 0, "", StatusUnknown, fmt.Errorf("agent not found on server, please re-register")
	}

	if resp.StatusCode != 200 {
		return 0, "", StatusUnknown, fmt.Errorf("get token failed with status %d", resp.StatusCode)
	}

	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return 0, "", StatusUnknown, fmt.Errorf("invalid token response")
	}

	statusStr := data["registration_status"].(string)
	r.mu.Lock()
	r.status = RegistrationStatus(statusStr)
	r.mu.Unlock()

	// Check if agent is still pending
	if statusStr == "pending" || statusStr == "rejected" {
		r.log.Infof("Agent status is %s, waiting for approval... (agent_id=%d)", statusStr, agentID)
		return agentID, registerKey, RegistrationStatus(statusStr), nil
	}

	// Agent is approved, get the token
	token, ok := data["token"].(string)
	if !ok || token == "" {
		return 0, "", StatusUnknown, fmt.Errorf("no token in response")
	}

	// Save the token
	if err := r.tokenMgr.SaveToken(token); err != nil {
		r.log.Warnf("Failed to save token: %v", err)
	}

	if err := r.tokenMgr.DeleteRegisterKey(); err != nil {
		r.log.Warnf("Failed to delete register key: %v", err)
	}

	r.mu.Lock()
	r.agentID = agentID
	r.mu.Unlock()

	r.log.Infof("Token obtained: agent_id=%d, status=%s", agentID, statusStr)
	return agentID, token, r.status, nil
}

// CheckApprovalStatus checks if the agent has been approved
func (r *Register) CheckApprovalStatus(ctx context.Context) (RegistrationStatus, string, error) {
	r.mu.RLock()
	agentID := r.agentID
	registerKey := r.registerKey
	r.mu.RUnlock()

	if agentID == 0 {
		return StatusUnknown, "", fmt.Errorf("agent not registered")
	}

	selfReq := map[string]interface{}{
		"agent_id": agentID,
	}

	// First try with register key if we have one
	if registerKey != "" {
		selfReq["register_key"] = registerKey
	}

	resp, err := r.client.Post(ctx, "/api/agents/self", selfReq)
	if err != nil {
		return StatusUnknown, "", err
	}

	if resp.StatusCode != 200 {
		return StatusUnknown, "", fmt.Errorf("status check failed: %d", resp.StatusCode)
	}

	data, ok := resp.Body["data"].(map[string]interface{})
	if !ok {
		return StatusUnknown, "", fmt.Errorf("invalid response")
	}

	statusStr := data["registration_status"].(string)
	r.mu.Lock()
	r.status = RegistrationStatus(statusStr)
	r.mu.Unlock()

	// Get token if approved and we have register key
	if statusStr == "approved" && registerKey != "" {
		if token, ok := data["token"].(string); ok && token != "" {
			r.tokenMgr.SaveToken(token)
			r.tokenMgr.DeleteRegisterKey()
			r.log.Info("Token obtained after approval")
		}
	}

	return r.status, "", nil
}

// GetAgentID returns the current agent ID
func (r *Register) GetAgentID() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentID
}

// GetStatus returns the current registration status
func (r *Register) GetStatus() RegistrationStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Register) GetRegisterKey() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registerKey
}

// SetStatus sets the registration status
func (r *Register) SetStatus(status RegistrationStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

// WaitForApproval waits for the agent to be approved
func (r *Register) WaitForApproval(ctx context.Context, checkInterval time.Duration) error {
	r.log.Info("Waiting for agent approval...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			status, _, err := r.CheckApprovalStatus(ctx)
			if err != nil {
				r.log.Warnf("Approval check failed: %v", err)
				time.Sleep(checkInterval)
				continue
			}

			if status == StatusApproved {
				r.log.Info("Agent approved!")
				return nil
			}

			if status == StatusRejected {
				return fmt.Errorf("agent registration rejected")
			}

			// Still pending
			r.log.Debugf("Status: %s, waiting...", status)
			time.Sleep(checkInterval)
		}
	}
}
