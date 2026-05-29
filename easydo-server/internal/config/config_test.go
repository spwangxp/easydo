package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestInit_BindsBootstrapDockerHubMirrorsEnv(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", "https://mirror-a.example, https://mirror-b.example")
	Init()

	if got := Config.GetString("buildkit.bootstrap_dockerhub_mirrors"); got != "https://mirror-a.example, https://mirror-b.example" {
		t.Fatalf("bootstrap mirrors=%q, want env value", got)
	}
}

func TestBootstrapDockerHubMirrors_ParsesEnvList(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", " https://mirror-a.example ,https://mirror-b.example ,, ")
	Init()

	got := BootstrapDockerHubMirrors()
	want := []string{"https://mirror-a.example", "https://mirror-b.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mirrors=%v, want=%v", got, want)
	}
}

func TestBootstrapDockerHubMirrors_EmptyEnvReturnsBuiltInDefaults(t *testing.T) {
	t.Setenv("BOOTSTRAP_DOCKERHUB_MIRRORS", "")
	Init()

	got := BootstrapDockerHubMirrors()
	want := []string{
		"https://docker.1ms.run/",
		"https://hub-mirror.c.163.com/",
		"https://docker.mirrors.ustc.edu.cn/",
		"https://docker.m.daocloud.io/",
		"https://mirror.aliyuncs.com/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mirrors=%v, want=%v", got, want)
	}
}

func TestMCPAllowedOriginsParsesEnvList(t *testing.T) {
	t.Setenv("MCP_ALLOWED_ORIGINS", " https://mcp-a.example ,https://mcp-b.example ,, ")
	Init()

	got := MCPAllowedOrigins()
	want := []string{"https://mcp-a.example", "https://mcp-b.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins=%v, want=%v", got, want)
	}
}

func TestGetDSNForcesUTF8MB4ConnectionCollation(t *testing.T) {
	Init()
	Config.Set("database.host", "db")
	Config.Set("database.port", 3306)
	Config.Set("database.username", "user")
	Config.Set("database.password", "pass")
	Config.Set("database.name", "easydo")

	dsn := GetDSN()
	for _, expected := range []string{
		"charset=utf8mb4",
		"collation=utf8mb4_unicode_ci",
	} {
		if !strings.Contains(dsn, expected) {
			t.Fatalf("expected DSN to contain %s, got %s", expected, dsn)
		}
	}
}

func TestValidateMultiReplicaRequirements_AllowsDerivedInternalURL(t *testing.T) {
	Init()
	Config.Set("server.id", "easydo-server-0")
	Config.Set("server.internal_url", "")
	Config.Set("server.internal_token", "shared-secret")

	if err := ValidateMultiReplicaRequirements(); err != nil {
		t.Fatalf("expected derived internal url configuration to pass, got err=%v", err)
	}
}

func TestValidateMultiReplicaRequirements_RequiresServerIDAndInternalToken(t *testing.T) {
	Init()
	Config.Set("server.id", "")
	Config.Set("server.internal_url", "")
	Config.Set("server.internal_token", "")

	err := ValidateMultiReplicaRequirements()
	if err == nil {
		t.Fatal("expected missing server.id and server.internal_token to fail validation")
	}
	if err.Error() != "multi-replica configuration missing required settings: server.id, server.internal_token" {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
