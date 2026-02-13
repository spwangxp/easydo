package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds server configuration for health check endpoints
type ServerConfig struct {
	Port int `yaml:"port"`
}

// Config represents the agent configuration
type Config struct {
	ServerURL string `yaml:"server_url"`
	Server    ServerConfig `yaml:"server"`
	Agent     AgentConfig `yaml:"agent"`
	Heartbeat HeartbeatConfig `yaml:"heartbeat"`
	Task      TaskConfig `yaml:"task"`
	Logging   LoggingConfig `yaml:"logging"`
}

// AgentConfig holds agent-specific configuration
type AgentConfig struct {
	Name      string `yaml:"name"`
	TokenFile string `yaml:"token_file"`
}

// HeartbeatConfig holds heartbeat configuration
type HeartbeatConfig struct {
	InitialInterval int `yaml:"initial_interval"`
	RetryCount      int `yaml:"retry_count"`
	RetryInterval   int `yaml:"retry_interval"`
}

// TaskConfig holds task polling configuration
type TaskConfig struct {
	PollInterval      int `yaml:"poll_interval"`
	ExecutionTimeout  int `yaml:"execution_timeout"`
	WorkspacePath     string `yaml:"workspace_path"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load loads configuration from the config file
func Load() (*Config, error) {
	configPaths := []string{
		"config.yaml",
		"/data/agent/config.yaml",
		"/etc/easydo-agent/config.yaml",
	}

	var cfg *Config
	var lastErr error

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}

		cfg = &Config{}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			lastErr = err
			continue
		}

		// Apply environment variable overrides
		applyEnvOverrides(cfg)

		return cfg, nil
	}

	return nil, fmt.Errorf("failed to load config from any path: %v", lastErr)
}

// applyEnvOverrides applies environment variable overrides to the config
func applyEnvOverrides(cfg *Config) {
	// Server URL
	if v := os.Getenv("EASYDO_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}

	// Server port for health check
	if v := os.Getenv("AGENT_SERVER_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 {
			cfg.Server.Port = port
		}
	}

	// Heartbeat interval from config or environment
	if v := os.Getenv("AGENT_HEARTBEAT_INTERVAL"); v != "" {
		var interval int
		if _, err := fmt.Sscanf(v, "%d", &interval); err == nil && interval > 0 {
			cfg.Heartbeat.InitialInterval = interval
		}
	}

	// Poll interval from environment
	if v := os.Getenv("AGENT_POLL_INTERVAL"); v != "" {
		var interval int
		if _, err := fmt.Sscanf(v, "%d", &interval); err == nil && interval > 0 {
			cfg.Task.PollInterval = interval
		}
	}

	// Agent name
	if v := os.Getenv("AGENT_NAME"); v != "" {
		cfg.Agent.Name = v
	}

	// Token file
	if v := os.Getenv("AGENT_TOKEN_FILE"); v != "" {
		cfg.Agent.TokenFile = v
	}

	// Agent token (for re-registration)
	if v := os.Getenv("AGENT_TOKEN"); v != "" {
		// This will be used by token manager, not config
		_ = v
	}

	// Log level
	if v := os.Getenv("AGENT_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
}

// GetHeartbeatInterval returns the heartbeat interval in seconds
func (c *Config) GetHeartbeatInterval() int {
	if c.Heartbeat.InitialInterval > 0 {
		return c.Heartbeat.InitialInterval
	}
	return 10 // Default 10 seconds
}

// GetRetryCount returns the number of retries on failure
func (c *Config) GetRetryCount() int {
	if c.Heartbeat.RetryCount > 0 {
		return c.Heartbeat.RetryCount
	}
	return 3 // Default 3 retries
}

// GetRetryInterval returns the retry interval in seconds
func (c *Config) GetRetryInterval() int {
	if c.Heartbeat.RetryInterval > 0 {
		return c.Heartbeat.RetryInterval
	}
	return 5 // Default 5 seconds
}

// GetPollInterval returns the task poll interval in seconds
func (c *Config) GetPollInterval() int {
	if c.Task.PollInterval > 0 {
		return c.Task.PollInterval
	}
	return 5 // Default 5 seconds
}

// GetExecutionTimeout returns the task execution timeout in seconds
func (c *Config) GetExecutionTimeout() int {
	if c.Task.ExecutionTimeout > 0 {
		return c.Task.ExecutionTimeout
	}
	return 3600 // Default 1 hour
}

// GetWorkspacePath returns the workspace directory path
func (c *Config) GetWorkspacePath() string {
	if c.Task.WorkspacePath != "" {
		return c.Task.WorkspacePath
	}
	return "./workspace" // Default workspace directory
}

// GetServerPort returns the health check server port
func (c *Config) GetServerPort() int {
	if c.Server.Port > 0 {
		return c.Server.Port
	}
	return 8080 // Default port for health check server
}
