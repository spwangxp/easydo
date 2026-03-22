package task

import (
	"context"
	"strings"
	"testing"
)

func TestParseParams_PreservesEnvVarsWhenJSONContainsNonStringValues(t *testing.T) {
	params, err := ParseParams(map[string]interface{}{
		"id":       float64(1),
		"env_vars": `{"EASYDO_CRED_REPO_AUTH_PASSWORD":"secret","CI":true,"DEPTH":2}`,
	})
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.EnvVars["EASYDO_CRED_REPO_AUTH_PASSWORD"] != "secret" {
		t.Fatalf("expected credential env to be preserved, got %#v", params.EnvVars)
	}
	if params.EnvVars["CI"] != "true" {
		t.Fatalf("expected bool env to stringify, got %#v", params.EnvVars["CI"])
	}
	if params.EnvVars["DEPTH"] != "2" {
		t.Fatalf("expected numeric env to stringify, got %#v", params.EnvVars["DEPTH"])
	}
	if len(params.EnvVars) != 3 {
		t.Fatalf("expected all env vars to survive parsing, got %#v", params.EnvVars)
	}
}

func TestRunScript_PreservesSystemPathWhenCustomEnvProvided(t *testing.T) {
	executor := &Executor{}
	stdout, stderr, err := executor.runScript(context.Background(), `command -v sh >/dev/null && printf '%s' "$PATH"`, "/tmp", map[string]string{"EASYDO_FLAG": "1"})
	if err != nil {
		t.Fatalf("expected runScript to preserve PATH, got err=%v stderr=%s", err, stderr)
	}
	if stdout == "" {
		t.Fatalf("expected PATH to remain available when custom env is provided")
	}
}

func TestRunScript_PreservesLongSingleLineStdout(t *testing.T) {
	executor := &Executor{}
	stdout, stderr, err := executor.runScript(
		context.Background(),
		`dd if=/dev/zero bs=70000 count=1 2>/dev/null | tr '\000' 'a'; printf '\n'`,
		"/tmp",
		nil,
	)
	if err != nil {
		t.Fatalf("expected long single-line stdout to succeed, got err=%v stderr=%s", err, stderr)
	}
	if len(stdout) != 70001 {
		t.Fatalf("stdout len=%d, want=70001", len(stdout))
	}
	if !strings.HasPrefix(stdout, strings.Repeat("a", 64)) {
		t.Fatalf("expected stdout prefix to be preserved")
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("expected stdout to keep trailing newline")
	}
}
