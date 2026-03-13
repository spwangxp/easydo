package agent

import (
	"fmt"
	"testing"
)

func TestReportTaskUpdateV2_QueuesTerminalUpdateWhenWebsocketUnavailable(t *testing.T) {
	h := &TaskHandler{}
	task := &Task{ID: 11}

	err := h.reportTaskUpdateV2(task, 1, "execute_success", 0, "", 1000, map[string]interface{}{"stdout_size": 1})
	if err == nil {
		t.Fatal("expected websocket unavailable error")
	}
	if len(h.pendingWS) != 1 {
		t.Fatalf("pending queue len=%d, want=1", len(h.pendingWS))
	}
	if h.pendingWS[0].messageType != "task_update_v2" {
		t.Fatalf("queued message type=%s, want task_update_v2", h.pendingWS[0].messageType)
	}
}

func TestFlushPendingWebSocketMessagesWithSender_FlushesInOrderAndStopsOnFailure(t *testing.T) {
	h := &TaskHandler{}
	h.enqueuePendingWebSocketMessage("task_log_chunk_v2", map[string]interface{}{"seq": int64(1)})
	h.enqueuePendingWebSocketMessage("task_update_v2", map[string]interface{}{"status": "execute_success"})

	var sent []string
	h.flushPendingWebSocketMessagesWithSender(func(messageType string, payload map[string]interface{}) error {
		sent = append(sent, fmt.Sprintf("%s:%v", messageType, payload))
		return nil
	})

	if len(sent) != 2 {
		t.Fatalf("sent=%d, want=2 (%v)", len(sent), sent)
	}
	if len(h.pendingWS) != 0 {
		t.Fatalf("pending queue len=%d, want=0", len(h.pendingWS))
	}

	h.enqueuePendingWebSocketMessage("task_log_chunk_v2", map[string]interface{}{"seq": int64(2)})
	h.enqueuePendingWebSocketMessage("task_log_end_v2", map[string]interface{}{"final_seq": int64(2)})

	sent = sent[:0]
	h.flushPendingWebSocketMessagesWithSender(func(messageType string, payload map[string]interface{}) error {
		sent = append(sent, messageType)
		if len(sent) == 1 {
			return fmt.Errorf("temporary send failure")
		}
		return nil
	})

	if len(sent) != 1 {
		t.Fatalf("sent after failure=%d, want=1", len(sent))
	}
	if len(h.pendingWS) != 2 {
		t.Fatalf("pending queue len after failure=%d, want=2", len(h.pendingWS))
	}
}
