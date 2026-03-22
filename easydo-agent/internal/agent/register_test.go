package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-agent/internal/client"
	"easydo-agent/internal/config"
	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

func newDiscardLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func newTestRegister(serverURL string, cfg *config.Config) *Register {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Agent.TokenFile == "" {
		cfg.Agent.TokenFile = "/tmp/easydo-agent-test-token"
	}
	return NewRegister(
		client.NewHTTPClient(serverURL, 0),
		NewTokenManager(cfg.Agent.TokenFile),
		cfg,
		&system.Info{Hostname: "host-a", IPAddress: "10.0.0.1", OS: "linux", Arch: "amd64", CPUCores: 4, MemoryTotal: 1024, DiskTotal: 2048},
		"agent-a",
		newDiscardLogger(),
	)
}

func TestNewRegistration_SendsWorkspaceIDWhenConfigured(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"registration_status":"pending","register_key":"abc"}}`))
	}))
	defer server.Close()

	register := newTestRegister(server.URL, &config.Config{Agent: config.AgentConfig{TokenFile: t.TempDir() + "/token", WorkspaceID: 42}})
	if _, _, _, err := register.newRegistration(context.Background()); err != nil {
		t.Fatalf("newRegistration returned error: %v", err)
	}
	if got := uint64(captured["workspace_id"].(float64)); got != 42 {
		t.Fatalf("workspace_id=%d, want=42 payload=%v", got, captured)
	}
}

func TestNewRegistration_OmitsWorkspaceIDWhenUnconfigured(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"registration_status":"pending","register_key":"abc"}}`))
	}))
	defer server.Close()

	register := newTestRegister(server.URL, &config.Config{Agent: config.AgentConfig{TokenFile: t.TempDir() + "/token"}})
	if _, _, _, err := register.newRegistration(context.Background()); err != nil {
		t.Fatalf("newRegistration returned error: %v", err)
	}
	if _, exists := captured["workspace_id"]; exists {
		t.Fatalf("workspace_id unexpectedly present payload=%v", captured)
	}
}

func TestReRegister_DoesNotSendWorkspaceID(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"status":"online","registration_status":"approved"}}`))
	}))
	defer server.Close()

	register := newTestRegister(server.URL, &config.Config{Agent: config.AgentConfig{TokenFile: t.TempDir() + "/token", WorkspaceID: 55}})
	if _, _, _, err := register.reRegister(context.Background(), "approved-token"); err != nil {
		t.Fatalf("reRegister returned error: %v", err)
	}
	if _, exists := captured["workspace_id"]; exists {
		t.Fatalf("workspace_id unexpectedly present in re-registration payload=%v", captured)
	}
	if captured["token"] != "approved-token" {
		t.Fatalf("token=%v, want=approved-token", captured["token"])
	}
}

func TestNewRegistration_SendsRuntimeCapabilityLabels(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"registration_status":"pending","register_key":"abc"}}`))
	}))
	defer server.Close()

	register := NewRegister(
		client.NewHTTPClient(server.URL, 0),
		NewTokenManager(t.TempDir()+"/token"),
		&config.Config{},
		&system.Info{
			Hostname:  "host-a",
			IPAddress: "10.0.0.1",
			OS:        "linux",
			Arch:      "amd64",
			Runtime: system.RuntimeCapabilities{
				ExecutionMode:         system.ExecutionModeContainer,
				PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit,
				PrimaryRuntime:        "docker",
				AvailableRuntimes:     []string{"docker", "podman"},
				AvailableBuilders:     []string{"buildctl", "buildkitd"},
			},
		},
		"agent-a",
		newDiscardLogger(),
	)
	if _, _, _, err := register.newRegistration(context.Background()); err != nil {
		t.Fatalf("newRegistration returned error: %v", err)
	}
	labels, _ := captured["labels"].(string)
	if labels == "" {
		t.Fatalf("expected labels to be populated, payload=%v", captured)
	}
	if !containsLabel(labels, "execution=container") {
		t.Fatalf("labels=%s, expected execution=container", labels)
	}
	if !containsLabel(labels, "build-backend=embedded-buildkit") {
		t.Fatalf("labels=%s, expected build-backend=embedded-buildkit", labels)
	}
	if !containsLabel(labels, "runtime=podman") {
		t.Fatalf("labels=%s, expected runtime=podman", labels)
	}
}

func TestNewRegistration_SendsBaseInfoPayload(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"agent_id":1,"registration_status":"pending","register_key":"abc"}}`))
	}))
	defer server.Close()

	register := NewRegister(
		client.NewHTTPClient(server.URL, 0),
		NewTokenManager(t.TempDir()+"/token"),
		&config.Config{},
		&system.Info{
			Hostname:    "host-a",
			IPAddress:   "10.0.0.1",
			OS:          "linux",
			Arch:        "amd64",
			CPUCores:    8,
			MemoryTotal: 34359738368,
			DiskTotal:   536870912000,
			BaseInfo:    `{"schemaVersion":1,"status":"success","source":"agent_self","machine":{"cpu":{"logicalCores":8},"memory":{"totalBytes":34359738368},"storage":{"totalDiskBytes":536870912000},"gpu":{"count":1}}}`,
		},
		"agent-a",
		newDiscardLogger(),
	)
	if _, _, _, err := register.newRegistration(context.Background()); err != nil {
		t.Fatalf("newRegistration returned error: %v", err)
	}
	if captured["base_info"] == "" {
		t.Fatalf("expected base_info in registration payload, got=%v", captured)
	}
	if captured["base_info_collected_at"] == nil {
		t.Fatalf("expected base_info_collected_at in registration payload, got=%v", captured)
	}
	if captured["base_info"] != register.sysInfo.BaseInfo {
		t.Fatalf("base_info payload mismatch, got=%v want=%s", captured["base_info"], register.sysInfo.BaseInfo)
	}
}

func containsLabel(raw string, want string) bool {
	var labels []string
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return false
	}
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
