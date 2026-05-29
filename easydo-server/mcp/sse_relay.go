package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"easydo-server/internal/services"
	"easydo-server/pkg/utils"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	mcpSSERelayTopicBase    = "easydo:mcp:sse:relay:"
	mcpSSERelayAckTopicBase = "easydo:mcp:sse:relay_ack:"
	mcpSSESessionKeyPrefix  = "easydo:mcp:sse:session:"
)

type sseSharedSession struct {
	SessionID     string `json:"session_id"`
	ServerID      string `json:"server_id"`
	UserID        uint64 `json:"user_id"`
	AuthSessionID string `json:"auth_session_id"`
}

type sseRelayEnvelope struct {
	SessionID  string                `json:"session_id"`
	RelayID    string                `json:"relay_id"`
	ReplyTopic string                `json:"reply_topic"`
	Actor      services.ActorContext `json:"actor"`
	Request    JSONRPCRequest        `json:"request"`
	ClientIP   string                `json:"client_ip"`
}

type sseRelayAck struct {
	RelayID   string             `json:"relay_id"`
	ErrorCode services.ErrorCode `json:"error_code,omitempty"`
	Message   string             `json:"message,omitempty"`
}

func newSSESessionID(serverID string) string {
	serverID = strings.TrimSpace(serverID)
	id := uuid.NewString()
	if serverID == "" {
		return id
	}
	return serverID + "." + id
}

func mcpSSERelayTopic(serverID string) string {
	return mcpSSERelayTopicBase + strings.TrimSpace(serverID)
}

func mcpSSERelayAckTopic(relayID string) string {
	return mcpSSERelayAckTopicBase + strings.TrimSpace(relayID)
}

func mcpSSESessionKey(sessionID string) string {
	return mcpSSESessionKeyPrefix + strings.TrimSpace(sessionID)
}

func (s *Server) startSSERelayConsumer() {
	if s == nil || utils.RedisClient == nil || strings.TrimSpace(s.serverID) == "" {
		return
	}
	s.sseRelayOnce.Do(func() {
		go s.consumeSSERelay(context.Background())
	})
}

func (s *Server) consumeSSERelay(ctx context.Context) {
	for {
		if ctx.Err() != nil || utils.RedisClient == nil {
			return
		}
		pubsub := utils.RedisClient.Subscribe(ctx, mcpSSERelayTopic(s.serverID))
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
				envelope := sseRelayEnvelope{}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					continue
				}
				s.handleSSERelayEnvelope(envelope)
			}
		}
		_ = pubsub.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Server) registerSharedSSESession(ctx context.Context, session *sseSession) error {
	if s == nil || session == nil || utils.RedisClient == nil || strings.TrimSpace(s.serverID) == "" {
		return nil
	}
	payload, err := json.Marshal(sseSharedSession{
		SessionID:     session.id,
		ServerID:      s.serverID,
		UserID:        session.actor.UserID,
		AuthSessionID: session.actor.SessionID,
	})
	if err != nil {
		return err
	}
	ttl := s.sseSessions.maxLifetime
	if ttl <= 0 {
		ttl = SSEMaxSessionLifetime
	}
	return utils.RedisClient.Set(ctx, mcpSSESessionKey(session.id), payload, ttl).Err()
}

func (s *Server) unregisterSharedSSESession(sessionID string) {
	if utils.RedisClient == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = utils.RedisClient.Del(ctx, mcpSSESessionKey(sessionID)).Err()
}

func (s *Server) relaySSEMessage(ctx context.Context, sessionID string, actor services.ActorContext, req JSONRPCRequest, clientIP string) (bool, error) {
	if s == nil || utils.RedisClient == nil || strings.TrimSpace(sessionID) == "" {
		return false, nil
	}
	shared, err := s.loadSharedSSESession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to load SSE session"}
	}
	if strings.TrimSpace(shared.ServerID) == "" || shared.ServerID == s.serverID {
		return false, nil
	}
	if shared.UserID != actor.UserID || shared.AuthSessionID != actor.SessionID {
		return false, services.ServiceError{Code: services.ErrorCodeForbidden, Message: "SSE session access denied"}
	}
	relayID := uuid.NewString()
	replyTopic := mcpSSERelayAckTopic(relayID)
	pubsub := utils.RedisClient.Subscribe(ctx, replyTopic)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return false, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to listen for SSE relay ack"}
	}
	defer pubsub.Close()

	data, err := json.Marshal(sseRelayEnvelope{SessionID: sessionID, RelayID: relayID, ReplyTopic: replyTopic, Actor: actor, Request: req, ClientIP: clientIP})
	if err != nil {
		return false, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to encode SSE relay"}
	}
	subscribers, err := utils.RedisClient.Publish(ctx, mcpSSERelayTopic(shared.ServerID), data).Result()
	if err != nil {
		return false, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to relay SSE message"}
	}
	if subscribers == 0 {
		return false, services.ServiceError{Code: services.ErrorCodeNotFound, Message: "SSE session not found"}
	}
	ack, err := waitSSERelayAck(ctx, pubsub.Channel(), relayID)
	if err != nil {
		return false, err
	}
	if ack.ErrorCode != "" {
		return false, services.ServiceError{Code: ack.ErrorCode, Message: ack.Message}
	}
	return true, nil
}

func waitSSERelayAck(ctx context.Context, channel <-chan *redis.Message, relayID string) (sseRelayAck, error) {
	for {
		select {
		case <-ctx.Done():
			return sseRelayAck{}, services.ServiceError{Code: services.ErrorCodeTimeout, Message: "SSE relay timed out"}
		case msg, ok := <-channel:
			if !ok {
				return sseRelayAck{}, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "SSE relay ack channel closed"}
			}
			ack := sseRelayAck{}
			if err := json.Unmarshal([]byte(msg.Payload), &ack); err != nil || ack.RelayID != relayID {
				continue
			}
			return ack, nil
		}
	}
}

func (s *Server) loadSharedSSESession(ctx context.Context, sessionID string) (sseSharedSession, error) {
	var shared sseSharedSession
	if utils.RedisClient == nil {
		return shared, redis.Nil
	}
	data, err := utils.RedisClient.Get(ctx, mcpSSESessionKey(sessionID)).Bytes()
	if err != nil {
		return shared, err
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		return shared, err
	}
	return shared, nil
}

func (s *Server) handleSSERelayEnvelope(envelope sseRelayEnvelope) {
	if s == nil || envelope.SessionID == "" || envelope.Actor.UserID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()
	auditInput := auditInputForRequest(envelope.Request)
	auditInput.Protocol = sseProtocol
	auditInput.UserID = envelope.Actor.UserID
	session, err := s.sseSessions.getForActor(envelope.SessionID, envelope.Actor)
	if err != nil {
		s.recordFailure(ctx, auditInput, err)
		s.publishSSERelayAck(envelope, err)
		return
	}
	s.publishSSERelayAck(envelope, s.deliverSSEMessage(ctx, session, envelope.Request, envelope.Actor, auditInput, envelope.ClientIP))
}

func (s *Server) publishSSERelayAck(envelope sseRelayEnvelope, err error) {
	if utils.RedisClient == nil || envelope.ReplyTopic == "" || envelope.RelayID == "" {
		return
	}
	ack := sseRelayAck{RelayID: envelope.RelayID}
	if err != nil {
		ack.ErrorCode, ack.Message = sseRelayError(err)
	}
	data, marshalErr := json.Marshal(ack)
	if marshalErr != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = utils.RedisClient.Publish(ctx, envelope.ReplyTopic, data).Err()
}

func sseRelayError(err error) (services.ErrorCode, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		svcErr := services.ServiceError{Code: services.ErrorCodeTimeout, Message: "request timed out"}
		return svcErr.Code, svcErr.Error()
	}
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.Code, svcErr.Error()
	}
	svcErr = services.ServiceError{Code: services.ErrorCodeInternalError, Message: "internal error"}
	return svcErr.Code, svcErr.Error()
}
