package client

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebSocketClient_HandleMessageRoutesTerminalSessionMessages(t *testing.T) {
	handler := &recordingTerminalHandler{
		openCh:       make(chan *TerminalSessionOpenMessage, 1),
		inputCh:      make(chan terminalInputCall, 1),
		resizeCh:     make(chan terminalResizeCall, 1),
		closeCh:      make(chan terminalCloseCall, 1),
		rootSwitchCh: make(chan string, 1),
	}
	client := &WebSocketClient{}
	client.SetTerminalHandler(handler)

	client.handleMessage(mustTerminalMessage(t, "terminal_session_open", map[string]interface{}{
		"session_id": "session-1",
		"endpoint":   "10.0.0.41:22",
		"credential": map[string]interface{}{
			"type":    "PASSWORD",
			"payload": map[string]interface{}{"username": "root", "password": "secret123"},
		},
	}))
	open := waitForTerminalValue(t, handler.openCh)
	if open.SessionID != "session-1" || open.Endpoint != "10.0.0.41:22" {
		t.Fatalf("unexpected open message: %+v", open)
	}

	client.handleMessage(mustTerminalMessage(t, "terminal_session_input", map[string]interface{}{
		"session_id": "session-1",
		"data":       "pwd\n",
	}))
	input := waitForTerminalValue(t, handler.inputCh)
	if input.sessionID != "session-1" || input.data != "pwd\n" {
		t.Fatalf("unexpected input message: %+v", input)
	}

	client.handleMessage(mustTerminalMessage(t, "terminal_session_resize", map[string]interface{}{
		"session_id": "session-1",
		"cols":       140,
		"rows":       50,
	}))
	resize := waitForTerminalValue(t, handler.resizeCh)
	if resize.sessionID != "session-1" || resize.cols != 140 || resize.rows != 50 {
		t.Fatalf("unexpected resize message: %+v", resize)
	}

	client.handleMessage(mustTerminalMessage(t, "terminal_session_close", map[string]interface{}{
		"session_id": "session-1",
		"reason":     "frontend_closed",
	}))
	closeCall := waitForTerminalValue(t, handler.closeCh)
	if closeCall.sessionID != "session-1" || closeCall.reason != "frontend_closed" {
		t.Fatalf("unexpected close message: %+v", closeCall)
	}

	client.handleMessage(mustTerminalMessage(t, "terminal_session_root_switch", map[string]interface{}{
		"session_id": "session-1",
	}))
	rootSwitchSessionID := waitForTerminalValue(t, handler.rootSwitchCh)
	if rootSwitchSessionID != "session-1" {
		t.Fatalf("unexpected root switch session id: %s", rootSwitchSessionID)
	}
}

func TestWebSocketClient_TerminalMessagesPreservePerSessionOrder(t *testing.T) {
	handler := &blockingTerminalHandler{
		releaseOpenCh: make(chan struct{}),
		openStartedCh: make(chan struct{}, 1),
		inputCh:       make(chan terminalInputCall, 1),
		resizeCh:      make(chan terminalResizeCall, 1),
	}
	client := &WebSocketClient{}
	client.SetTerminalHandler(handler)

	doneCh := make(chan struct{})
	go func() {
		client.handleMessage(mustTerminalMessage(t, "terminal_session_open", map[string]interface{}{
			"session_id": "session-ordered",
			"endpoint":   "10.0.0.41:22",
		}))
		client.handleMessage(mustTerminalMessage(t, "terminal_session_input", map[string]interface{}{
			"session_id": "session-ordered",
			"data":       "pwd\n",
		}))
		client.handleMessage(mustTerminalMessage(t, "terminal_session_resize", map[string]interface{}{
			"session_id": "session-ordered",
			"cols":       140,
			"rows":       50,
		}))
		close(doneCh)
	}()
	waitForTerminalSignal(t, handler.openStartedCh)

	assertTerminalNoValue(t, handler.inputCh)
	assertTerminalNoValue(t, handler.resizeCh)

	close(handler.releaseOpenCh)

	input := waitForTerminalValue(t, handler.inputCh)
	if input.sessionID != "session-ordered" || input.data != "pwd\n" {
		t.Fatalf("unexpected input message after open release: %+v", input)
	}

	resize := waitForTerminalValue(t, handler.resizeCh)
	if resize.sessionID != "session-ordered" || resize.cols != 140 || resize.rows != 50 {
		t.Fatalf("unexpected resize message after open release: %+v", resize)
	}
	waitForTerminalSignal(t, doneCh)

	if got := handler.openCalls.Load(); got != 1 {
		t.Fatalf("open calls=%d, want 1", got)
	}
}

func mustTerminalMessage(t *testing.T, msgType string, payload map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(WebSocketMessage{Type: msgType, Payload: payload})
	if err != nil {
		t.Fatalf("marshal message failed: %v", err)
	}
	return data
}

func waitForTerminalValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		var zero T
		t.Fatalf("timed out waiting for terminal handler callback")
		return zero
	}
}

func waitForTerminalSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for terminal signal")
	}
}

func assertTerminalNoValue[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case value := <-ch:
		t.Fatalf("expected no terminal handler callback yet, got %+v", value)
	case <-time.After(150 * time.Millisecond):
	}
}

type recordingTerminalHandler struct {
	openCh       chan *TerminalSessionOpenMessage
	inputCh      chan terminalInputCall
	resizeCh     chan terminalResizeCall
	closeCh      chan terminalCloseCall
	rootSwitchCh chan string
}

type blockingTerminalHandler struct {
	releaseOpenCh chan struct{}
	openStartedCh chan struct{}
	inputCh       chan terminalInputCall
	resizeCh      chan terminalResizeCall
	openCalls     atomic.Int32
}

func (h *blockingTerminalHandler) HandleTerminalSessionOpen(msg *TerminalSessionOpenMessage) error {
	h.openCalls.Add(1)
	select {
	case h.openStartedCh <- struct{}{}:
	default:
	}
	<-h.releaseOpenCh
	return nil
}

func (h *blockingTerminalHandler) HandleTerminalSessionInput(sessionID, data string) error {
	h.inputCh <- terminalInputCall{sessionID: sessionID, data: data}
	return nil
}

func (h *blockingTerminalHandler) HandleTerminalSessionResize(sessionID string, cols, rows int) error {
	h.resizeCh <- terminalResizeCall{sessionID: sessionID, cols: cols, rows: rows}
	return nil
}

func (h *blockingTerminalHandler) HandleTerminalSessionClose(sessionID, reason string) error {
	return nil
}

func (h *blockingTerminalHandler) HandleTerminalSessionRootSwitch(sessionID string) error {
	return nil
}

func (h *recordingTerminalHandler) HandleTerminalSessionOpen(msg *TerminalSessionOpenMessage) error {
	h.openCh <- msg
	return nil
}

func (h *recordingTerminalHandler) HandleTerminalSessionInput(sessionID, data string) error {
	h.inputCh <- terminalInputCall{sessionID: sessionID, data: data}
	return nil
}

func (h *recordingTerminalHandler) HandleTerminalSessionResize(sessionID string, cols, rows int) error {
	h.resizeCh <- terminalResizeCall{sessionID: sessionID, cols: cols, rows: rows}
	return nil
}

func (h *recordingTerminalHandler) HandleTerminalSessionClose(sessionID, reason string) error {
	h.closeCh <- terminalCloseCall{sessionID: sessionID, reason: reason}
	return nil
}

func (h *recordingTerminalHandler) HandleTerminalSessionRootSwitch(sessionID string) error {
	h.rootSwitchCh <- sessionID
	return nil
}

type terminalInputCall struct {
	sessionID string
	data      string
}

type terminalResizeCall struct {
	sessionID string
	cols      int
	rows      int
}

type terminalCloseCall struct {
	sessionID string
	reason    string
}
