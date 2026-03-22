package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"easydo-agent/internal/client"
	"easydo-agent/internal/terminal"
	"github.com/sirupsen/logrus"
)

type terminalMessageSender interface {
	SendMessage(msgType string, payload map[string]interface{}) error
	GetSessionID() string
	IsConnected() bool
}

type TerminalHandler struct {
	log     *logrus.Logger
	runtime *terminal.Runtime

	mu sync.RWMutex
	ws terminalMessageSender
}

func NewTerminalHandler(log *logrus.Logger) *TerminalHandler {
	handler := &TerminalHandler{log: log}
	handler.runtime = terminal.NewRuntime(nil, handler)
	return handler
}

func (h *TerminalHandler) SetWebSocketClient(ws terminalMessageSender) {
	h.mu.Lock()
	h.ws = ws
	h.mu.Unlock()
}

func (h *TerminalHandler) HandleTerminalSessionOpen(msg *client.TerminalSessionOpenMessage) error {
	if msg == nil {
		return fmt.Errorf("terminal open message is nil")
	}
	return h.runtime.Open(context.Background(), terminal.OpenRequest{
		SessionID: msg.SessionID,
		Endpoint:  msg.Endpoint,
		Credential: terminal.Credential{
			ID:       msg.Credential.ID,
			Type:     msg.Credential.Type,
			Category: msg.Credential.Category,
			Payload:  msg.Credential.Payload,
		},
		Cols: msg.Cols,
		Rows: msg.Rows,
	})
}

func (h *TerminalHandler) HandleTerminalSessionInput(sessionID, data string) error {
	return h.runtime.Input(sessionID, data)
}

func (h *TerminalHandler) HandleTerminalSessionResize(sessionID string, cols, rows int) error {
	return h.runtime.Resize(sessionID, cols, rows)
}

func (h *TerminalHandler) HandleTerminalSessionClose(sessionID, reason string) error {
	return h.runtime.Close(sessionID, reason)
}

func (h *TerminalHandler) HandleTerminalSessionRootSwitch(sessionID string) error {
	return h.runtime.RootSwitch(sessionID)
}

func (h *TerminalHandler) CloseAll(reason string) {
	h.runtime.CloseAll(reason)
}

func (h *TerminalHandler) Emit(event terminal.Event) {
	payload := map[string]interface{}{
		"session_id": event.SessionID,
		"timestamp":  time.Now().UnixMilli(),
	}
	messageType := ""
	switch event.Type {
	case terminal.EventReady:
		messageType = "terminal_session_ready"
	case terminal.EventOutput:
		messageType = "terminal_session_output"
		payload["data"] = event.Data
		payload["stream"] = event.Stream
	case terminal.EventError:
		messageType = "terminal_session_error"
		payload["message"] = event.Message
	case terminal.EventClosed:
		messageType = "terminal_session_closed"
		payload["reason"] = event.Reason
	default:
		return
	}

	h.mu.RLock()
	ws := h.ws
	h.mu.RUnlock()
	if ws == nil || !ws.IsConnected() {
		if h.log != nil {
			h.log.Warnf("dropping terminal event %s for session %s: websocket unavailable", event.Type, event.SessionID)
		}
		return
	}
	if agentSessionID := ws.GetSessionID(); agentSessionID != "" {
		payload["agent_session_id"] = agentSessionID
	}
	if err := ws.SendMessage(messageType, payload); err != nil && h.log != nil {
		h.log.Warnf("failed to send terminal event %s for session %s: %v", event.Type, event.SessionID, err)
	}
}
