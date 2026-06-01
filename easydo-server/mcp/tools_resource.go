package mcp

import (
	"context"
	"fmt"
	"strings"

	"easydo-server/internal/services"
)

type ResourceBaseInfoRefreshService interface {
	RequestResourceBaseInfoRefresh(context.Context, services.RefreshResourceBaseInfoRequest) (services.RefreshResourceBaseInfoResult, error)
}

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
		{
			Name:          "easydo_resource_gpu_usage",
			Description:   "Get structured GPU usage for one resource in one workspace",
			OperationType: OperationRead,
			TargetType:    "resource",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id", "resource_id"},
				"properties": map[string]any{
					"workspace_id":        map[string]any{"type": "integer", "minimum": 1},
					"resource_id":         map[string]any{"type": "integer", "minimum": 1},
					"include_allocations": map[string]any{"type": "boolean", "default": true},
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
				includeAllocations, err := optionalBoolArgument(invocation.Arguments, "include_allocations", true)
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetResourceGPUUsage(ctx, services.GetResourceGPUUsageRequest{Actor: actor, WorkspaceID: workspaceID, ResourceID: resourceID, IncludeAllocations: includeAllocations})
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

func RegisterResourceOperationTools(registry *Registry, service ResourceBaseInfoRefreshService) error {
	if registry == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "registry is required"}
	}
	if service == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "resource refresh service is required"}
	}
	return registry.Register(Tool{
		Name:          "easydo_resource_base_info_refresh",
		Description:   "Trigger resource base information refresh collection in one workspace",
		OperationType: OperationWrite,
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
			result, err := service.RequestResourceBaseInfoRefresh(ctx, services.RefreshResourceBaseInfoRequest{Actor: actor, WorkspaceID: workspaceID, ResourceID: resourceID})
			if err != nil {
				return ToolResult{}, err
			}
			return ToolResult{StructuredContent: result}, nil
		},
	})
}

func optionalBoolArgument(args map[string]any, key string, defaultValue bool) (bool, error) {
	if args == nil {
		return defaultValue, nil
	}
	value, exists := args[key]
	if !exists || value == nil {
		return defaultValue, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		text := strings.TrimSpace(strings.ToLower(typed))
		if text == "true" || text == "1" || text == "yes" {
			return true, nil
		}
		if text == "false" || text == "0" || text == "no" {
			return false, nil
		}
	}
	return false, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: fmt.Sprintf("%s must be a boolean", key)}
}
