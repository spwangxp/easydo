package handlers

import (
	"easydo-server/internal/models"
	"sync"

	"gorm.io/gorm"
)

const (
	heartbeatDBSampleIntervalSeconds int64 = 60
	heartbeatScheduleDebounceSeconds int64 = 60
)

var (
	heartbeatDBSampleMu          sync.Mutex
	heartbeatLastStoredAt        = make(map[uint64]int64)
	heartbeatScheduleDebounceMu  sync.Mutex
	heartbeatLastScheduleAt      = make(map[uint64]int64)
	heartbeatScheduleTestCounter int
)

func resetHeartbeatSamplingForTest() {
	heartbeatDBSampleMu.Lock()
	defer heartbeatDBSampleMu.Unlock()
	heartbeatLastStoredAt = make(map[uint64]int64)
}

func resetHeartbeatSchedulerDebounceForTest() {
	heartbeatScheduleDebounceMu.Lock()
	defer heartbeatScheduleDebounceMu.Unlock()
	heartbeatLastScheduleAt = make(map[uint64]int64)
	heartbeatScheduleTestCounter = 0
}

func heartbeatSchedulerDebounceCount() int {
	heartbeatScheduleDebounceMu.Lock()
	defer heartbeatScheduleDebounceMu.Unlock()
	return heartbeatScheduleTestCounter
}

func shouldPersistHeartbeatToDB(heartbeat models.AgentHeartbeat) bool {
	if heartbeat.AgentID == 0 || heartbeat.Timestamp == 0 {
		return false
	}
	heartbeatDBSampleMu.Lock()
	defer heartbeatDBSampleMu.Unlock()
	lastStoredAt := heartbeatLastStoredAt[heartbeat.AgentID]
	if lastStoredAt > 0 && heartbeat.Timestamp-lastStoredAt < heartbeatDBSampleIntervalSeconds {
		return false
	}
	heartbeatLastStoredAt[heartbeat.AgentID] = heartbeat.Timestamp
	return true
}

func shouldScheduleQueuedRunsFromHeartbeat(agentID uint64, timestamp int64) bool {
	if agentID == 0 || timestamp == 0 {
		return false
	}
	heartbeatScheduleDebounceMu.Lock()
	defer heartbeatScheduleDebounceMu.Unlock()
	lastScheduledAt := heartbeatLastScheduleAt[agentID]
	if lastScheduledAt > 0 && timestamp-lastScheduledAt < heartbeatScheduleDebounceSeconds {
		return false
	}
	heartbeatLastScheduleAt[agentID] = timestamp
	heartbeatScheduleTestCounter++
	return true
}

func recordAgentHeartbeat(db *gorm.DB, ws *WebSocketHandler, heartbeat models.AgentHeartbeat) {
	if heartbeat.AgentID == 0 || heartbeat.Timestamp == 0 {
		return
	}

	if ws != nil {
		ws.storeHeartbeat(heartbeat.AgentID, heartbeat)
	}

	if db != nil && shouldPersistHeartbeatToDB(heartbeat) {
		_ = db.Create(&heartbeat).Error
	}
}
