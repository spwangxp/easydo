package handlers

import (
	"testing"
	"time"

	"easydo-server/internal/models"
)

func TestTaskLogStoreAppendAndQuery(t *testing.T) {
	models.DB = openHandlerTestDB(t)
	store := newTaskLogStore()
	now := time.Now().Unix()

	if err := store.Append(fileLogEntry{
		AgentID:       1,
		TaskID:        11,
		PipelineRunID: 101,
		Level:         "info",
		Message:       "build started",
		Source:        "stdout",
		Timestamp:     now,
		LineNumber:    1,
		Attempt:       1,
		Seq:           1,
	}); err != nil {
		t.Fatalf("append first log failed: %v", err)
	}
	if err := store.Append(fileLogEntry{
		AgentID:       1,
		TaskID:        12,
		PipelineRunID: 101,
		Level:         "error",
		Message:       "build failed",
		Source:        "stderr",
		Timestamp:     now + 1,
		LineNumber:    2,
		Attempt:       1,
		Seq:           2,
	}); err != nil {
		t.Fatalf("append second log failed: %v", err)
	}

	runLogs, err := store.QueryRunLogs(101, 0, "", "")
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

	errLogs, err := store.QueryRunLogs(101, 0, "error", "")
	if err != nil {
		t.Fatalf("query run logs by level failed: %v", err)
	}
	if len(errLogs) != 1 || errLogs[0].Level != "error" {
		t.Fatalf("expected 1 error log, got %+v", errLogs)
	}

	stdoutLogs, err := store.QueryRunLogs(101, 0, "", "stdout")
	if err != nil {
		t.Fatalf("query run logs by source failed: %v", err)
	}
	if len(stdoutLogs) != 1 || stdoutLogs[0].Source != "stdout" {
		t.Fatalf("expected 1 stdout log, got %+v", stdoutLogs)
	}
}

func TestTaskLogStoreLiveQuery(t *testing.T) {
	models.DB = openHandlerTestDB(t)
	store := newTaskLogStore()
	if err := store.Append(fileLogEntry{AgentID: 2, TaskID: 21, PipelineRunID: 201, Level: "info", Message: "one", Source: "stdout", Timestamp: time.Now().Unix(), Attempt: 1, Seq: 1}); err != nil {
		t.Fatalf("append log failed: %v", err)
	}
	if err := store.Append(fileLogEntry{AgentID: 2, TaskID: 21, PipelineRunID: 201, Level: "info", Message: "two", Source: "stdout", Timestamp: time.Now().Unix(), Attempt: 1, Seq: 2}); err != nil {
		t.Fatalf("append log failed: %v", err)
	}
	entries, err := store.QueryLiveTaskLogs(21, 1, 1)
	if err != nil {
		t.Fatalf("query live logs failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Message != "two" {
		t.Fatalf("unexpected live log query result: %+v", entries)
	}
}

func TestTaskLogStoreQueryTaskLogs_IncludesPersistedChunkRowsAfterOwnerCrash(t *testing.T) {
	models.DB = openHandlerTestDB(t)
	store := newTaskLogStore()
	now := time.Now().Unix()

	chunks := []models.AgentLogChunk{
		{TaskID: 31, PipelineRunID: 301, AgentID: 1, AgentSessionID: "session-a", Attempt: 1, Seq: 1, Stream: "stdout", Chunk: "start", Timestamp: now, UniqueKey: "31:1:1"},
		{TaskID: 31, PipelineRunID: 301, AgentID: 1, AgentSessionID: "session-a", Attempt: 1, Seq: 2, Stream: "stdout", Chunk: "mid", Timestamp: now + 1, UniqueKey: "31:1:2"},
		{TaskID: 31, PipelineRunID: 301, AgentID: 1, AgentSessionID: "session-b", Attempt: 1, Seq: 3, Stream: "stdout", Chunk: "done", Timestamp: now + 2, UniqueKey: "31:1:3"},
	}
	for _, chunk := range chunks {
		copy := chunk
		if err := models.DB.Create(&copy).Error; err != nil {
			t.Fatalf("create log chunk failed: %v", err)
		}
	}

	logs, err := store.QueryTaskLogs(301, 31, "")
	if err != nil {
		t.Fatalf("query task logs failed: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs from persisted chunks, got %d: %+v", len(logs), logs)
	}
	if logs[0].Message != "start" || logs[1].Message != "mid" || logs[2].Message != "done" {
		t.Fatalf("unexpected log ordering/messages: %+v", logs)
	}
}

func TestTaskLogStoreQueryTaskLogs_DedupesPersistedChunksAndLiveBuffer(t *testing.T) {
	models.DB = openHandlerTestDB(t)
	store := newTaskLogStore()
	now := time.Now().Unix()

	chunk := models.AgentLogChunk{TaskID: 41, PipelineRunID: 401, AgentID: 1, AgentSessionID: "session-a", Attempt: 1, Seq: 1, Stream: "stdout", Chunk: "only-once", Timestamp: now, UniqueKey: "41:1:1"}
	if err := models.DB.Create(&chunk).Error; err != nil {
		t.Fatalf("create log chunk failed: %v", err)
	}
	if err := store.Append(fileLogEntry{AgentID: 1, TaskID: 41, PipelineRunID: 401, Level: "info", Message: "only-once", Source: "stdout", Timestamp: now, Attempt: 1, Seq: 1}); err != nil {
		t.Fatalf("append live log failed: %v", err)
	}

	logs, err := store.QueryTaskLogs(401, 41, "")
	if err != nil {
		t.Fatalf("query task logs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected deduped single log, got %d: %+v", len(logs), logs)
	}
	if logs[0].Message != "only-once" {
		t.Fatalf("unexpected log message: %+v", logs)
	}
}

func TestQueryRunLogs_FiltersByTaskID(t *testing.T) {
	models.DB = openHandlerTestDB(t)
	store := newTaskLogStore()
	now := time.Now().Unix()

	entries := []fileLogEntry{
		{AgentID: 1, TaskID: 10, PipelineRunID: 100, Level: "info", Message: "task 10 log 1", Source: "stdout", Timestamp: now, Attempt: 1, Seq: 1},
		{AgentID: 1, TaskID: 10, PipelineRunID: 100, Level: "info", Message: "task 10 log 2", Source: "stdout", Timestamp: now + 1, Attempt: 1, Seq: 2},
		{AgentID: 1, TaskID: 20, PipelineRunID: 100, Level: "info", Message: "task 20 log 1", Source: "stdout", Timestamp: now + 2, Attempt: 1, Seq: 3},
		{AgentID: 1, TaskID: 20, PipelineRunID: 100, Level: "error", Message: "task 20 error", Source: "stderr", Timestamp: now + 3, Attempt: 1, Seq: 4},
	}
	for _, entry := range entries {
		if err := store.Append(entry); err != nil {
			t.Fatalf("append log failed: %v", err)
		}
	}

	allLogs, err := store.QueryRunLogs(100, 0, "", "")
	if err != nil {
		t.Fatalf("query all run logs failed: %v", err)
	}
	if len(allLogs) != 4 {
		t.Fatalf("expected 4 logs without task_id filter, got %d", len(allLogs))
	}

	task10Logs, err := store.QueryRunLogs(100, 10, "", "")
	if err != nil {
		t.Fatalf("query run logs with task_id=10 failed: %v", err)
	}
	if len(task10Logs) != 2 {
		t.Fatalf("expected 2 logs for task_id=10, got %d", len(task10Logs))
	}
	for _, log := range task10Logs {
		if log.TaskID != 10 {
			t.Fatalf("expected task_id=10, got %d", log.TaskID)
		}
	}

	task20Logs, err := store.QueryRunLogs(100, 20, "", "")
	if err != nil {
		t.Fatalf("query run logs with task_id=20 failed: %v", err)
	}
	if len(task20Logs) != 2 {
		t.Fatalf("expected 2 logs for task_id=20, got %d", len(task20Logs))
	}
	for _, log := range task20Logs {
		if log.TaskID != 20 {
			t.Fatalf("expected task_id=20, got %d", log.TaskID)
		}
	}

	task20ErrorLogs, err := store.QueryRunLogs(100, 20, "error", "")
	if err != nil {
		t.Fatalf("query run logs with task_id=20 and level=error failed: %v", err)
	}
	if len(task20ErrorLogs) != 1 {
		t.Fatalf("expected 1 error log for task_id=20, got %d", len(task20ErrorLogs))
	}
	if task20ErrorLogs[0].Level != "error" || task20ErrorLogs[0].TaskID != 20 {
		t.Fatalf("unexpected log: %+v", task20ErrorLogs[0])
	}
}
