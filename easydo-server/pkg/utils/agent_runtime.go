package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"easydo-server/internal/config"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	serverIDOnce sync.Once
	serverID     string
)

const (
	agentPresenceKeyPrefix = "easydo:agent:presence:"
	agentStreamKeyPrefix   = "easydo:agent:stream:"
	frontendRealtimeTopic  = "easydo:frontend:realtime"
	InternalTokenHeader    = "X-EasyDo-Internal-Token"
)

type FrontendRealtimeEvent struct {
	OriginServerID string                 `json:"origin_server_id"`
	RunID          uint64                 `json:"run_id"`
	Type           string                 `json:"type"`
	Payload        map[string]interface{} `json:"payload"`
}

type AgentPresence struct {
	AgentID           uint64            `json:"agent_id"`
	AgentSessionID    string            `json:"agent_session_id"`
	ServerID          string            `json:"server_id"`
	ServerURL         string            `json:"server_url"`
	Status            string            `json:"status"`
	LastHeartbeatAt   int64             `json:"last_heartbeat_at"`
	HeartbeatInterval int               `json:"heartbeat_interval"`
	CPUUsage          float64           `json:"cpu_usage"`
	MemoryUsage       float64           `json:"memory_usage"`
	DiskUsage         float64           `json:"disk_usage"`
	TasksRunning      int               `json:"tasks_running"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

func ServerInternalURL() string {
	if v := strings.TrimSpace(config.Config.GetString("server.internal_url")); v != "" {
		return strings.TrimRight(v, "/")
	}
	port := strings.TrimSpace(config.Config.GetString("server.port"))
	if port == "" {
		port = "8080"
	}
	return "http://127.0.0.1:" + port
}

func ServerInternalToken() string {
	return strings.TrimSpace(config.Config.GetString("server.internal_token"))
}

type AgentStreamEvent struct {
	TaskID          uint64 `json:"task_id"`
	DispatchToken   string `json:"dispatch_token"`
	DispatchAttempt int    `json:"dispatch_attempt"`
	CreatedAt       int64  `json:"created_at"`
}

func ServerID() string {
	serverIDOnce.Do(func() {
		if id := strings.TrimSpace(config.Config.GetString("server.id")); id != "" {
			serverID = id
			return
		}
		serverID = "srv-" + uuid.NewString()
	})
	return serverID
}

func AgentPresenceKey(agentID uint64) string {
	return agentPresenceKeyPrefix + formatUint64(agentID)
}

func AgentStreamKey(agentID uint64) string {
	return agentStreamKeyPrefix + formatUint64(agentID)
}

func FrontendRealtimeTopic() string {
	return frontendRealtimeTopic
}

func PutAgentPresence(ctx context.Context, presence AgentPresence) error {
	if RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}
	if presence.AgentID == 0 || presence.AgentSessionID == "" || presence.ServerID == "" {
		return fmt.Errorf("invalid agent presence")
	}
	if presence.LastHeartbeatAt == 0 {
		presence.LastHeartbeatAt = time.Now().Unix()
	}
	data, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	ttl := time.Duration(maxInt(presence.HeartbeatInterval*3, 30)) * time.Second
	return RedisClient.Set(ctx, AgentPresenceKey(presence.AgentID), data, ttl).Err()
}

func GetAgentPresence(ctx context.Context, agentID uint64) (*AgentPresence, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}
	if agentID == 0 {
		return nil, redis.Nil
	}
	data, err := RedisClient.Get(ctx, AgentPresenceKey(agentID)).Bytes()
	if err != nil {
		return nil, err
	}
	var presence AgentPresence
	if err := json.Unmarshal(data, &presence); err != nil {
		return nil, err
	}
	return &presence, nil
}

func DeleteAgentPresence(ctx context.Context, agentID uint64, sessionID string) error {
	presence, err := GetAgentPresence(ctx, agentID)
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		return err
	}
	if sessionID != "" && presence.AgentSessionID != sessionID {
		return nil
	}
	return RedisClient.Del(ctx, AgentPresenceKey(agentID)).Err()
}

func PublishAgentStreamEvent(ctx context.Context, agentID uint64, event AgentStreamEvent) (string, error) {
	if RedisClient == nil {
		return "", fmt.Errorf("redis client not initialized")
	}
	if agentID == 0 || event.TaskID == 0 || event.DispatchToken == "" || event.DispatchAttempt <= 0 {
		return "", fmt.Errorf("invalid agent stream event")
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	return RedisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: AgentStreamKey(agentID),
		Values: map[string]interface{}{
			"task_id":          event.TaskID,
			"dispatch_token":   event.DispatchToken,
			"dispatch_attempt": event.DispatchAttempt,
			"created_at":       event.CreatedAt,
		},
		MaxLen: 1024,
		Approx: true,
	}).Result()
}

func ParseAgentStreamEvent(msg redis.XMessage) (AgentStreamEvent, error) {
	event := AgentStreamEvent{}
	if v, ok := msg.Values["task_id"]; ok {
		event.TaskID = toUint64(v)
	}
	if v, ok := msg.Values["dispatch_token"]; ok {
		event.DispatchToken = fmt.Sprintf("%v", v)
	}
	if v, ok := msg.Values["dispatch_attempt"]; ok {
		event.DispatchAttempt = int(toUint64(v))
	}
	if v, ok := msg.Values["created_at"]; ok {
		event.CreatedAt = int64(toUint64(v))
	}
	if event.TaskID == 0 || event.DispatchToken == "" || event.DispatchAttempt <= 0 {
		return event, fmt.Errorf("invalid stream event")
	}
	return event, nil
}

func PublishFrontendRealtimeEvent(ctx context.Context, event FrontendRealtimeEvent) error {
	if RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}
	if event.RunID == 0 || event.Type == "" || len(event.Payload) == 0 {
		return fmt.Errorf("invalid frontend realtime event")
	}
	if strings.TrimSpace(event.OriginServerID) == "" {
		event.OriginServerID = ServerID()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return RedisClient.Publish(ctx, FrontendRealtimeTopic(), data).Err()
}

func ParseFrontendRealtimeEvent(raw string) (FrontendRealtimeEvent, error) {
	var event FrontendRealtimeEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return event, err
	}
	if event.RunID == 0 || event.Type == "" || len(event.Payload) == 0 {
		return event, fmt.Errorf("invalid frontend realtime event")
	}
	return event, nil
}

func toUint64(v interface{}) uint64 {
	switch val := v.(type) {
	case uint64:
		return val
	case int64:
		if val < 0 {
			return 0
		}
		return uint64(val)
	case int:
		if val < 0 {
			return 0
		}
		return uint64(val)
	case string:
		var out uint64
		fmt.Sscanf(val, "%d", &out)
		return out
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
