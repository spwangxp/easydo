package utils

import (
	"testing"

	"easydo-server/internal/config"
)

func TestValidateServerInternalURL_AcceptsReachableOverride(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "easydo-server-0")

	validated, err := ValidateServerInternalURL("http://10.0.0.25:8080")
	if err != nil {
		t.Fatalf("expected explicit reachable override to validate, got err=%v", err)
	}
	if validated != "http://10.0.0.25:8080" {
		t.Fatalf("validated url=%q, want %q", validated, "http://10.0.0.25:8080")
	}
}

func TestValidateServerInternalURL_RejectsServerIDHostnameDefault(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "easydo-server-0")

	_, err := ValidateServerInternalURL("http://easydo-server-0:8080")
	if err == nil {
		t.Fatal("expected pod-name style internal url to be rejected")
	}
}

func TestServerInternalURL_FallsBackToRuntimeIPWhenOverrideIsPodHostname(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "easydo-server-0")
	config.Config.Set("server.port", "18080")
	config.Config.Set("server.internal_url", "http://easydo-server-0:8080")

	originalDiscover := discoverRuntimeIPv4
	discoverRuntimeIPv4 = func() (string, error) {
		return "10.9.8.7", nil
	}
	defer func() {
		discoverRuntimeIPv4 = originalDiscover
	}()

	got := ServerInternalURL()
	if got != "http://10.9.8.7:18080" {
		t.Fatalf("ServerInternalURL()=%q, want %q", got, "http://10.9.8.7:18080")
	}
}

func TestServerInternalURL_PrefersPodIPEnvWhenNoValidOverride(t *testing.T) {
	t.Setenv("POD_IP", "10.2.3.4")
	config.Init()
	config.Config.Set("server.id", "easydo-server-0")
	config.Config.Set("server.port", "18080")
	config.Config.Set("server.internal_url", "")

	originalDiscover := discoverRuntimeIPv4
	discoverRuntimeIPv4 = func() (string, error) {
		return "10.9.8.7", nil
	}
	defer func() {
		discoverRuntimeIPv4 = originalDiscover
	}()

	got := ServerInternalURL()
	if got != "http://10.2.3.4:18080" {
		t.Fatalf("ServerInternalURL()=%q, want %q", got, "http://10.2.3.4:18080")
	}
}

func TestServerInternalURL_DerivesRuntimeIPWhenNoOverride(t *testing.T) {
	config.Init()
	config.Config.Set("server.id", "easydo-server-0")
	config.Config.Set("server.port", "18080")
	config.Config.Set("server.internal_url", "")

	originalDiscover := discoverRuntimeIPv4
	discoverRuntimeIPv4 = func() (string, error) {
		return "10.9.8.7", nil
	}
	defer func() {
		discoverRuntimeIPv4 = originalDiscover
	}()

	got := ServerInternalURL()
	if got != "http://10.9.8.7:18080" {
		t.Fatalf("ServerInternalURL()=%q, want %q", got, "http://10.9.8.7:18080")
	}
}
