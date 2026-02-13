package models

import (
	"fmt"
	"time"
)

// Message represents a system notification/message
type Message struct {
	BaseModel
	Type       string `gorm:"size:32;not null" json:"type"`       // message, alert, warning
	Title      string `gorm:"size:256;not null" json:"title"`     // Message title
	Content    string `gorm:"type:longtext" json:"content"`       // Message content
	SenderID   *uint64 `gorm:"index" json:"sender_id"`            // Sender user ID (nil for system)
	SenderType string `gorm:"size:32;default:'system'" json:"sender_type"` // system, user
	Priority   int    `gorm:"default:0" json:"priority"`          // 0: normal, 1: important, 2: urgent
	IsRead     bool   `gorm:"default:false" json:"is_read"`       // Whether message has been read
	ReadAt     int64  `json:"read_at"`                            // Read timestamp
	Metadata   string `gorm:"type:text" json:"metadata"`          // JSON metadata (e.g., agent_id, alert_type)
	
	Sender *User `gorm:"foreignKey:SenderID" json:"sender"`
}

// MessageType constants
const (
	MessageTypeSystem  = "system"
	MessageTypeAlert   = "alert"
	MessageTypeWarning = "warning"
)

// CreateAgentOfflineAlert creates an offline alert message for an agent
func CreateAgentOfflineAlert(agentID uint64, agentName string, lastHeartAt int64) *Message {
	content := fmt.Sprintf("执行器 \"%s\" (ID: %d) 已离线，最后心跳时间: %s",
		agentName, agentID, formatTimestamp(lastHeartAt))
	
	return &Message{
		Type:       MessageTypeAlert,
		Title:      "执行器离线警告",
		Content:    content,
		SenderType: "system",
		Priority:   1,
		Metadata:   fmt.Sprintf(`{"agent_id": %d, "agent_name": "%s", "last_heart_at": %d}`, agentID, agentName, lastHeartAt),
	}
}

func formatTimestamp(ts int64) string {
	if ts == 0 {
		return "未知"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}
