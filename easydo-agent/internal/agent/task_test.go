package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	agenttask "easydo-agent/internal/task"
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

func TestTaskParseParams_PreservesEnvVarsWhenJSONContainsNonStringValues(t *testing.T) {
	task := &Task{
		ID:      12,
		EnvVars: `{"EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN":"gho_test","CI":true,"DEPTH":3}`,
	}

	params, err := task.ParseParams()
	if err != nil {
		t.Fatalf("parse params failed: %v", err)
	}
	if params.EnvVars["EASYDO_CRED_REPO_AUTH_ACCESS_TOKEN"] != "gho_test" {
		t.Fatalf("expected credential env to be preserved, got %#v", params.EnvVars)
	}
	if params.EnvVars["CI"] != "true" {
		t.Fatalf("expected bool env to stringify, got %#v", params.EnvVars["CI"])
	}
	if params.EnvVars["DEPTH"] != "3" {
		t.Fatalf("expected numeric env to stringify, got %#v", params.EnvVars["DEPTH"])
	}
	if len(params.EnvVars) != 3 {
		t.Fatalf("expected all env vars to survive parsing, got %#v", params.EnvVars)
	}
}

func TestBuildTaskResultPayload_IncludesStdoutForResourceBaseInfoTasks(t *testing.T) {
	h := &TaskHandler{}
	task := &Task{
		ID:     21,
		Params: `{"collection":{"kind":"resource_base_info_refresh","resource_type":"vm"}}`,
	}
	result := &agenttask.Result{
		ExitCode: 0,
		Stdout:   "EASYDO_BASE_INFO_BEGIN\nEASYDO_CPU_LOGICAL_CORES=8\nEASYDO_BASE_INFO_END\n",
		Stderr:   "",
		Duration: 3 * time.Second,
	}

	payload, status, errorMsg := h.buildTaskResultPayload(task, result)
	if status != "execute_success" {
		t.Fatalf("status=%s, want execute_success", status)
	}
	if errorMsg != "" {
		t.Fatalf("errorMsg=%s, want empty", errorMsg)
	}
	if payload["stdout"] != result.Stdout {
		t.Fatalf("stdout payload mismatch, got=%v want=%q", payload["stdout"], result.Stdout)
	}
	if payload["stderr"] != result.Stderr {
		t.Fatalf("stderr payload mismatch, got=%v want=%q", payload["stderr"], result.Stderr)
	}
}

func TestBuildTaskResultPayload_FailsResourceBaseInfoTaskWhenStdoutMissingMarkers(t *testing.T) {
	h := &TaskHandler{}
	task := &Task{
		ID:     22,
		Params: `{"collection":{"kind":"resource_base_info_refresh","resource_type":"vm"}}`,
	}
	result := &agenttask.Result{
		ExitCode: 0,
		Stdout:   "plain stdout without expected markers",
		Duration: time.Second,
	}

	payload, status, errorMsg := h.buildTaskResultPayload(task, result)
	if status != "execute_failed" {
		t.Fatalf("status=%s, want execute_failed", status)
	}
	if !strings.Contains(errorMsg, "基础资源采集结果格式无效") {
		t.Fatalf("errorMsg=%q, want invalid base info format", errorMsg)
	}
	if payload["stdout"] != result.Stdout {
		t.Fatalf("stdout payload mismatch, got=%v want=%q", payload["stdout"], result.Stdout)
	}
	if payload["stderr"] != result.Stderr {
		t.Fatalf("stderr payload mismatch, got=%v want=%q", payload["stderr"], result.Stderr)
	}
}

func TestBuildTaskResultPayload_AcceptsK8sBaseInfoMarkers(t *testing.T) {
	h := &TaskHandler{}
	task := &Task{
		ID:     23,
		Params: `{"collection":{"kind":"resource_base_info_refresh","resource_type":"k8s"}}`,
	}
	result := &agenttask.Result{
		ExitCode: 0,
		Stdout:   "[easydo][step] 执行 Kubernetes 任务\nEASYDO_K8S_VERSION_BEGIN\n{}\nEASYDO_K8S_VERSION_END\nEASYDO_K8S_NODES_BEGIN\n{\"items\":[]}\nEASYDO_K8S_NODES_END\n",
		Duration: time.Second,
	}

	payload, status, errorMsg := h.buildTaskResultPayload(task, result)
	if status != "execute_success" {
		t.Fatalf("status=%s, want execute_success", status)
	}
	if errorMsg != "" {
		t.Fatalf("errorMsg=%q, want empty", errorMsg)
	}
	if payload["stdout"] != result.Stdout {
		t.Fatalf("stdout payload mismatch, got=%v want=%q", payload["stdout"], result.Stdout)
	}
}

func TestBuildTaskResultPayload_IncludesStdoutForResourceK8sQueryTasks(t *testing.T) {
	h := &TaskHandler{}
	task := &Task{
		ID:     24,
		Params: `{"k8s":{"kind":"resource_k8s_namespace_query","resource_id":1},"task_type":"kubernetes"}`,
	}
	result := &agenttask.Result{
		ExitCode: 0,
		Stdout:   `{"items":[{"metadata":{"name":"default"}}]}`,
		Stderr:   "",
		Duration: time.Second,
	}

	payload, status, errorMsg := h.buildTaskResultPayload(task, result)
	if status != "execute_success" {
		t.Fatalf("status=%s, want execute_success", status)
	}
	if errorMsg != "" {
		t.Fatalf("errorMsg=%q, want empty", errorMsg)
	}
	if payload["stdout"] != result.Stdout {
		t.Fatalf("stdout payload mismatch, got=%v want=%q", payload["stdout"], result.Stdout)
	}
	if payload["stderr"] != result.Stderr {
		t.Fatalf("stderr payload mismatch, got=%v want=%q", payload["stderr"], result.Stderr)
	}
}
