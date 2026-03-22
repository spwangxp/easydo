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

func TestDatabaseStartupFlags_AreNoLongerConfigured(t *testing.T) {
	t.Setenv("DB_AUTO_MIGRATE", "true")
	t.Setenv("DB_SEED_TEST_DATA", "true")

	Init()

	if Config.IsSet("database.auto_migrate") {
		t.Fatal("expected database.auto_migrate to be removed from configuration")
	}
	if Config.IsSet("database.seed_test_data") {
		t.Fatal("expected database.seed_test_data to be removed from configuration")
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
