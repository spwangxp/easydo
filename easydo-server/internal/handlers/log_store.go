package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"easydo-server/internal/models"
)

type fileLogEntry struct {
	TaskID        uint64 `json:"task_id"`
	PipelineRunID uint64 `json:"pipeline_run_id"`
	Level         string `json:"level"`
	Message       string `json:"message"`
	Source        string `json:"source"`
	Timestamp     int64  `json:"timestamp"`
	LineNumber    int    `json:"line_number,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	Seq           int64  `json:"seq,omitempty"`
}

type fileLogStore struct {
	baseDir string
	muMap   sync.Map // runID(string) -> *sync.Mutex
}

func newFileLogStore() *fileLogStore {
	baseDir := os.Getenv("EASYDO_LOG_DIR")
	if baseDir == "" {
		baseDir = "data/agent-logs"
	}
	return &fileLogStore{baseDir: baseDir}
}

func (s *fileLogStore) runLogFilePath(runID uint64) string {
	return filepath.Join(s.baseDir, fmt.Sprintf("run_%d.log", runID))
}

func (s *fileLogStore) lockForRun(runID uint64) *sync.Mutex {
	key := fmt.Sprintf("%d", runID)
	v, _ := s.muMap.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *fileLogStore) Append(entry fileLogEntry) error {
	if entry.PipelineRunID == 0 {
		return fmt.Errorf("pipeline_run_id is required")
	}

	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("create log directory failed: %w", err)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry failed: %w", err)
	}

	mu := s.lockForRun(entry.PipelineRunID)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(s.runLogFilePath(entry.PipelineRunID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open run log file failed: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append run log file failed: %w", err)
	}

	return nil
}

func (s *fileLogStore) QueryRunLogs(runID uint64, level, source string) ([]models.AgentLog, error) {
	entries, err := s.readRunFile(runID)
	if err != nil {
		return nil, err
	}

	result := make([]models.AgentLog, 0, len(entries))
	for i := range entries {
		e := entries[i]
		if level != "" && !strings.EqualFold(e.Level, level) {
			continue
		}
		if source != "" && !strings.EqualFold(e.Source, source) {
			continue
		}
		result = append(result, models.AgentLog{
			TaskID:        e.TaskID,
			PipelineRunID: e.PipelineRunID,
			Level:         e.Level,
			Message:       e.Message,
			Timestamp:     e.Timestamp,
			Source:        e.Source,
		})
	}
	return result, nil
}

func (s *fileLogStore) QueryTaskLogs(runID uint64, taskID uint64, level string) ([]models.AgentLog, error) {
	entries, err := s.readRunFile(runID)
	if err != nil {
		return nil, err
	}

	result := make([]models.AgentLog, 0, len(entries))
	for i := range entries {
		e := entries[i]
		if e.TaskID != taskID {
			continue
		}
		if level != "" && !strings.EqualFold(e.Level, level) {
			continue
		}
		result = append(result, models.AgentLog{
			TaskID:        e.TaskID,
			PipelineRunID: e.PipelineRunID,
			Level:         e.Level,
			Message:       e.Message,
			Timestamp:     e.Timestamp,
			Source:        e.Source,
		})
	}
	return result, nil
}

func (s *fileLogStore) readRunFile(runID uint64) ([]fileLogEntry, error) {
	path := s.runLogFilePath(runID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []fileLogEntry{}, nil
		}
		return nil, fmt.Errorf("open run log file failed: %w", err)
	}
	defer f.Close()

	entries := make([]fileLogEntry, 0, 256)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e fileLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan run log file failed: %w", err)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp == entries[j].Timestamp {
			if entries[i].Seq == entries[j].Seq {
				return entries[i].LineNumber < entries[j].LineNumber
			}
			return entries[i].Seq < entries[j].Seq
		}
		return entries[i].Timestamp < entries[j].Timestamp
	})

	return entries, nil
}

var agentFileLogs = newFileLogStore()
