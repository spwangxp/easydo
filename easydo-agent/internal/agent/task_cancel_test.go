package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"easydo-agent/internal/config"
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
	statBytes, readErr := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
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

func TestHandleTaskCancel_CancelsRunningTaskProcessGroup(t *testing.T) {
	workspaceRoot := t.TempDir()
	cfg := &config.Config{}
	cfg.Task.WorkspacePath = workspaceRoot

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	h := NewTaskHandler(nil, nil, cfg, nil, system.RuntimeCapabilities{}, logger)
	h.Start(context.Background())

	pidFile := filepath.Join(workspaceRoot, "child.pid")
	assignedTask := &Task{
		ID:            101,
		PipelineRunID: 55,
		NodeID:        "node-cancel",
		TaskType:      "shell",
		Name:          "long-running-shell",
		Timeout:       30,
		Script:        `sh -c 'trap "" HUP TERM INT; while true; do sleep 1; done' >/dev/null 2>&1 & child=$!; echo $child > "` + pidFile + `"; wait`,
	}
	params, err := assignedTask.ParseParams()
	if err != nil {
		t.Fatalf("ParseParams returned error: %v", err)
	}

	taskCtx, cancel := context.WithCancel(context.Background())
	h.runningTasks.Store(assignedTask.ID, &runningTaskExecution{cancel: cancel})
	defer h.runningTasks.Delete(assignedTask.ID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.executor.Execute(taskCtx, *params, nil)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for child pid file")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := h.HandleTaskCancel(assignedTask.ID); err != nil {
		t.Fatalf("HandleTaskCancel returned error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("task execution did not stop after cancellation")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid file failed: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse child pid failed: %v", err)
	}
	terminated, state, killErr := processTerminated(pid)
	if !terminated {
		t.Fatalf("expected child process to be terminated after cancellation, state=%q killErr=%v", state, killErr)
	}
}

func TestHandleTaskCancel_ReturnsErrorForUnknownTask(t *testing.T) {
	logger := logrus.New()
	h := &TaskHandler{log: logger}
	if err := h.HandleTaskCancel(999); err == nil {
		t.Fatal("expected error for unknown task")
	}
}
