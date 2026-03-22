package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type terminalFrontendClient struct {
	conn              *websocket.Conn
	sessionID         string
	userID            uint64
	ownerConnectionID string
	mu                sync.Mutex
}

type terminalRelayEnvelope struct {
	Kind        string                 `json:"kind"`
	AgentID     uint64                 `json:"agent_id,omitempty"`
	MessageType string                 `json:"message_type"`
	Payload     map[string]interface{} `json:"payload"`
}

func (h *WebSocketHandler) startTerminalRelayConsumer() {
	if h == nil || utils.RedisClient == nil {
		return
	}
	h.terminalRelayOnce.Do(func() {
		go h.consumeTerminalRelay(context.Background())
	})
}

func (h *WebSocketHandler) consumeTerminalRelay(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if utils.RedisClient == nil {
			return
		}
		pubsub := utils.RedisClient.Subscribe(ctx, utils.TerminalRelayTopic(h.serverID))
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
				envelope := terminalRelayEnvelope{}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					continue
				}
				h.handleTerminalRelayEnvelope(envelope)
			}
		}
		_ = pubsub.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *WebSocketHandler) handleTerminalRelayEnvelope(envelope terminalRelayEnvelope) {
	if h == nil || envelope.MessageType == "" || len(envelope.Payload) == 0 {
		return
	}
	switch envelope.Kind {
	case "agent_message":
		if envelope.AgentID == 0 {
			return
		}
		_ = h.sendMessageToAgent(envelope.AgentID, envelope.MessageType, envelope.Payload)
	case "frontend_message":
		sessionID, _ := envelope.Payload["session_id"].(string)
		if sessionID == "" {
			return
		}
		h.broadcastTerminalToFrontend(sessionID, envelope.MessageType, envelope.Payload)
	}
}

func (h *WebSocketHandler) publishTerminalRelay(targetServerID string, envelope terminalRelayEnvelope) bool {
	if h == nil || utils.RedisClient == nil || strings.TrimSpace(targetServerID) == "" {
		return false
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return false
	}
	return utils.RedisClient.Publish(context.Background(), utils.TerminalRelayTopic(strings.TrimSpace(targetServerID)), data).Err() == nil
}

func (h *WebSocketHandler) HandleTerminalFrontendConnection(c *gin.Context) {
	h.startTerminalRelayConsumer()
	sessionID := strings.TrimSpace(c.Query("session_id"))
	token := strings.TrimSpace(c.Query("token"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "session_id is required"})
		return
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing token"})
		return
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid token"})
		return
	}
	if err := middleware.ValidateTokenSession(c.Request.Context(), claims); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "session expired"})
		return
	}
	if claims.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid user"})
		return
	}

	session := models.ResourceTerminalSession{}
	if err := models.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "terminal session not found"})
		return
	}
	if session.Status != models.ResourceTerminalSessionStatusActive {
		c.JSON(http.StatusGone, gin.H{"code": http.StatusGone, "message": "terminal session closed"})
		return
	}
	if !userCanWriteWorkspaceResource(models.DB, session.WorkspaceID, claims.UserID, claims.Role) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "forbidden"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	clientID := h.nextClientID("terminal")
	client := &terminalFrontendClient{conn: conn, sessionID: session.SessionID, userID: claims.UserID, ownerConnectionID: clientID}
	h.terminalFrontendsMu.Lock()
	if h.terminalFrontends[session.SessionID] == nil {
		h.terminalFrontends[session.SessionID] = make(map[string]*terminalFrontendClient)
	}
	h.terminalFrontends[session.SessionID][clientID] = client
	h.terminalFrontendsMu.Unlock()

	claimed, err := h.claimTerminalSessionOwner(session.SessionID, clientID, claims.UserID)
	if err != nil {
		h.removeTerminalFrontend(session.SessionID, clientID)
		client.mu.Lock()
		_ = client.conn.Close()
		client.mu.Unlock()
		return
	}
	_ = h.openTerminalSession(claimed)
	h.handleTerminalFrontendMessages(client, claimed, clientID)
}

func (h *WebSocketHandler) handleTerminalFrontendMessages(client *terminalFrontendClient, session *models.ResourceTerminalSession, clientID string) {
	defer func() {
		h.removeTerminalFrontend(client.sessionID, clientID)
		_ = h.releaseTerminalSessionOwner(client.sessionID, clientID)
		client.mu.Lock()
		_ = client.conn.Close()
		client.mu.Unlock()
	}()

	for {
		client.mu.Lock()
		_ = client.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		client.mu.Unlock()
		_, raw, err := client.conn.ReadMessage()
		if err != nil {
			return
		}
		msg := WebSocketMessage{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Payload == nil {
			msg.Payload = map[string]interface{}{}
		}
		msg.Payload["session_id"] = client.sessionID
		switch msg.Type {
		case "terminal_input":
			_ = h.routeTerminalMessageToAgent(session, "terminal_session_input", msg.Payload)
		case "terminal_resize":
			_ = h.routeTerminalMessageToAgent(session, "terminal_session_resize", msg.Payload)
		case "terminal_root_switch":
			_ = h.routeTerminalMessageToAgent(session, "terminal_session_root_switch", msg.Payload)
		case "terminal_ping":
			continue
		case "terminal_close":
			_, _ = h.CloseTerminalSession(client.sessionID, defaultIfEmpty(strings.TrimSpace(getString(msg.Payload, "reason")), "frontend_closed"), client.userID)
			return
		}
	}
}

func (h *WebSocketHandler) openTerminalSession(session *models.ResourceTerminalSession) bool {
	if h == nil || session == nil {
		return false
	}
	credential, payload, err := h.loadTerminalCredentialPayload(session)
	if err != nil {
		return false
	}
	openPayload := map[string]interface{}{
		"session_id":    session.SessionID,
		"workspace_id":  session.WorkspaceID,
		"resource_id":   session.ResourceID,
		"resource_type": session.ResourceType,
		"credential_id": credential.ID,
		"credential": map[string]interface{}{
			"id":       credential.ID,
			"type":     credential.Type,
			"category": credential.Category,
			"payload":  payload,
		},
		"endpoint":        session.Endpoint,
		"owner_server_id": session.OwnerServerID,
		"created_by":      session.CreatedBy,
	}
	return h.routeTerminalMessageToAgent(session, "terminal_session_open", openPayload)
}

func (h *WebSocketHandler) loadTerminalCredentialPayload(session *models.ResourceTerminalSession) (*models.Credential, map[string]interface{}, error) {
	if session == nil || session.CredentialID == 0 {
		return nil, nil, fmt.Errorf("invalid terminal session credential")
	}
	credential := models.Credential{}
	if err := models.DB.First(&credential, session.CredentialID).Error; err != nil {
		return nil, nil, err
	}
	payload, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return nil, nil, err
	}
	return &credential, payload, nil
}

func (h *WebSocketHandler) routeTerminalMessageToAgent(session *models.ResourceTerminalSession, msgType string, payload map[string]interface{}) bool {
	if h == nil || session == nil || session.AgentID == 0 || msgType == "" || len(payload) == 0 {
		return false
	}
	presence, err := utils.GetAgentPresence(context.Background(), session.AgentID)
	if err == nil && presence != nil && strings.TrimSpace(presence.ServerID) != "" && presence.ServerID != h.serverID {
		return h.publishTerminalRelay(presence.ServerID, terminalRelayEnvelope{
			Kind:        "agent_message",
			AgentID:     session.AgentID,
			MessageType: msgType,
			Payload:     payload,
		})
	}
	return h.sendMessageToAgent(session.AgentID, msgType, payload)
}

func (h *WebSocketHandler) handleTerminalAgentMessage(client *wsClient, msgType string, payload map[string]interface{}) {
	if h == nil || client == nil || payload == nil {
		return
	}
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		return
	}
	presence, err := utils.GetAgentPresence(context.Background(), client.agentID)
	if err == nil && presence != nil {
		if presence.AgentSessionID != client.sessionID || presence.ServerID != client.serverID {
			return
		}
	}
	session := models.ResourceTerminalSession{}
	if err := models.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return
	}
	if session.AgentID != client.agentID {
		return
	}
	frontendType := ""
	switch msgType {
	case "terminal_session_ready":
		frontendType = "terminal_ready"
	case "terminal_session_output":
		frontendType = "terminal_output"
	case "terminal_session_error":
		frontendType = "terminal_error"
	case "terminal_session_closed":
		frontendType = "terminal_closed"
		reason := defaultIfEmpty(strings.TrimSpace(getString(payload, "reason")), "agent_closed")
		closed, closeErr := closeTerminalSessionRecord(models.DB, &session, reason, 0)
		if closeErr == nil {
			session = *closed
		}
		payload["reason"] = reason
	}
	if frontendType == "" {
		return
	}
	payload["session_id"] = session.SessionID
	_ = h.routeTerminalMessageToFrontend(&session, frontendType, payload)
}

func (h *WebSocketHandler) routeTerminalMessageToFrontend(session *models.ResourceTerminalSession, msgType string, payload map[string]interface{}) bool {
	if h == nil || session == nil || msgType == "" || len(payload) == 0 {
		return false
	}
	ownerServerID := strings.TrimSpace(session.OwnerServerID)
	if ownerServerID == "" || ownerServerID == h.serverID {
		return h.broadcastTerminalToFrontend(session.SessionID, msgType, payload)
	}
	return h.publishTerminalRelay(ownerServerID, terminalRelayEnvelope{
		Kind:        "frontend_message",
		MessageType: msgType,
		Payload:     payload,
	})
}

func (h *WebSocketHandler) broadcastTerminalToFrontend(sessionID, msgType string, payload map[string]interface{}) bool {
	if h == nil || sessionID == "" || msgType == "" || len(payload) == 0 {
		return false
	}
	h.terminalFrontendsMu.RLock()
	clients, exists := h.terminalFrontends[sessionID]
	h.terminalFrontendsMu.RUnlock()
	if !exists || len(clients) == 0 {
		return false
	}
	data, err := json.Marshal(WebSocketMessage{Type: msgType, Payload: payload})
	if err != nil {
		return false
	}
	sent := false
	h.terminalFrontendsMu.RLock()
	defer h.terminalFrontendsMu.RUnlock()
	for _, client := range clients {
		client.mu.Lock()
		writeErr := client.conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
		if writeErr == nil {
			sent = true
		}
	}
	return sent
}

func (h *WebSocketHandler) claimTerminalSessionOwner(sessionID, connectionID string, userID uint64) (*models.ResourceTerminalSession, error) {
	if h == nil || sessionID == "" || connectionID == "" {
		return nil, fmt.Errorf("invalid terminal owner claim")
	}
	now := time.Now().Unix()
	result := models.DB.Model(&models.ResourceTerminalSession{}).Where("session_id = ? AND status = ?", sessionID, models.ResourceTerminalSessionStatusActive).Updates(map[string]interface{}{
		"owner_server_id":     h.serverID,
		"owner_connection_id": connectionID,
		"attached_by":         optionalUint64(userID),
		"attached_at":         now,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("terminal session closed")
	}
	session := models.ResourceTerminalSession{}
	if err := models.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (h *WebSocketHandler) releaseTerminalSessionOwner(sessionID, connectionID string) error {
	if h == nil || models.DB == nil || sessionID == "" || connectionID == "" {
		return nil
	}
	return models.DB.Model(&models.ResourceTerminalSession{}).
		Where("session_id = ? AND owner_server_id = ? AND owner_connection_id = ? AND status = ?", sessionID, h.serverID, connectionID, models.ResourceTerminalSessionStatusActive).
		Updates(map[string]interface{}{"owner_server_id": "", "owner_connection_id": ""}).Error
}

func (h *WebSocketHandler) removeTerminalFrontend(sessionID, clientID string) {
	if h == nil || sessionID == "" || clientID == "" {
		return
	}
	h.terminalFrontendsMu.Lock()
	defer h.terminalFrontendsMu.Unlock()
	clients := h.terminalFrontends[sessionID]
	if clients == nil {
		return
	}
	delete(clients, clientID)
	if len(clients) == 0 {
		delete(h.terminalFrontends, sessionID)
	}
}

func (h *WebSocketHandler) nextClientID(prefix string) string {
	h.clientIDMu.Lock()
	defer h.clientIDMu.Unlock()
	h.clientIDCounter++
	return fmt.Sprintf("%s_%d", defaultIfEmpty(strings.TrimSpace(prefix), "client"), h.clientIDCounter)
}

func (h *WebSocketHandler) CloseTerminalSession(sessionID, reason string, closedBy uint64) (*models.ResourceTerminalSession, error) {
	if h == nil || models.DB == nil {
		return nil, fmt.Errorf("terminal runtime unavailable")
	}
	session := models.ResourceTerminalSession{}
	if err := models.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("终端会话不存在")
		}
		return nil, fmt.Errorf("加载终端会话失败")
	}
	closed, err := closeTerminalSessionRecord(models.DB, &session, reason, closedBy)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{"session_id": closed.SessionID, "reason": strings.TrimSpace(closed.CloseReason)}
	_ = h.routeTerminalMessageToAgent(closed, "terminal_session_close", payload)
	_ = h.routeTerminalMessageToFrontend(closed, "terminal_closed", payload)
	return closed, nil
}
