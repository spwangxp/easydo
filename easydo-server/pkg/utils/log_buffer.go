package utils

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"

	"gorm.io/gorm"
)

type LogBufferManager struct {
	db            *gorm.DB
	flushInterval time.Duration
	maxBatchSize  int
	stopChan      chan struct{}
}

func NewLogBufferManager(db *gorm.DB, flushInterval time.Duration, maxBatchSize int) *LogBufferManager {
	if flushInterval <= 0 {
		flushInterval = 5 * time.Second
	}
	if maxBatchSize <= 0 {
		maxBatchSize = 1000
	}

	return &LogBufferManager{
		db:            db,
		flushInterval: flushInterval,
		maxBatchSize:  maxBatchSize,
		stopChan:      make(chan struct{}),
	}
}

func (m *LogBufferManager) Start() {
	ticker := time.NewTicker(m.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.flushAllBuffers()
		case <-m.stopChan:
			return
		}
	}
}

func (m *LogBufferManager) Stop() {
	close(m.stopChan)
	m.flushAllBuffers()
}

func (m *LogBufferManager) FlushTaskLogs(runID, taskID uint64) error {
	return m.flushBuffer(runID, taskID)
}

func (m *LogBufferManager) flushAllBuffers() {
	keys, err := GetActiveLogKeys()
	if err != nil {
		return
	}

	for _, key := range keys {
		runID, taskID := parseLogKey(key)
		if runID > 0 && taskID > 0 {
			m.flushBuffer(runID, taskID)
		}
	}
}

func (m *LogBufferManager) flushBuffer(runID, taskID uint64) error {
	logs, err := GetLogsFromBuffer(runID, taskID)
	if err != nil {
		return err
	}

	if len(logs) == 0 {
		return nil
	}

	agentLogs := make([]models.AgentLog, 0, len(logs))
	for _, logJSON := range logs {
		var logData map[string]interface{}
		if err := json.Unmarshal([]byte(logJSON), &logData); err != nil {
			continue
		}

		agentLog := models.AgentLog{
			TaskID:    getUint64FromInterface(logData["task_id"]),
			Level:     getStringFromInterface(logData["level"]),
			Message:   getStringFromInterface(logData["message"]),
			Timestamp: getInt64FromInterface(logData["timestamp"]),
			Source:    getStringFromInterface(logData["source"]),
		}

		if agentLog.TaskID == 0 {
			agentLog.TaskID = taskID
		}
		if agentLog.Timestamp == 0 {
			agentLog.Timestamp = time.Now().UnixMilli()
		}

		agentLogs = append(agentLogs, agentLog)
	}

	if len(agentLogs) == 0 {
		return nil
	}

	if err := m.db.CreateInBatches(agentLogs, m.maxBatchSize).Error; err != nil {
		return err
	}

	return ClearLogBuffer(runID, taskID)
}

func parseLogKey(key string) (uint64, uint64) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return 0, 0
	}

	runID, _ := strconv.ParseUint(parts[1], 10, 64)
	taskID, _ := strconv.ParseUint(parts[2], 10, 64)
	return runID, taskID
}

func getUint64FromInterface(v interface{}) uint64 {
	switch val := v.(type) {
	case float64:
		return uint64(val)
	case int:
		return uint64(val)
	case int64:
		return uint64(val)
	case uint64:
		return val
	case string:
		id, _ := strconv.ParseUint(val, 10, 64)
		return id
	}
	return 0
}

func getInt64FromInterface(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	case string:
		timestamp, _ := strconv.ParseInt(val, 10, 64)
		return timestamp
	}
	return 0
}

func getStringFromInterface(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	}
	return ""
}
