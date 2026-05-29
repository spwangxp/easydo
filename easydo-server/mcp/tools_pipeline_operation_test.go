package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
)

type fakePipelineOperationUseCase struct {
	triggerCalls int
	retryCalls   int
	cancelCalls  int

	triggerReq services.TriggerPipelineRequest
	retryReq   services.RetryPipelineTaskRequest
	cancelReq  services.CancelPipelineRunRequest

	triggerResult services.TriggerPipelineResult
	retryResult   services.RetryPipelineTaskResult
	cancelResult  services.CancelPipelineRunResult

	triggerErr error
	retryErr   error
	cancelErr  error
}

func (f *fakePipelineOperationUseCase) TriggerPipeline(ctx context.Context, req services.TriggerPipelineRequest) (services.TriggerPipelineResult, error) {
	f.triggerCalls++
	f.triggerReq = req
	return f.triggerResult, f.triggerErr
}

func (f *fakePipelineOperationUseCase) RetryPipelineTask(ctx context.Context, req services.RetryPipelineTaskRequest) (services.RetryPipelineTaskResult, error) {
	f.retryCalls++
	f.retryReq = req
	return f.retryResult, f.retryErr
}

func (f *fakePipelineOperationUseCase) CancelPipelineRun(ctx context.Context, req services.CancelPipelineRunRequest) (services.CancelPipelineRunResult, error) {
	f.cancelCalls++
	f.cancelReq = req
	return f.cancelResult, f.cancelErr
}

func registerPipelineOperationToolsForTest(t *testing.T, fake *fakePipelineOperationUseCase) *Registry {
	t.Helper()
	registry := NewRegistry()
	if err := RegisterPipelineOperationTools(registry, fake); err != nil {
		t.Fatalf("RegisterPipelineOperationTools returned error: %v", err)
	}
	return registry
}

func assertServiceError(t *testing.T, err error, code services.ErrorCode, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected service error %q", code)
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != code {
		t.Fatalf("error code=%q, want %q", svcErr.Code, code)
	}
	if message != "" && svcErr.Message != message {
		t.Fatalf("error message=%q, want %q", svcErr.Message, message)
	}
}

func TestPipelineTriggerToolRequiresWorkspaceAndPipelineID(t *testing.T) {
	fake := &fakePipelineOperationUseCase{}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_trigger", Invocation{Actor: actor, Arguments: map[string]any{"pipeline_id": 22}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, "workspace_id is required")
	if fake.triggerCalls != 0 {
		t.Fatalf("trigger usecase calls=%d, want 0", fake.triggerCalls)
	}

	_, err = registry.Invoke(context.Background(), "easydo_pipeline_trigger", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, "pipeline_id is required")
	if fake.triggerCalls != 0 {
		t.Fatalf("trigger usecase calls=%d, want 0", fake.triggerCalls)
	}
}

func TestPipelineTriggerToolRejectsOversizedInputsBeforeUseCase(t *testing.T) {
	fake := &fakePipelineOperationUseCase{}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}
	oversized := map[string]any{"node-1": map[string]any{"value": strings.Repeat("x", MaxBodySizeBytes+1)}}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_trigger", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "inputs": oversized}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, "")
	if fake.triggerCalls != 0 {
		t.Fatalf("trigger usecase calls=%d, want 0", fake.triggerCalls)
	}
}

func TestPipelineTriggerToolRejectsMalformedInputsAndOptionsBeforeUseCase(t *testing.T) {
	cases := []struct {
		name      string
		arguments map[string]any
	}{
		{
			name:      "inputs is not object",
			arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "inputs": []any{"not-object"}},
		},
		{
			name:      "inputs value is not object",
			arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "inputs": map[string]any{"node-1": "not-object"}},
		},
		{
			name:      "options is not object",
			arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "options": []any{"not-object"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePipelineOperationUseCase{}
			registry := registerPipelineOperationToolsForTest(t, fake)
			actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}

			_, err := registry.Invoke(context.Background(), "easydo_pipeline_trigger", Invocation{Actor: actor, Arguments: tc.arguments})
			assertServiceError(t, err, services.ErrorCodeInvalidArgument, "")
			if fake.triggerCalls != 0 {
				t.Fatalf("trigger usecase calls=%d, want 0", fake.triggerCalls)
			}
		})
	}
}

func TestPipelineTriggerToolDelegatesPermissionToUseCase(t *testing.T) {
	permissionErr := services.ServiceError{Code: services.ErrorCodeForbidden, Message: "workspace role is insufficient for pipeline operation"}
	fake := &fakePipelineOperationUseCase{triggerErr: permissionErr}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}
	inputs := map[string]any{"build": map[string]any{"branch": "main"}}
	options := map[string]any{"reason": "manual"}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_trigger", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "inputs": inputs, "options": options}, Protocol: streamableHTTPProtocol})
	assertServiceError(t, err, services.ErrorCodeForbidden, permissionErr.Message)
	if fake.triggerCalls != 1 {
		t.Fatalf("trigger usecase calls=%d, want 1", fake.triggerCalls)
	}
	if fake.triggerReq.Actor != actor {
		t.Fatalf("actor=%+v, want %+v", fake.triggerReq.Actor, actor)
	}
	if fake.triggerReq.WorkspaceID != 11 || fake.triggerReq.PipelineID != 22 {
		t.Fatalf("request ids=%d/%d, want 11/22", fake.triggerReq.WorkspaceID, fake.triggerReq.PipelineID)
	}
	if fake.triggerReq.Inputs["build"]["branch"] != "main" {
		t.Fatalf("inputs=%+v, want build branch main", fake.triggerReq.Inputs)
	}
	if fake.triggerReq.Options["reason"] != "manual" {
		t.Fatalf("options=%+v, want reason manual", fake.triggerReq.Options)
	}
	if fake.triggerReq.TriggerType != "mcp" {
		t.Fatalf("trigger type=%q, want mcp", fake.triggerReq.TriggerType)
	}
	if fake.triggerReq.TriggerSource != streamableHTTPProtocol {
		t.Fatalf("trigger source=%q, want %q", fake.triggerReq.TriggerSource, streamableHTTPProtocol)
	}
}

func TestPipelineTaskRetryToolRejectsOversizedArgumentsBeforeUseCase(t *testing.T) {
	fake := &fakePipelineOperationUseCase{}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_task_retry", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11, "task_id": 33, "extra": strings.Repeat("x", MaxBodySizeBytes+1)}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, "")
	if fake.retryCalls != 0 {
		t.Fatalf("retry usecase calls=%d, want 0", fake.retryCalls)
	}
}

func TestPipelineRunCancelToolRejectsOversizedArgumentsBeforeUseCase(t *testing.T) {
	fake := &fakePipelineOperationUseCase{}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_run_cancel", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11, "pipeline_id": 22, "run_id": 44, "extra": strings.Repeat("x", MaxBodySizeBytes+1)}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, "")
	if fake.cancelCalls != 0 {
		t.Fatalf("cancel usecase calls=%d, want 0", fake.cancelCalls)
	}
}

func TestPipelineTaskRetryToolDelegatesStateErrorsToUseCase(t *testing.T) {
	stateErr := services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "只能重试失败的任务"}
	fake := &fakePipelineOperationUseCase{retryErr: stateErr}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}

	_, err := registry.Invoke(context.Background(), "easydo_pipeline_task_retry", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": 11, "task_id": 33}})
	assertServiceError(t, err, services.ErrorCodeInvalidArgument, stateErr.Message)
	if fake.retryCalls != 1 {
		t.Fatalf("retry usecase calls=%d, want 1", fake.retryCalls)
	}
	if fake.retryReq.Actor != actor || fake.retryReq.WorkspaceID != 11 || fake.retryReq.TaskID != 33 {
		t.Fatalf("retry request=%+v, want actor/workspace/task", fake.retryReq)
	}
}

func TestPipelineRunCancelToolAuditsFailedWrite(t *testing.T) {
	db := openAuditTestDB(t)
	recorder := NewGormAuditRecorder(db)
	forbiddenErr := services.ServiceError{Code: services.ErrorCodeForbidden, Message: "token secret denied"}
	fake := &fakePipelineOperationUseCase{cancelErr: forbiddenErr}
	registry := registerPipelineOperationToolsForTest(t, fake)
	actor := services.ActorContext{UserID: 1001, Username: "pipeline-op-user", SystemRole: "user"}
	arguments := map[string]any{"workspace_id": 11, "pipeline_id": 22, "run_id": 44, "note": "Bearer token secret text"}
	tool := registry.tools["easydo_pipeline_run_cancel"]

	_, err := WrapAudit(context.Background(), recorder, AuditRecordInput{
		RequestID:     "pipeline-cancel-failure",
		UserID:        actor.UserID,
		WorkspaceID:   11,
		ToolName:      tool.Name,
		OperationType: tool.OperationType,
		TargetType:    tool.TargetType,
		TargetID:      "44",
		Protocol:      "test",
		InputSummary:  arguments,
	}, func(ctx context.Context) (AuditResult, error) {
		result, err := registry.Invoke(ctx, tool.Name, Invocation{Actor: actor, Arguments: arguments, RequestID: "pipeline-cancel-failure", Protocol: "test"})
		return AuditResult{OutputSummary: result.StructuredContent}, err
	})
	assertServiceError(t, err, services.ErrorCodeForbidden, forbiddenErr.Message)
	if fake.cancelCalls != 1 {
		t.Fatalf("cancel usecase calls=%d, want 1", fake.cancelCalls)
	}

	var audit models.MCPCallAudit
	if err := db.Where("request_id = ?", "pipeline-cancel-failure").First(&audit).Error; err != nil {
		t.Fatalf("load audit failed: %v", err)
	}
	if audit.Status != string(AuditStatusFailure) || audit.OperationType != string(OperationWrite) {
		t.Fatalf("audit status/op=%q/%q, want failure/write", audit.Status, audit.OperationType)
	}
	if audit.ToolName != "easydo_pipeline_run_cancel" || audit.TargetType != "pipeline_run" {
		t.Fatalf("audit tool/target=%q/%q", audit.ToolName, audit.TargetType)
	}
	if audit.ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit error code=%q, want %q", audit.ErrorCode, services.ErrorCodeForbidden)
	}
	combined := strings.ToLower(audit.InputSummary + " " + audit.OutputSummary + " " + audit.ErrorMessage)
	for _, forbidden := range []string{"bearer", "token", "secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("audit leaked %q: input=%q output=%q error=%q", forbidden, audit.InputSummary, audit.OutputSummary, audit.ErrorMessage)
		}
	}
}
