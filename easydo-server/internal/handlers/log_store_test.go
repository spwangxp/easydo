package handlers

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileLogStoreAppendAndQuery(t *testing.T) {
	t.Setenv("EASYDO_LOG_DIR", t.TempDir())

	store := newFileLogStore()
	now := time.Now().Unix()

	if err := store.Append(fileLogEntry{
		TaskID:        11,
		PipelineRunID: 101,
		Level:         "info",
		Message:       "build started",
		Source:        "stdout",
		Timestamp:     now,
		LineNumber:    1,
	}); err != nil {
		t.Fatalf("append first log failed: %v", err)
	}
	if err := store.Append(fileLogEntry{
		TaskID:        12,
		PipelineRunID: 101,
		Level:         "error",
		Message:       "build failed",
		Source:        "stderr",
		Timestamp:     now + 1,
		LineNumber:    2,
	}); err != nil {
		t.Fatalf("append second log failed: %v", err)
	}

	runLogs, err := store.QueryRunLogs(101, "", "")
	if err != nil {
		t.Fatalf("query run logs failed: %v", err)
	}
	if len(runLogs) != 2 {
		t.Fatalf("expected 2 run logs, got %d", len(runLogs))
	}

	taskLogs, err := store.QueryTaskLogs(101, 11, "")
	if err != nil {
		t.Fatalf("query task logs failed: %v", err)
	}
	if len(taskLogs) != 1 {
		t.Fatalf("expected 1 task log, got %d", len(taskLogs))
	}
	if taskLogs[0].Message != "build started" {
		t.Fatalf("unexpected task log message: %q", taskLogs[0].Message)
	}

	errLogs, err := store.QueryRunLogs(101, "error", "")
	if err != nil {
		t.Fatalf("query run logs by level failed: %v", err)
	}
	if len(errLogs) != 1 || errLogs[0].Level != "error" {
		t.Fatalf("expected 1 error log, got %+v", errLogs)
	}

	stdoutLogs, err := store.QueryRunLogs(101, "", "stdout")
	if err != nil {
		t.Fatalf("query run logs by source failed: %v", err)
	}
	if len(stdoutLogs) != 1 || stdoutLogs[0].Source != "stdout" {
		t.Fatalf("expected 1 stdout log, got %+v", stdoutLogs)
	}
}

func TestFileLogStorePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EASYDO_LOG_DIR", dir)

	store := newFileLogStore()
	path := store.runLogFilePath(123)
	want := filepath.Join(dir, "run_123.log")
	if path != want {
		t.Fatalf("unexpected path: got %q want %q", path, want)
	}
}
