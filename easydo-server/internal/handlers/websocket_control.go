package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
)

type controlRelayEnvelope struct {
	CommandType   string                 `json:"command_type"`
	CommandID     string                 `json:"command_id"`
	TaskID        uint64                 `json:"task_id"`
	AgentID       uint64                 `json:"agent_id"`
	TargetServerID string                `json:"target_server_id"`
	AgentSessionID string                `json:"agent_session_id"`
	Payload       map[string]interface{} `json:"payload"`
}

func (h *WebSocketHandler) startControlRelayConsumer() {
	if h == nil || utils.RedisClient == nil {
		return
	}
	h.controlRelayOnce.Do(func() {
		go h.consumeControlRelay(context.Background())
	})
}

func (h *WebSocketHandler) consumeControlRelay(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if utils.RedisClient == nil {
			return
		}
		pubsub := utils.RedisClient.Subscribe(ctx, utils.ControlRelayTopic(h.serverID))
		if _, err := pubsub.Receive(ctx); err != nil {
			_ = pubsub.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		channel := pubsub.Channel()
		closed := false
		for !closed {
			select {
			case <-ctx.Done():
				_ = pubsub.Close()
				return
			case msg, ok := <-channel:
				if !ok {
					closed = true
					continue
				}
				envelope := controlRelayEnvelope{}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					continue
				}
				h.handleControlRelayEnvelope(envelope)
			}
		}
		_ = pubsub.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *WebSocketHandler) handleControlRelayEnvelope(envelope controlRelayEnvelope) {
	if h == nil || envelope.AgentID == 0 || envelope.CommandType == "" || len(envelope.Payload) == 0 {
		return
	}
	if strings.TrimSpace(envelope.TargetServerID) != "" && strings.TrimSpace(envelope.TargetServerID) != h.serverID {
		return
	}
	switch envelope.CommandType {
	case "task_cancel":
		if !h.validateTaskCancelRelay(envelope) {
			return
		}
	default:
		return
	}
	_ = h.sendMessageToLocalAgent(envelope.AgentID, envelope.CommandType, envelope.Payload)
}

func (h *WebSocketHandler) validateTaskCancelRelay(envelope controlRelayEnvelope) bool {
	if h == nil || envelope.AgentID == 0 || envelope.TaskID == 0 {
		return false
	}
	var task models.AgentTask
	if err := models.DB.Select("id", "agent_id", "status", "agent_session_id", "owner_server_id").First(&task, envelope.TaskID).Error; err != nil {
		return false
	}
	if task.AgentID != envelope.AgentID {
		return false
	}
	if task.Status != models.TaskStatusCancelRequested {
		return false
	}
	if task.OwnerServerID != "" && task.OwnerServerID != h.serverID {
		return false
	}
	if envelope.AgentSessionID != "" && task.AgentSessionID != "" && task.AgentSessionID != envelope.AgentSessionID {
		return false
	}
	return true
}

func (h *WebSocketHandler) publishControlRelay(targetServerID string, envelope controlRelayEnvelope) bool {
	if h == nil || utils.RedisClient == nil || strings.TrimSpace(targetServerID) == "" {
		return false
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return false
	}
	return utils.RedisClient.Publish(context.Background(), utils.ControlRelayTopic(strings.TrimSpace(targetServerID)), data).Err() == nil
}

func (h *WebSocketHandler) sendControlMessageToAgent(agentID uint64, msgType string, payload map[string]interface{}) bool {
	if h == nil || agentID == 0 || strings.TrimSpace(msgType) == "" || payload == nil {
		return false
	}
	if h.sendMessageToLocalAgent(agentID, msgType, payload) {
		return true
	}
	presence, err := utils.GetAgentPresence(context.Background(), agentID)
	if err != nil || presence == nil {
		return false
	}
	targetServerID := strings.TrimSpace(presence.ServerID)
	if targetServerID == "" || targetServerID == h.serverID {
		return false
	}
	return h.publishControlRelay(targetServerID, controlRelayEnvelope{
		CommandType:    msgType,
		CommandID:      getString(payload, "command_id"),
		TaskID:         uint64(getFloat64(payload, "task_id")),
		AgentID:        agentID,
		TargetServerID: targetServerID,
		AgentSessionID: strings.TrimSpace(presence.AgentSessionID),
		Payload:        payload,
	})
}
