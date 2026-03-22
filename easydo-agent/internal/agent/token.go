package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TokenManager manages agent token persistence
type TokenManager struct {
	tokenFile string
}

// NewTokenManager creates a new token manager
func NewTokenManager(tokenFile string) *TokenManager {
	return &TokenManager{
		tokenFile: tokenFile,
	}
}

// GetToken retrieves the token from environment or file
func (tm *TokenManager) GetToken() (string, bool, error) {
	// First, check environment variable
	if token := os.Getenv("AGENT_TOKEN"); token != "" {
		return token, true, nil
	}

	// Then, check token file
	token, err := tm.readTokenFile()
	if err != nil {
		fmt.Printf("[DEBUG] TokenManager: readTokenFile error: %v\n", err)
		return "", false, nil // No token found
	}

	fmt.Printf("[DEBUG] TokenManager: readTokenFile returned: [%s], len=%d\n", token, len(token))

	if token == "" {
		return "", false, nil // Empty token
	}
	return token, true, nil
}

// SaveToken saves the token to file
func (tm *TokenManager) SaveToken(token string) error {
	// Ensure directory exists
	dir := filepath.Dir(tm.tokenFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	// Write token to file
	if err := os.WriteFile(tm.tokenFile, []byte(token), 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// DeleteToken removes the token file
func (tm *TokenManager) DeleteToken() error {
	if _, err := os.Stat(tm.tokenFile); err == nil {
		if err := os.Remove(tm.tokenFile); err != nil {
			return fmt.Errorf("failed to delete token file: %w", err)
		}
	}
	return nil
}

// readTokenFile reads the token from file
func (tm *TokenManager) readTokenFile() (string, error) {
	data, err := os.ReadFile(tm.tokenFile)
	fmt.Printf("[DEBUG] readTokenFile: path=%s, err=%v, data_len=%d\n", tm.tokenFile, err, len(data))
	if err != nil {
		return "", nil // File doesn't exist or can't be read
	}

	token := strings.TrimSpace(string(data))
	fmt.Printf("[DEBUG] readTokenFile: trimmed token=[%s], len=%d\n", token, len(token))
	if token == "" {
		return "", nil // Empty file
	}

	return token, nil
}

// RegisterKey represents the registration key received during initial registration
type RegisterKey struct {
	Key     string
	AgentID uint64
}

// SaveRegisterKey saves the register key
func (tm *TokenManager) SaveRegisterKey(key string, agentID uint64) error {
	// Store register key with agent ID in a separate file
	registerKeyFile := tm.tokenFile + ".register"

	data := fmt.Sprintf("%d:%s", agentID, key)

	dir := filepath.Dir(registerKeyFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create register key directory: %w", err)
	}

	if err := os.WriteFile(registerKeyFile, []byte(data), 0600); err != nil {
		return fmt.Errorf("failed to write register key file: %w", err)
	}

	return nil
}

// GetRegisterKey retrieves the register key
func (tm *TokenManager) GetRegisterKey() (uint64, string, bool, error) {
	registerKeyFile := tm.tokenFile + ".register"

	data, err := os.ReadFile(registerKeyFile)
	if err != nil {
		return 0, "", false, nil
	}

	parts := strings.SplitN(string(data), ":", 2)
	if len(parts) != 2 {
		return 0, "", false, nil
	}

	var agentID uint64
	if _, err := fmt.Sscanf(parts[0], "%d", &agentID); err != nil {
		return 0, "", false, nil
	}

	return agentID, parts[1], true, nil
}

// DeleteRegisterKey removes the register key file
func (tm *TokenManager) DeleteRegisterKey() error {
	registerKeyFile := tm.tokenFile + ".register"
	if _, err := os.Stat(registerKeyFile); err == nil {
		if err := os.Remove(registerKeyFile); err != nil {
			return fmt.Errorf("failed to delete register key file: %w", err)
		}
	}
	return nil
}

// HasToken checks if a token exists (either in env or file)
func (tm *TokenManager) HasToken() bool {
	token, _, _ := tm.GetToken()
	return token != ""
}
