package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"easydo-agent/internal/system"
	"github.com/sirupsen/logrus"
)

func processTerminated(pid int) (bool, string, error) {
	killErr := syscall.Kill(pid, 0)
	if errors.Is(killErr, syscall.ESRCH) {
		return true, "", killErr
	}
	if killErr != nil {
		return false, "", killErr
	}
	statBytes, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return true, "", readErr
		}
		return false, "", readErr
	}
	fields := strings.Fields(string(statBytes))
	if len(fields) >= 3 {
		return fields[2] == "Z", fields[2], nil
	}
	return false, "", nil
}

func TestExecutorHasNoAIAgentExecutionPath(t *testing.T) {
	if _, err := os.Stat("ai_task.go"); !os.IsNotExist(err) {
		t.Fatalf("easydo-agent must not contain ai_task.go, stat err=%v", err)
	}
	for _, path := range []string{"executor.go", "../agent/task.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, forbidden := range []string{"isAITaskParams", "executeAITask", "IsAITaskPayload", "getAITaskOutputs"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s must not contain AI Agent execution coupling %q", path, forbidden)
			}
		}
	}
}

func TestExecutorConcurrentLogCallbacks(t *testing.T) {
	executor := NewExecutor(logrus.New(), t.TempDir(), system.RuntimeCapabilities{})
	workspacePath := t.TempDir()
	executor.workspace = NewWorkspaceManager(workspacePath, logrus.New())

	taskA := TaskParams{
		TaskID:        201,
		PipelineRunID: 1,
		TaskType:      "shell",
		Name:          "task-a",
		Script:        `printf 'task-a\n'; sleep 0.2; printf 'task-a-done\n'`,
		Timeout:       5,
	}
	taskB := TaskParams{
		TaskID:        202,
		PipelineRunID: 2,
		TaskType:      "shell",
		Name:          "task-b",
		Script:        `sleep 0.05; printf 'task-b\n'; printf 'task-b-done\n'`,
		Timeout:       5,
	}

	var mu sync.Mutex
	logs := map[uint64][]string{}
	makeCallback := func(owner uint64) LogCallback {
		return func(taskID uint64, level, message, source string, lineNumber int) {
			if taskID != owner {
				return
			}
			mu.Lock()
			logs[owner] = append(logs[owner], fmt.Sprintf("%s:%s", source, message))
			mu.Unlock()
		}
	}

	readyForTaskB := make(chan struct{})
	startTaskA := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		callback := makeCallback(taskA.TaskID)
		close(readyForTaskB)
		<-startTaskA
		_ = executor.Execute(context.Background(), taskA, callback)
	}()
	go func() {
		defer wg.Done()
		<-readyForTaskB
		callback := makeCallback(taskB.TaskID)
		close(startTaskA)
		_ = executor.Execute(context.Background(), taskB, callback)
	}()
	wg.Wait()

	if len(logs[taskA.TaskID]) == 0 {
		t.Fatalf("expected task A logs, got none: %#v", logs)
	}
	if len(logs[taskB.TaskID]) == 0 {
		t.Fatalf("expected task B logs, got none: %#v", logs)
	}
	if got := strings.Join(logs[taskA.TaskID], "\n"); !strings.Contains(got, "task-a") || !strings.Contains(got, "task-a-done") {
		t.Fatalf("expected task A callback to capture both lines, got=%q", got)
	}
	if got := strings.Join(logs[taskB.TaskID], "\n"); !strings.Contains(got, "task-b") || !strings.Contains(got, "task-b-done") {
		t.Fatalf("expected task B callback to capture both lines, got=%q", got)
	}
}

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
	stdout, stderr, err := executor.runScript(context.Background(), 1, `command -v sh >/dev/null && printf '%s' "$PATH"`, "sh", "/tmp", map[string]string{"EASYDO_FLAG": "1"}, nil)
	if err != nil {
		t.Fatalf("expected runScript to preserve PATH, got err=%v stderr=%s", err, stderr)
	}
	if stdout == "" {
		t.Fatalf("expected PATH to remain available when custom env is provided")
	}
}

func TestRunScript_UsesExplicitBashWhenRequested(t *testing.T) {
	executor := &Executor{}
	stdout, stderr, err := executor.runScript(context.Background(), 1, `for i in {1..3}; do printf '%s' "$i"; done`, "bash", "/tmp", nil, nil)
	if err != nil {
		t.Fatalf("expected bash script to succeed, got err=%v stderr=%s", err, stderr)
	}
	if stdout != "123" {
		t.Fatalf("stdout=%q, want 123", stdout)
	}
}

func TestRunScript_PreservesLongSingleLineStdout(t *testing.T) {
	executor := &Executor{}
	stdout, stderr, err := executor.runScript(
		context.Background(),
		1,
		`dd if=/dev/zero bs=70000 count=1 2>/dev/null | tr '\000' 'a'; printf '\n'`,
		"sh",
		"/tmp",
		nil,
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

	_, _, err := executor.runScript(ctx, 1, `sh -c 'trap "" HUP TERM INT; while true; do sleep 1; done' >/dev/null 2>&1 & child=$!; echo $child > "`+pidFile+`"; wait`, "sh", tmpDir, nil, nil)
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
			terminated, _, killErr := processTerminated(pid)
			if terminated {
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
