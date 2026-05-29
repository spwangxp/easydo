package mcp

import (
	"context"

	"easydo-server/internal/services"
)

func RegisterResourceTools(registry *Registry, usecase *services.ResourceUseCase) error {
	if registry == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "registry is required"}
	}
	if usecase == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "resource usecase is required"}
	}
	for _, tool := range []Tool{
		{
			Name:          "easydo_resource_list",
			Description:   "List resources in one workspace",
			OperationType: OperationRead,
			TargetType:    "resource",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"page":         map[string]any{"type": "integer", "minimum": 1},
					"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
					"query":        map[string]any{"type": "string"},
					"type":         map[string]any{"type": "string"},
					"status":       map[string]any{"type": "string"},
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
				result, err := usecase.ListResources(ctx, services.ListResourcesRequest{Actor: actor, WorkspaceID: workspaceID, Page: page, Limit: limit, Query: optionalStringArgument(invocation.Arguments, "query"), Type: optionalStringArgument(invocation.Arguments, "type"), Status: optionalStringArgument(invocation.Arguments, "status")})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_resource_get",
			Description:   "Get one resource in one workspace",
			OperationType: OperationRead,
			TargetType:    "resource",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "resource_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"resource_id":  map[string]any{"type": "integer", "minimum": 1},
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
				resourceID, err := requiredUint64Argument(invocation.Arguments, "resource_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetResource(ctx, services.GetResourceRequest{Actor: actor, WorkspaceID: workspaceID, ResourceID: resourceID})
				if err != nil {
					return ToolResult{}, err
				}
				return ToolResult{StructuredContent: result}, nil
			},
		},
		{
			Name:          "easydo_resource_status",
			Description:   "Get resource health and base information summary in one workspace",
			OperationType: OperationRead,
			TargetType:    "resource",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "resource_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
					"resource_id":  map[string]any{"type": "integer", "minimum": 1},
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
				resourceID, err := requiredUint64Argument(invocation.Arguments, "resource_id")
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetResourceStatus(ctx, services.GetResourceStatusRequest{Actor: actor, WorkspaceID: workspaceID, ResourceID: resourceID})
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
