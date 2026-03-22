package terminal

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRuntime_OpenInputResizeCloseLifecycle(t *testing.T) {
	sink := newRecordingSink()
	session := newFakeSession()
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		if req.SessionID != "session-1" {
			t.Fatalf("unexpected session id: %s", req.SessionID)
		}
		if req.Endpoint != "10.0.0.41:22" {
			t.Fatalf("unexpected endpoint: %s", req.Endpoint)
		}
		return session, nil
	}), sink)

	if err := runtime.Open(context.Background(), OpenRequest{
		SessionID: "session-1",
		Endpoint:  "10.0.0.41:22",
		Credential: Credential{
			Type:    "PASSWORD",
			Payload: map[string]interface{}{"username": "root", "password": "secret123"},
		},
		Cols: 120,
		Rows: 40,
	}); err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if event := sink.waitForType(t, EventReady); event.SessionID != "session-1" {
		t.Fatalf("ready session id=%s, want session-1", event.SessionID)
	}

	if err := runtime.Input("session-1", "ls -la\n"); err != nil {
		t.Fatalf("input failed: %v", err)
	}
	if got := session.lastWrite(); got != "ls -la\n" {
		t.Fatalf("written input=%q, want %q", got, "ls -la\n")
	}

	if err := runtime.Resize("session-1", 140, 50); err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	if got := session.lastResize(); got != [2]int{140, 50} {
		t.Fatalf("resize=%v, want [140 50]", got)
	}

	session.writeStdout("total 0\n")
	output := sink.waitForType(t, EventOutput)
	if output.SessionID != "session-1" || output.Data != "total 0\n" || output.Stream != "stdout" {
		t.Fatalf("unexpected output event: %+v", output)
	}

	if err := runtime.Close("session-1", "frontend_closed"); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	closed := sink.waitForType(t, EventClosed)
	if closed.SessionID != "session-1" || closed.Reason != "frontend_closed" {
		t.Fatalf("unexpected closed event: %+v", closed)
	}
	if runtime.sessionCount() != 0 {
		t.Fatalf("active sessions=%d, want 0", runtime.sessionCount())
	}
}

func TestRuntime_ConcurrentSessionsStayIsolated(t *testing.T) {
	sink := newRecordingSink()
	sessionA := newFakeSession()
	sessionB := newFakeSession()
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		switch req.SessionID {
		case "session-a":
			return sessionA, nil
		case "session-b":
			return sessionB, nil
		default:
			t.Fatalf("unexpected session id: %s", req.SessionID)
			return nil, nil
		}
	}), sink)

	for _, sessionID := range []string{"session-a", "session-b"} {
		if err := runtime.Open(context.Background(), OpenRequest{SessionID: sessionID, Endpoint: "10.0.0.51:22"}); err != nil {
			t.Fatalf("open %s failed: %v", sessionID, err)
		}
	}
	sink.waitForType(t, EventReady)
	sink.waitForType(t, EventReady)

	if err := runtime.Input("session-a", "pwd\n"); err != nil {
		t.Fatalf("input session-a failed: %v", err)
	}
	if err := runtime.Input("session-b", "hostname\n"); err != nil {
		t.Fatalf("input session-b failed: %v", err)
	}
	if got := sessionA.lastWrite(); got != "pwd\n" {
		t.Fatalf("session-a input=%q, want pwd\\n", got)
	}
	if got := sessionB.lastWrite(); got != "hostname\n" {
		t.Fatalf("session-b input=%q, want hostname\\n", got)
	}

	sessionA.writeStdout("/workspace/a\n")
	sessionB.writeStdout("vm-b\n")

	first := sink.waitForType(t, EventOutput)
	second := sink.waitForType(t, EventOutput)
	outputs := map[string]string{first.SessionID: first.Data, second.SessionID: second.Data}
	if outputs["session-a"] != "/workspace/a\n" {
		t.Fatalf("session-a output=%q, want /workspace/a\\n", outputs["session-a"])
	}
	if outputs["session-b"] != "vm-b\n" {
		t.Fatalf("session-b output=%q, want vm-b\\n", outputs["session-b"])
	}
}

func TestRuntime_CleansUpSessionOnWaitError(t *testing.T) {
	sink := newRecordingSink()
	session := newFakeSession()
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		return session, nil
	}), sink)

	if err := runtime.Open(context.Background(), OpenRequest{SessionID: "session-error", Endpoint: "10.0.0.61:22"}); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	sink.waitForType(t, EventReady)

	session.finish(errors.New("remote shell exited"))
	errEvent := sink.waitForType(t, EventError)
	if errEvent.SessionID != "session-error" || errEvent.Message == "" {
		t.Fatalf("unexpected error event: %+v", errEvent)
	}
	closed := sink.waitForType(t, EventClosed)
	if closed.SessionID != "session-error" || closed.Reason != "session_error" {
		t.Fatalf("unexpected closed event after error: %+v", closed)
	}
	if runtime.sessionCount() != 0 {
		t.Fatalf("active sessions=%d, want 0", runtime.sessionCount())
	}
}

func TestRuntime_OpenExistingSessionReusesRunningSession(t *testing.T) {
	sink := newRecordingSink()
	session := newFakeSession()
	dialCount := 0
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		dialCount++
		return session, nil
	}), sink)

	req := OpenRequest{
		SessionID: "session-reuse",
		Endpoint:  "10.0.0.81:22",
		Credential: Credential{
			Type:    "PASSWORD",
			Payload: map[string]interface{}{"username": "demo", "password": "secret123"},
		},
	}

	if err := runtime.Open(context.Background(), req); err != nil {
		t.Fatalf("first open failed: %v", err)
	}
	sink.waitForType(t, EventReady)

	if err := runtime.Open(context.Background(), req); err != nil {
		t.Fatalf("second open should reuse active session: %v", err)
	}
	ready := sink.waitForType(t, EventReady)
	if ready.SessionID != "session-reuse" {
		t.Fatalf("ready session id=%s, want session-reuse", ready.SessionID)
	}
	if runtime.sessionCount() != 1 {
		t.Fatalf("active sessions=%d, want 1", runtime.sessionCount())
	}
	if dialCount != 1 {
		t.Fatalf("dial count=%d, want 1", dialCount)
	}
}

func TestRuntime_RootSwitchUsesStoredPasswordCredential(t *testing.T) {
	sink := newRecordingSink()
	session := newFakeSession()
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		return session, nil
	}), sink)

	if err := runtime.Open(context.Background(), OpenRequest{
		SessionID: "session-root-switch",
		Endpoint:  "10.0.0.91:22",
		Credential: Credential{
			Type:    "PASSWORD",
			Payload: map[string]interface{}{"username": "demo", "password": "secret123"},
		},
	}); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	sink.waitForType(t, EventReady)

	if err := runtime.RootSwitch("session-root-switch"); err != nil {
		t.Fatalf("root switch failed: %v", err)
	}
	firstWrite := session.lastWrite()
	if firstWrite == "" || !containsAll(firstWrite, []string{"sudo -S -p '[EASYDO_ROOT_PROMPT:session-root-switch]'", "-u root -i"}) {
		t.Fatalf("unexpected root switch command: %q", firstWrite)
	}

	session.writeStdout("[EASYDO_ROOT_PROMPT:session-root-switch]")
	waitForWrites(t, session, 2)
	if got := session.lastWrite(); got != "secret123\n" {
		t.Fatalf("password write=%q, want secret123\\n", got)
	}
}

func TestRuntime_RootSwitchDoesNotEmitPasswordEcho(t *testing.T) {
	sink := newRecordingSink()
	session := newFakeSession()
	runtime := NewRuntime(fakeDialerFunc(func(ctx context.Context, req OpenRequest) (Session, error) {
		return session, nil
	}), sink)

	if err := runtime.Open(context.Background(), OpenRequest{
		SessionID: "session-root-mask",
		Endpoint:  "10.0.0.92:22",
		Credential: Credential{
			Type:    "PASSWORD",
			Payload: map[string]interface{}{"username": "demo", "password": "secret123"},
		},
	}); err != nil {
		t.Fatalf("open failed: %v", err)
	}
	sink.waitForType(t, EventReady)

	if err := runtime.RootSwitch("session-root-mask"); err != nil {
		t.Fatalf("root switch failed: %v", err)
	}

	session.writeStdout("[EASYDO_ROOT_PROMPT:session-root-mask]")
	waitForWrites(t, session, 2)
	session.writeStdout("secret123\r\nroot@vm:~# ")

	output := sink.waitForType(t, EventOutput)
	if strings.Contains(output.Data, "secret123") {
		t.Fatalf("output leaked password: %q", output.Data)
	}
	if !strings.Contains(output.Data, "root@vm:~#") {
		t.Fatalf("output=%q, want root prompt", output.Data)
	}
}

type fakeDialerFunc func(context.Context, OpenRequest) (Session, error)

func (f fakeDialerFunc) Open(ctx context.Context, req OpenRequest) (Session, error) {
	return f(ctx, req)
}

type recordingSink struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newRecordingSink() *recordingSink {
	return &recordingSink{ch: make(chan Event, 32)}
}

func (s *recordingSink) Emit(event Event) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	s.ch <- event
}

func (s *recordingSink) waitForType(t *testing.T, want string) Event {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-s.ch:
			if event.Type == want {
				return event
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for event type %s", want)
		}
	}
}

type fakeSession struct {
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	stderrReader *io.PipeReader
	stderrWriter *io.PipeWriter

	mu       sync.Mutex
	writes   []string
	resizes  [][2]int
	closeCnt int
	waitCh   chan error
	closed   bool
}

func newFakeSession() *fakeSession {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	return &fakeSession{
		stdoutReader: stdoutReader,
		stdoutWriter: stdoutWriter,
		stderrReader: stderrReader,
		stderrWriter: stderrWriter,
		waitCh:       make(chan error, 1),
	}
}

func (s *fakeSession) Stdout() io.Reader { return s.stdoutReader }

func (s *fakeSession) Stderr() io.Reader { return s.stderrReader }

func (s *fakeSession) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, string(data))
	return len(data), nil
}

func (s *fakeSession) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizes = append(s.resizes, [2]int{cols, rows})
	return nil
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	s.closeCnt++
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.stdoutWriter.Close()
	_ = s.stderrWriter.Close()
	s.finish(nil)
	return nil
}

func (s *fakeSession) Wait() error {
	return <-s.waitCh
}

func (s *fakeSession) writeStdout(data string) {
	_, _ = io.WriteString(s.stdoutWriter, data)
}

func (s *fakeSession) finish(err error) {
	select {
	case s.waitCh <- err:
	default:
	}
	_ = s.stdoutWriter.Close()
	_ = s.stderrWriter.Close()
}

func (s *fakeSession) lastWrite() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writes) == 0 {
		return ""
	}
	return s.writes[len(s.writes)-1]
}

func (s *fakeSession) lastResize() [2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.resizes) == 0 {
		return [2]int{}
	}
	return s.resizes[len(s.resizes)-1]
}

func waitForWrites(t *testing.T, session *fakeSession, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		count := len(session.writes)
		session.mu.Unlock()
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d writes", want)
}

func containsAll(value string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
