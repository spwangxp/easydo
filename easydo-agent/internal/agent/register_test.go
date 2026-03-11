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
