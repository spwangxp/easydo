package mcp

import (
	"context"
	"encoding/json"

	"easydo-server/internal/services"
)

type PipelineOperationService interface {
	TriggerPipeline(context.Context, services.TriggerPipelineRequest) (services.TriggerPipelineResult, error)
	RetryPipelineTask(context.Context, services.RetryPipelineTaskRequest) (services.RetryPipelineTaskResult, error)
	CancelPipelineRun(context.Context, services.CancelPipelineRunRequest) (services.CancelPipelineRunResult, error)
}

func RegisterPipelineTools(registry *Registry, usecase *services.PipelineQueryUseCase) error {
	if registry == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "registry is required"}
	}
	if usecase == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "pipeline query usecase is required"}
	}
	for _, tool := range []Tool{
		{
			Name:          "easydo_pipeline_list",
			Description:   "List pipelines in one workspace",
			OperationType: OperationRead,
			TargetType:    "pipeline",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"page":         map[string]any{"type": "integer", "minimum": 1},
					"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					"query":        map[string]any{"type": "string"},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				page, err := optionalIntArgument(invocation.Arguments, "page")
				if err != nil {
					return ToolResult{}, err
				}
				limit, err := optionalIntArgument(invocation.Arguments, "limit")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.ListPipelines(ctx, services.ListPipelinesRequest{Actor: actor, WorkspaceID: workspaceID, Page: page, Limit: limit, Query: optionalStringArgument(invocation.Arguments, "query")})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_get",
			Description:   "Get one pipeline in one workspace",
			OperationType: OperationRead,
			TargetType:    "pipeline",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetPipeline(ctx, services.GetPipelineRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_run_list",
			Description:   "List pipeline runs in one workspace",
			OperationType: OperationRead,
			TargetType:    "pipeline_run",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"page":         map[string]any{"type": "integer", "minimum": 1},
					"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				page, err := optionalIntArgument(invocation.Arguments, "page")
				if err != nil {
					return ToolResult{}, err
				}
				limit, err := optionalIntArgument(invocation.Arguments, "limit")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.ListPipelineRuns(ctx, services.ListPipelineRunsRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, Page: page, Limit: limit})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_run_get",
			Description:   "Get one pipeline run in one workspace",
			OperationType: OperationRead,
			TargetType:    "pipeline_run",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id", "run_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"run_id":       map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				runID, err := requiredUint64Argument(invocation.Arguments, "run_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetPipelineRun(ctx, services.GetPipelineRunRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, RunID: runID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_parameter_schema",
			Description:   "Get runtime parameter schema and nested inputs example for one pipeline",
			OperationType: OperationRead,
			TargetType:    "pipeline",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetPipelineParameterSchema(ctx, services.GetPipelineParameterSchemaRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_trigger_preview",
			Description:   "Preview and validate nested runtime inputs before triggering one pipeline",
			OperationType: OperationRead,
			TargetType:    "pipeline",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"inputs":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object"}},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				if err := rejectOversizedPipelineOperationArguments(invocation.Arguments); err != nil {
					return ToolResult{}, err
				}
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				inputs, err := pipelineTriggerInputsArgument(invocation.Arguments)
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.PreviewPipelineTriggerParameters(ctx, services.PreviewPipelineTriggerParametersRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, Inputs: inputs})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_run_parameters_get",
			Description:   "Get historical runtime parameter view for one pipeline run",
			OperationType: OperationRead,
			TargetType:    "pipeline_run",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id", "run_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"run_id":       map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				runID, err := requiredUint64Argument(invocation.Arguments, "run_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetPipelineRunParameters(ctx, services.GetPipelineRunParametersRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, RunID: runID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_task_get",
			Description:   "Get one pipeline task in one workspace",
			OperationType: OperationRead,
			TargetType:    "pipeline_task",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id", "run_id", "task_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"run_id":       map[string]any{"type": "integer", "minimum": 1},
					"task_id":      map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				runID, err := requiredUint64Argument(invocation.Arguments, "run_id")
				if err != nil {
					return ToolResult{}, err
				}
				taskID, err := requiredUint64Argument(invocation.Arguments, "task_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetPipelineTask(ctx, services.GetPipelineTaskRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, RunID: runID, TaskID: taskID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
	} {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func RegisterPipelineOperationTools(registry *Registry, usecase PipelineOperationService) error {
	if registry == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "registry is required"}
	}
	if usecase == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "pipeline operation usecase is required"}
	}
	for _, tool := range []Tool{
		{
			Name:          "easydo_pipeline_trigger",
			Description:   "Trigger a pipeline run in one workspace",
			OperationType: OperationWrite,
			TargetType:    "pipeline",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"inputs":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object"}},
					"options":      map[string]any{"type": "object"},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				if err := rejectOversizedPipelineOperationArguments(invocation.Arguments); err != nil {
					return ToolResult{}, err
				}
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				inputs, err := pipelineTriggerInputsArgument(invocation.Arguments)
				if err != nil {
					return ToolResult{}, err
				}
				options, err := pipelineTriggerOptionsArgument(invocation.Arguments)
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.TriggerPipeline(ctx, services.TriggerPipelineRequest{
					Actor:         actor,
					WorkspaceID:   workspaceID,
					PipelineID:    pipelineID,
					Inputs:        inputs,
					Options:       options,
					TriggerType:   "mcp",
					TriggerSource: invocation.Protocol,
				})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_task_retry",
			Description:   "Retry a failed pipeline task in one workspace",
			OperationType: OperationWrite,
			TargetType:    "pipeline_task",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "task_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"task_id":      map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				if err := rejectOversizedPipelineOperationArguments(invocation.Arguments); err != nil {
					return ToolResult{}, err
				}
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				taskID, err := requiredUint64Argument(invocation.Arguments, "task_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.RetryPipelineTask(ctx, services.RetryPipelineTaskRequest{Actor: actor, WorkspaceID: workspaceID, TaskID: taskID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_pipeline_run_cancel",
			Description:   "Cancel a pipeline run in one workspace",
			OperationType: OperationWrite,
			TargetType:    "pipeline_run",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "pipeline_id", "run_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"pipeline_id":  map[string]any{"type": "integer", "minimum": 1},
					"run_id":       map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				if err := rejectOversizedPipelineOperationArguments(invocation.Arguments); err != nil {
					return ToolResult{}, err
				}
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredUint64Argument(invocation.Arguments, "workspace_id")
				if err != nil {
					return ToolResult{}, err
				}
				pipelineID, err := requiredUint64Argument(invocation.Arguments, "pipeline_id")
				if err != nil {
					return ToolResult{}, err
				}
				runID, err := requiredUint64Argument(invocation.Arguments, "run_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.CancelPipelineRun(ctx, services.CancelPipelineRunRequest{Actor: actor, WorkspaceID: workspaceID, PipelineID: pipelineID, RunID: runID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
	} {
		if err := registry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func rejectOversizedPipelineOperationArguments(args map[string]any) error {
	payload, err := json.Marshal(args)
	if err != nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "arguments must be JSON serializable"}
	}
	if len(payload) > MaxBodySizeBytes {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "arguments exceed maximum size"}
	}
	return nil
}

func pipelineTriggerInputsArgument(args map[string]any) (map[string]map[string]any, error) {
	value, exists := args["inputs"]
	if !exists || value == nil {
		return nil, nil
	}
	outer, ok := value.(map[string]any)
	if !ok {
		return nil, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "inputs must be an object"}
	}
	inputs := make(map[string]map[string]any, len(outer))
	for key, item := range outer {
		inner, ok := item.(map[string]any)
		if !ok {
			return nil, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "inputs values must be objects"}
		}
		inputs[key] = inner
	}
	return inputs, nil
}

func pipelineTriggerOptionsArgument(args map[string]any) (map[string]any, error) {
	value, exists := args["options"]
	if !exists || value == nil {
		return nil, nil
	}
	options, ok := value.(map[string]any)
	if !ok {
		return nil, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "options must be an object"}
	}
	return options, nil
}
