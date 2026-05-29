package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"easydo-server/internal/services"
)

func TestHandleInitializeReturnsServerCapabilities(t *testing.T) {
	resp, ok := HandleJSONRPC(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})
	if !ok {
		t.Fatal("expected response for initialize")
	}

	if resp.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc=%q, want 2.0", resp.JSONRPC)
	}
	if resp.ID != 1 {
		t.Fatalf("id=%v, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type=%T, want map[string]any", resp.Result)
	}

	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion=%v, want 2025-06-18", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo type=%T, want map[string]any", result["serverInfo"])
	}
	if serverInfo["name"] != "easydo-mcp-server" {
		t.Fatalf("serverInfo.name=%v, want easydo-mcp-server", serverInfo["name"])
	}

	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities type=%T, want map[string]any", result["capabilities"])
	}
	tools, ok := capabilities["tools"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities.tools type=%T, want map[string]any", capabilities["tools"])
	}
	if len(tools) != 0 {
		t.Fatalf("expected empty tools capability object, got %#v", tools)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	resp, ok := HandleJSONRPC(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "req-1",
		Method:  "unknown/method",
	})
	if !ok {
		t.Fatal("expected response for unknown method")
	}

	if resp.Error == nil {
		t.Fatal("expected method not found error")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("error code=%d, want -32601", resp.Error.Code)
	}
	if resp.ID != "req-1" {
		t.Fatalf("id=%v, want req-1", resp.ID)
	}
}

func TestNotificationInitializedProducesNoResponse(t *testing.T) {
	resp, ok := HandleJSONRPC(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})

	if ok {
		t.Fatalf("expected no response for notification, got %+v", resp)
	}
	if resp != nil {
		t.Fatalf("expected nil response for notification, got %+v", resp)
	}
}

func TestRequestsWithoutIDProduceNoResponse(t *testing.T) {
	resp, ok := HandleJSONRPC(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "ping",
	})

	if ok {
		t.Fatalf("expected no response for request without id, got %+v", resp)
	}
	if resp != nil {
		t.Fatalf("expected nil response for request without id, got %+v", resp)
	}
}

func TestServiceErrorMapsToMCPError(t *testing.T) {
	assertForbiddenMCPError(t, MapError(services.ServiceError{Code: services.ErrorCodeForbidden, Message: "access denied"}))
}

func TestServiceErrorMapsPointerAndWrappedErrors(t *testing.T) {
	pointerErr := &services.ServiceError{Code: services.ErrorCodeForbidden, Message: "access denied"}
	assertForbiddenMCPError(t, MapError(pointerErr))
	assertForbiddenMCPError(t, MapError(fmt.Errorf("wrapped: %w", pointerErr)))
}

func TestUnknownErrorMapsToGenericInternalError(t *testing.T) {
	mcpErr := MapError(errors.New("database password leaked in stack details"))

	if mcpErr == nil {
		t.Fatal("expected mapped error")
	}
	if mcpErr.Message != "internal error" {
		t.Fatalf("message=%q, want internal error", mcpErr.Message)
	}
	data, ok := mcpErr.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type=%T, want map[string]any", mcpErr.Data)
	}
	if data["code"] != string(services.ErrorCodeInternalError) {
		t.Fatalf("data.code=%v, want internal_error", data["code"])
	}
}

func assertForbiddenMCPError(t *testing.T, mcpErr *JSONRPCError) {
	t.Helper()
	if mcpErr == nil {
		t.Fatal("expected mapped error")
	}
	if mcpErr.Data == nil {
		t.Fatal("expected error data")
	}

	data, ok := mcpErr.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type=%T, want map[string]any", mcpErr.Data)
	}
	if data["code"] != string(services.ErrorCodeForbidden) {
		t.Fatalf("data.code=%v, want forbidden", data["code"])
	}
}
