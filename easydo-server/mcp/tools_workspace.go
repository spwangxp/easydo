package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"easydo-server/internal/services"
)

func RegisterWorkspaceTools(registry *Registry, usecase *services.WorkspaceUseCase) error {
	if registry == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "registry is required"}
	}
	if usecase == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "workspace usecase is required"}
	}
	for _, tool := range []Tool{
		{
			Name:          "easydo_workspace_list",
			Description:   "List workspaces the actor can access",
			OperationType: OperationRead,
			TargetType:    "workspace",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page":  map[string]any{"type": "integer", "minimum": 1},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": MaxPageLimit},
					"query": map[string]any{"type": "string"},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
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
				result, err := usecase.ListWorkspaces(ctx, services.ListWorkspacesRequest{Actor: actor, Page: page, Limit: limit, Query: optionalStringArgument(invocation.Arguments, "query"), Paginate: true})
				if err != nil {
					return ToolResult{}, err
				}
				structured := map[string]any{
					"list":                 result.List,
					"current_workspace_id": result.CurrentWorkspaceID,
				}
				return ToolResult{StructuredContent: structured}, nil
			},
		},
		{
			Name:          "easydo_workspace_get",
			Description:   "Get one workspace the actor can access",
			OperationType: OperationRead,
			TargetType:    "workspace",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"workspace_id"},
				"properties": map[string]any{
					"workspace_id": map[string]any{"type": "integer", "minimum": 1},
				},
			},
			Handler: func(ctx context.Context, invocation Invocation) (ToolResult, error) {
				actor, err := workspaceToolActor(invocation.Actor)
				if err != nil {
					return ToolResult{}, err
				}
				workspaceID, err := requiredWorkspaceArgument(actor, invocation.Arguments)
				if err != nil {
					return ToolResult{}, err
				}
				result, err := usecase.GetWorkspace(ctx, services.GetWorkspaceRequest{Actor: actor, WorkspaceID: workspaceID})
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

func workspaceToolActor(value any) (services.ActorContext, error) {
	actor, ok := value.(services.ActorContext)
	if !ok {
		if actorPtr, ok := value.(*services.ActorContext); ok && actorPtr != nil {
			actor = *actorPtr
		} else {
			return services.ActorContext{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
		}
	}
	if actor.UserID == 0 {
		return services.ActorContext{}, services.ServiceError{Code: services.ErrorCodeUnauthorized, Message: "unauthorized"}
	}
	return actor, nil
}

func optionalStringArgument(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, exists := args[key]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func optionalIntArgument(args map[string]any, key string) (int, error) {
	if args == nil {
		return 0, nil
	}
	value, exists := args[key]
	if !exists || value == nil {
		return 0, nil
	}
	parsed, err := parseUint64ArgumentValue(value, key)
	if err != nil {
		return 0, err
	}
	return int(parsed), nil
}

func requiredUint64Argument(args map[string]any, key string) (uint64, error) {
	if args == nil {
		return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " is required"}
	}
	value, exists := args[key]
	if !exists || value == nil {
		return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " is required"}
	}
	return parseUint64ArgumentValue(value, key)
}

func requiredWorkspaceArgument(actor services.ActorContext, args map[string]any) (uint64, error) {
	workspaceID, err := requiredUint64Argument(args, "workspace_id")
	if err != nil {
		return 0, err
	}
	if err := enforceMCPTokenWorkspace(actor, workspaceID); err != nil {
		return 0, err
	}
	return workspaceID, nil
}

func parseUint64ArgumentValue(value any, key string) (uint64, error) {
	switch typed := value.(type) {
	case uint64:
		if typed == 0 {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be greater than 0"}
		}
		return typed, nil
	case uint:
		return parseUint64ArgumentValue(uint64(typed), key)
	case int:
		if typed <= 0 {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be greater than 0"}
		}
		return uint64(typed), nil
	case int64:
		if typed <= 0 {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be greater than 0"}
		}
		return uint64(typed), nil
	case float64:
		if typed <= 0 || typed != float64(uint64(typed)) {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be a positive integer"}
		}
		return uint64(typed), nil
	case json.Number:
		text := strings.TrimSpace(typed.String())
		return parseUint64ArgumentValue(text, key)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " is required"}
		}
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil || parsed == 0 {
			return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be a positive integer"}
		}
		return parsed, nil
	default:
		return 0, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: key + " must be a positive integer"}
	}
}
