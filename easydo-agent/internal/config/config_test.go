package config

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentConfig_YAMLIncludesWorkspaceID(t *testing.T) {
	var cfg Config
	data := []byte(`server_url: "http://localhost:8080"
agent:
  name: "agent-a"
  token_file: "/tmp/token"
  workspace_id: 42
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config failed: %v", err)
	}
	if cfg.Agent.WorkspaceID != 42 {
		t.Fatalf("workspace_id=%d, want=42", cfg.Agent.WorkspaceID)
	}
}

func TestApplyEnvOverrides_OverridesAgentWorkspaceID(t *testing.T) {
	t.Setenv("AGENT_WORKSPACE_ID", "77")
	cfg := &Config{}
	applyEnvOverrides(cfg)
	if cfg.Agent.WorkspaceID != 77 {
		t.Fatalf("workspace_id=%d, want=77", cfg.Agent.WorkspaceID)
	}
}

func TestApplyEnvOverrides_InvalidWorkspaceIDIgnored(t *testing.T) {
	t.Setenv("AGENT_WORKSPACE_ID", "not-a-number")
	cfg := &Config{Agent: AgentConfig{WorkspaceID: 15}}
	applyEnvOverrides(cfg)
	if cfg.Agent.WorkspaceID != 15 {
		t.Fatalf("workspace_id=%d, want=15", cfg.Agent.WorkspaceID)
	}
}

func TestLoad_ConfigFileWithWorkspaceID(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	configData := []byte(`server_url: "http://localhost:8080"
server:
  port: 8080
agent:
  name: "agent-b"
  token_file: "/tmp/token"
  workspace_id: 88
heartbeat:
  initial_interval: 10
task:
  poll_interval: 5
logging:
  level: "info"
  format: "text"
`)
	if err := os.WriteFile("config.yaml", configData, 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.Agent.WorkspaceID != 88 {
		t.Fatalf("workspace_id=%d, want=88", cfg.Agent.WorkspaceID)
	}
}
