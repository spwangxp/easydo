package task

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
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
	stdout, stderr, err := executor.runScript(context.Background(), 1, `command -v sh >/dev/null && printf '%s' "$PATH"`, "/tmp", map[string]string{"EASYDO_FLAG": "1"})
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
		1,
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

func TestErrToString_PreservesNonExitErrors(t *testing.T) {
	err := errors.New("plain failure")
	if got := errToString(err, ""); got != "plain failure" {
		t.Fatalf("errToString()=%q, want plain failure", got)
	}
}

func TestErrToString_IncludesStderrForExitErrors(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 5").Run()
	if err == nil {
		t.Fatal("expected exit error")
	}
	got := errToString(err, "Permission denied\nToo many authentication failures\n")
	if !strings.Contains(got, "command exited with code 5") {
		t.Fatalf("expected exit code in error, got=%q", got)
	}
	if !strings.Contains(got, "Permission denied") {
		t.Fatalf("expected stderr snippet in error, got=%q", got)
	}
}

func TestEmbeddedBuildkitManagerEnsureRunningSetsExecutorEnv(t *testing.T) {
	workspacePath := t.TempDir()
	mgr := NewEmbeddedBuildkitManager(logrus.New(), workspacePath, system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit})
	startCalls := 0
	mgr.startProcess = func(configPath, socketPath, stateDir, logPath string) (processHandle, error) {
		startCalls++
		return processHandle{}, nil
	}
	mgr.waitUntilReady = func(socketPath string) error { return nil }
	mgr.stopProcess = func(processHandle) error { return nil }

	if err := mgr.EnsureRunning([]string{"https://mirror-a.example"}); err != nil {
		t.Fatalf("ensure running failed: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls=%d, want 1", startCalls)
	}
	env := mgr.Env()
	if env["EASYDO_BUILDKIT_SOCKET_PATH"] == "" || env["EASYDO_BUILDKIT_CONFIG_PATH"] == "" {
		t.Fatalf("expected buildkit env to be populated, got %#v", env)
	}
	configBytes, err := os.ReadFile(env["EASYDO_BUILDKIT_CONFIG_PATH"])
	if err != nil {
		t.Fatalf("read buildkit config failed: %v", err)
	}
	configText := string(configBytes)
	if !strings.Contains(configText, `[registry."docker.io"]`) || !strings.Contains(configText, `"https://mirror-a.example"`) {
		t.Fatalf("expected mirror config in buildkit config, got:\n%s", configText)
	}
}

func TestEmbeddedBuildkitManagerDoesNotRestartWhenMirrorsUnchanged(t *testing.T) {
	mgr := NewEmbeddedBuildkitManager(logrus.New(), t.TempDir(), system.RuntimeCapabilities{PreferredBuildBackend: system.BuildBackendEmbeddedBuildkit})
	startCalls := 0
	mgr.startProcess = func(configPath, socketPath, stateDir, logPath string) (processHandle, error) {
		startCalls++
		return processHandle{pid: 1}, nil
	}
	mgr.waitUntilReady = func(socketPath string) error { return nil }
	mgr.stopProcess = func(processHandle) error { return nil }

	mirrors := []string{"https://mirror-a.example"}
	if err := mgr.EnsureRunning(mirrors); err != nil {
		t.Fatalf("first ensure failed: %v", err)
	}
	if err := mgr.EnsureRunning(mirrors); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls=%d, want 1", startCalls)
	}
}

func TestRunScript_KillsBackgroundProcessGroupOnContextCancel(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "child.pid")
	executor := &Executor{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, err := executor.runScript(ctx, 1, `sh -c 'trap "" HUP TERM INT; while true; do sleep 1; done' >/dev/null 2>&1 & child=$!; echo $child > "`+pidFile+`"; wait`, tmpDir, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		data, readErr := os.ReadFile(pidFile)
		if readErr == nil && strings.TrimSpace(string(data)) != "" {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("invalid child pid %q: %v", string(data), convErr)
			}
			killErr := syscall.Kill(pid, 0)
			if errors.Is(killErr, syscall.ESRCH) {
				break
			}
			if killErr != nil {
				t.Fatalf("unexpected kill check error for pid %d: %v", pid, killErr)
			}
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(pidFile)
			t.Fatalf("background child still alive after cancellation, pid=%s", strings.TrimSpace(string(data)))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
