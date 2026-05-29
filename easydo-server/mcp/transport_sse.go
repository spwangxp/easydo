package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

const (
	sseProtocol        = "sse"
	sseConnectToolName = "sse/connect"
)

type sseSession struct {
	id             string
	actor          services.ActorContext
	userKey        string
	ipKey          string
	createdAt      time.Time
	lastActivityAt time.Time
	done           chan struct{}
	closeOnce      sync.Once
	streamMu       sync.Mutex
	writer         io.Writer
	flusher        http.Flusher
}

func (s *sseSession) attachStream(writer io.Writer, flusher http.Flusher) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	s.writer = writer
	s.flusher = flusher
}

func (s *sseSession) sendEvent(event, data string) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.writer == nil || s.flusher == nil {
		return false
	}
	if err := writeSSEEvent(s.writer, event, data); err != nil {
		return false
	}
	s.flusher.Flush()
	return true
}

func (s *sseSession) close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

type sseSessionManagerOptions struct {
	now             func() time.Time
	idleTimeout     time.Duration
	maxLifetime     time.Duration
	cleanupInterval time.Duration
	userLimiter     *inFlightLimiter
	ipLimiter       *inFlightLimiter
}

type sseSessionManager struct {
	now             func() time.Time
	idleTimeout     time.Duration
	maxLifetime     time.Duration
	cleanupInterval time.Duration
	userLimiter     *inFlightLimiter
	ipLimiter       *inFlightLimiter

	mu            sync.Mutex
	sessions      map[string]*sseSession
	lastCleanupAt time.Time
}

func newSSESessionManager(opts sseSessionManagerOptions) *sseSessionManager {
	now := opts.now
	if now == nil {
		now = time.Now
	}
	return &sseSessionManager{
		now:             now,
		idleTimeout:     opts.idleTimeout,
		maxLifetime:     opts.maxLifetime,
		cleanupInterval: opts.cleanupInterval,
		userLimiter:     opts.userLimiter,
		ipLimiter:       opts.ipLimiter,
		sessions:        make(map[string]*sseSession),
	}
}

func (m *sseSessionManager) create(actor services.ActorContext, ipKey string, serverID string) (*sseSession, error) {
	if m == nil {
		return nil, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "sse session manager is required"}
	}
	now := m.now()
	userKey := strconv.FormatUint(actor.UserID, 10)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now, true)
	if !m.userLimiter.acquire(userKey) {
		return nil, services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many open SSE connections"}
	}
	if !m.ipLimiter.acquire(ipKey) {
		m.userLimiter.release(userKey)
		return nil, services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many open SSE connections"}
	}
	session := &sseSession{
		id:             newSSESessionID(serverID),
		actor:          actor,
		userKey:        userKey,
		ipKey:          ipKey,
		createdAt:      now,
		lastActivityAt: now,
		done:           make(chan struct{}),
	}
	m.sessions[session.id] = session
	return session, nil
}

func (m *sseSessionManager) getForActor(sessionID string, actor services.ActorContext) (*sseSession, error) {
	if m == nil {
		return nil, services.ServiceError{Code: services.ErrorCodeInternalError, Message: "sse session manager is required"}
	}
	if sessionID == "" {
		return nil, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "session_id is required"}
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now, false)
	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, services.ServiceError{Code: services.ErrorCodeNotFound, Message: "SSE session not found"}
	}
	if session.actor.UserID != actor.UserID || session.actor.SessionID != actor.SessionID {
		return nil, services.ServiceError{Code: services.ErrorCodeForbidden, Message: "SSE session access denied"}
	}
	session.lastActivityAt = now
	return session, nil
}

func (m *sseSessionManager) close(sessionID string) {
	if m == nil || sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked(sessionID)
}

func (m *sseSessionManager) cleanupEvery() time.Duration {
	if m == nil || m.cleanupInterval <= 0 {
		return SSESessionCleanupInterval
	}
	return m.cleanupInterval
}

func (m *sseSessionManager) expireSessionIfNeeded(sessionID string) bool {
	if m == nil || sessionID == "" {
		return true
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return true
	}
	if !m.isExpired(now, session) {
		return false
	}
	m.closeLocked(sessionID)
	m.lastCleanupAt = now
	return true
}

func (m *sseSessionManager) cleanupExpiredLocked(now time.Time, force bool) {
	if !force && m.cleanupInterval > 0 && !m.lastCleanupAt.IsZero() && now.Sub(m.lastCleanupAt) < m.cleanupInterval {
		return
	}
	for sessionID, session := range m.sessions {
		if m.isExpired(now, session) {
			m.closeLocked(sessionID)
		}
	}
	m.lastCleanupAt = now
}

func (m *sseSessionManager) isExpired(now time.Time, session *sseSession) bool {
	if session == nil {
		return true
	}
	if m.idleTimeout > 0 && now.Sub(session.lastActivityAt) > m.idleTimeout {
		return true
	}
	if m.maxLifetime > 0 && now.Sub(session.createdAt) > m.maxLifetime {
		return true
	}
	return false
}

func (m *sseSessionManager) closeLocked(sessionID string) {
	session, ok := m.sessions[sessionID]
	if !ok {
		return
	}
	delete(m.sessions, sessionID)
	m.userLimiter.release(session.userKey)
	m.ipLimiter.release(session.ipKey)
	session.close()
}

func (s *Server) handleSSEConnect(c *gin.Context) {
	auditInput := AuditRecordInput{
		ToolName:      sseConnectToolName,
		OperationType: OperationRead,
		Protocol:      sseProtocol,
		InputSummary:  map[string]any{"path": "/mcp/sse"},
	}
	if err := s.checkOrigin(c.GetHeader("Origin")); err != nil {
		s.recordFailure(c.Request.Context(), auditInput, err)
		c.Status(http.StatusForbidden)
		return
	}
	actor, authErr := AuthenticateRequest(c.Request.Context(), c.GetHeader("Authorization"))
	if authErr != nil {
		s.recordFailure(c.Request.Context(), auditInput, authErr)
		c.Status(http.StatusUnauthorized)
		return
	}
	auditInput.UserID = actor.UserID
	session, err := s.sseSessions.create(actor, c.ClientIP(), s.serverID)
	if err != nil {
		s.recordOutcome(c.Request.Context(), auditInput, classifyAuditStatus(c.Request.Context(), err), err, s.now())
		c.Status(httpStatusForError(err))
		return
	}
	if err := s.registerSharedSSESession(c.Request.Context(), session); err != nil {
		s.sseSessions.close(session.id)
		err = services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to register SSE session"}
		s.recordFailure(c.Request.Context(), auditInput, err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer func() {
		s.sseSessions.close(session.id)
		s.unregisterSharedSSESession(session.id)
	}()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusInternalServerError)
		return
	}
	session.attachStream(c.Writer, flusher)
	if !session.sendEvent("endpoint", "/mcp/sse/"+session.id+"/message") {
		return
	}
	ticker := time.NewTicker(s.sseSessions.cleanupEvery())
	defer ticker.Stop()

	for {
		select {
		case <-session.done:
			return
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if s.sseSessions.expireSessionIfNeeded(session.id) {
				return
			}
		}
	}
}

func (s *Server) handleSSEMessage(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), s.requestTimeout)
	defer cancel()

	if err := s.checkOrigin(c.GetHeader("Origin")); err != nil {
		s.recordFailure(ctx, AuditRecordInput{
			OperationType: OperationRead,
			Protocol:      sseProtocol,
			InputSummary:  map[string]any{"path": c.Request.URL.Path},
		}, err)
		c.Status(httpStatusForError(err))
		return
	}

	req, parseErr := s.parseJSONRPCRequest(c.Writer, c.Request)
	auditInput := auditInputForRequest(req)
	auditInput.Protocol = sseProtocol
	if parseErr != nil {
		s.recordFailure(ctx, auditInput, parseErr)
		s.writeJSONRPCParseError(c, req.ID, parseErr)
		return
	}
	actor, authErr := AuthenticateRequest(ctx, c.GetHeader("Authorization"))
	if authErr != nil {
		s.recordFailure(ctx, auditInput, authErr)
		s.writeJSONRPCError(c, req.ID, authErr)
		return
	}
	auditInput.UserID = actor.UserID
	sessionID := c.Param("session_id")
	session, err := s.sseSessions.getForActor(sessionID, actor)
	if err != nil {
		var svcErr services.ServiceError
		if errors.As(err, &svcErr) && svcErr.Code == services.ErrorCodeNotFound {
			relayed, relayErr := s.relaySSEMessage(ctx, sessionID, actor, req, c.ClientIP())
			if relayErr != nil {
				s.recordFailure(ctx, auditInput, relayErr)
				s.writeJSONRPCError(c, req.ID, relayErr)
				return
			}
			if relayed {
				c.Status(http.StatusAccepted)
				return
			}
		}
		s.recordFailure(ctx, auditInput, err)
		s.writeJSONRPCError(c, req.ID, err)
		return
	}
	if err := s.deliverSSEMessage(ctx, session, req, actor, auditInput, c.ClientIP()); err != nil {
		s.writeJSONRPCError(c, req.ID, err)
		return
	}
	c.Status(http.StatusAccepted)
}

func (s *Server) deliverSSEMessage(ctx context.Context, session *sseSession, req JSONRPCRequest, actor services.ActorContext, auditInput AuditRecordInput, clientIP string) error {
	if session == nil {
		err := services.ServiceError{Code: services.ErrorCodeNotFound, Message: "SSE session not found"}
		s.recordFailure(ctx, auditInput, err)
		return err
	}
	userKey := strconv.FormatUint(actor.UserID, 10)
	if !s.userLimiter.acquire(userKey) {
		err := services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many in flight requests"}
		s.recordOutcome(ctx, auditInput, AuditStatusConcurrencyLimited, err, s.now())
		return err
	}
	defer s.userLimiter.release(userKey)
	if !s.ipLimiter.acquire(clientIP) {
		err := services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many in flight requests"}
		s.recordOutcome(ctx, auditInput, AuditStatusConcurrencyLimited, err, s.now())
		return err
	}
	defer s.ipLimiter.release(clientIP)

	resp, respond := s.handleJSONRPC(ctx, req, actor, auditInput)
	if !respond {
		return nil
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		err = services.ServiceError{Code: services.ErrorCodeInternalError, Message: "failed to encode response"}
		s.recordFailure(ctx, auditInput, err)
		return err
	}
	if !session.sendEvent("message", string(payload)) {
		s.sseSessions.close(session.id)
		err = services.ServiceError{Code: services.ErrorCodeNotFound, Message: "SSE session not found"}
		s.recordFailure(ctx, auditInput, err)
		return err
	}
	return nil
}

func writeSSEEvent(w io.Writer, event, data string) error {
	if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
		return err
	}
	return nil
}

func httpStatusForError(err error) int {
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case services.ErrorCodeUnauthorized:
			return http.StatusUnauthorized
		case services.ErrorCodeForbidden:
			return http.StatusForbidden
		case services.ErrorCodeNotFound:
			return http.StatusNotFound
		case services.ErrorCodeRateLimited, services.ErrorCodeConcurrencyLimited:
			return http.StatusTooManyRequests
		case services.ErrorCodeTimeout:
			return http.StatusGatewayTimeout
		default:
			return http.StatusBadRequest
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError
}
