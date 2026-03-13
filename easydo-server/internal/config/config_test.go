package config

import "testing"

func TestValidateMultiReplicaRequirements_RejectsMissingInternalSettings(t *testing.T) {
	Init()
	Config.Set("server.id", "")
	Config.Set("server.internal_url", "")
	Config.Set("server.internal_token", "")

	err := ValidateMultiReplicaRequirements()
	if err == nil {
		t.Fatal("expected validation error for missing multi-replica settings")
	}
}

func TestValidateMultiReplicaRequirements_AllowsCompleteInternalSettings(t *testing.T) {
	Init()
	Config.Set("server.id", "server-a")
	Config.Set("server.internal_url", "http://server-a:8080")
	Config.Set("server.internal_token", "shared-secret")

	if err := ValidateMultiReplicaRequirements(); err != nil {
		t.Fatalf("expected validation success, got %v", err)
	}
}

func TestDatabaseStartupFlags_DefaultDisabled(t *testing.T) {
	Init()

	if ShouldAutoMigrate() {
		t.Fatal("expected auto migrate to be disabled by default")
	}
	if ShouldSeedTestData() {
		t.Fatal("expected seed test data to be disabled by default")
	}
}

func TestDatabaseStartupFlags_AllowExplicitEnable(t *testing.T) {
	Init()
	Config.Set("database.auto_migrate", true)
	Config.Set("database.seed_test_data", true)

	if !ShouldAutoMigrate() {
		t.Fatal("expected auto migrate to be enabled when configured")
	}
	if !ShouldSeedTestData() {
		t.Fatal("expected seed test data to be enabled when configured")
	}
}

func TestServerMode_NormalizesInvalidValues(t *testing.T) {
	Init()
	Config.Set("server.mode", "invalid-mode")

	if got := ServerMode(); got != "release" {
		t.Fatalf("server mode=%s, want release", got)
	}

	Config.Set("server.mode", "debug")
	if got := ServerMode(); got != "debug" {
		t.Fatalf("server mode=%s, want debug", got)
	}
}
