package terminal

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	EventReady  = "ready"
	EventOutput = "output"
	EventError  = "error"
	EventClosed = "closed"

	defaultCols = 80
	defaultRows = 24
)

type Event struct {
	Type      string
	SessionID string
	Data      string
	Stream    string
	Message   string
	Reason    string
}

type EventSink interface {
	Emit(Event)
}

type Credential struct {
	ID       uint64
	Type     string
	Category string
	Payload  map[string]interface{}
}

type OpenRequest struct {
	SessionID  string
	Endpoint   string
	Credential Credential
	Cols       int
	Rows       int
}

type Dialer interface {
	Open(context.Context, OpenRequest) (Session, error)
}

type Session interface {
	Stdout() io.Reader
	Stderr() io.Reader
	Write([]byte) (int, error)
	Resize(cols, rows int) error
	Close() error
	Wait() error
}

type Runtime struct {
	dialer   Dialer
	sink     EventSink
	mu       sync.RWMutex
	sessions map[string]*managedSession
}

type managedSession struct {
	id         string
	session    Session
	runtime    *Runtime
	credential Credential

	mu            sync.Mutex
	closing       bool
	closeReason   string
	explicitClose bool
	closeOnce     sync.Once
	rootSwitch    *pendingRootSwitch
}

type pendingRootSwitch struct {
	promptToken  string
	password     string
	awaitingEcho bool
	echoBuffer   string
}

func NewRuntime(dialer Dialer, sink EventSink) *Runtime {
	if dialer == nil {
		dialer = NewSSHDialer()
	}
	return &Runtime{
		dialer:   dialer,
		sink:     sink,
		sessions: make(map[string]*managedSession),
	}
}

func (r *Runtime) Open(ctx context.Context, req OpenRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if req.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if req.Cols <= 0 {
		req.Cols = defaultCols
	}
	if req.Rows <= 0 {
		req.Rows = defaultRows
	}

	r.mu.Lock()
	if existing, exists := r.sessions[req.SessionID]; exists {
		existing.mu.Lock()
		existing.credential = req.Credential
		existing.mu.Unlock()
		r.mu.Unlock()
		r.emit(Event{Type: EventReady, SessionID: req.SessionID})
		return nil
	}
	r.mu.Unlock()

	session, err := r.dialer.Open(ctx, req)
	if err != nil {
		r.emit(Event{Type: EventError, SessionID: req.SessionID, Message: err.Error()})
		r.emit(Event{Type: EventClosed, SessionID: req.SessionID, Reason: "open_failed"})
		return err
	}

	managed := &managedSession{id: req.SessionID, session: session, runtime: r, credential: req.Credential}
	r.mu.Lock()
	r.sessions[req.SessionID] = managed
	r.mu.Unlock()

	go managed.readStream(session.Stdout(), "stdout")
	go managed.readStream(session.Stderr(), "stderr")
	go managed.wait()

	r.emit(Event{Type: EventReady, SessionID: req.SessionID})
	return nil
}

func (r *Runtime) Input(sessionID, data string) error {
	managed, err := r.getSession(sessionID)
	if err != nil {
		return err
	}
	_, err = managed.session.Write([]byte(data))
	return err
}

func (r *Runtime) Resize(sessionID string, cols, rows int) error {
	managed, err := r.getSession(sessionID)
	if err != nil {
		return err
	}
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	return managed.session.Resize(cols, rows)
}

func (r *Runtime) Close(sessionID, reason string) error {
	managed, err := r.getSession(sessionID)
	if err != nil {
		return err
	}
	managed.close(defaultReason(reason, "agent_closed"), true)
	return nil
}

func (r *Runtime) RootSwitch(sessionID string) error {
	managed, err := r.getSession(sessionID)
	if err != nil {
		return err
	}

	username := credentialStringValue(managed.credential.Payload["username"])
	password := credentialStringValue(managed.credential.Payload["password"])
	if strings.EqualFold(username, "root") {
		return nil
	}

	command := "sudo -n -u root -i\n"
	if strings.EqualFold(strings.TrimSpace(managed.credential.Type), "PASSWORD") && password != "" {
		promptToken := fmt.Sprintf("[EASYDO_ROOT_PROMPT:%s]", managed.id)
		managed.mu.Lock()
		managed.rootSwitch = &pendingRootSwitch{promptToken: promptToken, password: password}
		managed.mu.Unlock()
		command = fmt.Sprintf("sudo -S -p '%s' -u root -i\n", promptToken)
	}

	_, err = managed.session.Write([]byte(command))
	return err
}

func (r *Runtime) CloseAll(reason string) {
	r.mu.RLock()
	sessions := make([]*managedSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.RUnlock()
	for _, session := range sessions {
		session.close(defaultReason(reason, "agent_closed"), true)
	}
}

func (r *Runtime) getSession(sessionID string) (*managedSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	managed := r.sessions[sessionID]
	if managed == nil {
		return nil, fmt.Errorf("terminal session not found: %s", sessionID)
	}
	return managed, nil
}

func (r *Runtime) deleteSession(sessionID string) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

func (r *Runtime) emit(event Event) {
	if r.sink != nil {
		r.sink.Emit(event)
	}
}

func (r *Runtime) sessionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func (s *managedSession) readStream(reader io.Reader, stream string) {
	if reader == nil {
		return
	}
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			data := s.handleOutputChunk(string(buf[:n]))
			if data != "" {
				s.runtime.emit(Event{Type: EventOutput, SessionID: s.id, Stream: stream, Data: data})
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if !closing {
				s.runtime.emit(Event{Type: EventError, SessionID: s.id, Message: err.Error()})
			}
			return
		}
	}
}

func (s *managedSession) handleOutputChunk(data string) string {
	s.mu.Lock()
	pending := s.rootSwitch
	if pending == nil {
		s.mu.Unlock()
		return data
	}
	if pending.awaitingEcho {
		masked, done, hold := pending.consumePasswordEcho(data)
		if done {
			s.rootSwitch = nil
		}
		s.mu.Unlock()
		if hold {
			return ""
		}
		return masked
	}
	if !strings.Contains(data, pending.promptToken) {
		s.mu.Unlock()
		return data
	}
	password := pending.password
	promptToken := pending.promptToken
	pending.awaitingEcho = true
	s.mu.Unlock()

	_, _ = s.session.Write([]byte(password + "\n"))
	return strings.ReplaceAll(data, promptToken, "")
}

func (p *pendingRootSwitch) consumePasswordEcho(data string) (masked string, done bool, hold bool) {
	p.echoBuffer += data
	if strings.Contains(p.echoBuffer, p.password) {
		masked = strings.Replace(p.echoBuffer, p.password, "", 1)
		p.echoBuffer = ""
		return masked, true, false
	}

	trimmed := strings.TrimLeft(p.echoBuffer, "\r\n")
	if trimmed == "" || (len(trimmed) < len(p.password) && strings.HasPrefix(p.password, trimmed)) {
		return "", false, true
	}

	masked = p.echoBuffer
	p.echoBuffer = ""
	return masked, true, false
}

func (s *managedSession) wait() {
	err := s.session.Wait()
	s.mu.Lock()
	explicitClose := s.explicitClose
	s.mu.Unlock()
	if err != nil && !explicitClose {
		s.runtime.emit(Event{Type: EventError, SessionID: s.id, Message: err.Error()})
		s.close("session_error", false)
		return
	}
	if !explicitClose {
		s.close("session_closed", false)
	}
}

func (s *managedSession) close(reason string, explicit bool) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.closeReason = reason
		s.explicitClose = explicit
		s.mu.Unlock()

		s.runtime.deleteSession(s.id)
		_ = s.session.Close()
		s.runtime.emit(Event{Type: EventClosed, SessionID: s.id, Reason: reason})
	})
}

func defaultReason(reason, fallback string) string {
	if reason != "" {
		return reason
	}
	return fallback
}

func credentialStringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}
