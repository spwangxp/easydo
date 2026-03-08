package handlers

import (
	"easydo-server/internal/models"

	"gorm.io/gorm"
)

func recordAgentHeartbeat(db *gorm.DB, ws *WebSocketHandler, heartbeat models.AgentHeartbeat) {
	if heartbeat.AgentID == 0 || heartbeat.Timestamp == 0 {
		return
	}

	if ws != nil {
		ws.storeHeartbeat(heartbeat.AgentID, heartbeat)
	}

	if db != nil {
		_ = db.Create(&heartbeat).Error
	}
}
