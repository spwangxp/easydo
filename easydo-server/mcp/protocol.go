package mcp

import (
	"context"
	"encoding/json"
)

const mcpProtocolVersion = "2025-06-18"

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *JSONRPCError  `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func HandleJSONRPC(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, bool) {
	if req.ID == nil {
		return nil, false
	}

	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "easydo-mcp-server",
				"version": "0.1.0",
			},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{
			"tools": []any{},
		}
	case "tools/call":
		resp.Error = &JSONRPCError{
			Code:    jsonRPCCodeMethodNotFound,
			Message: "tool not found",
		}
	default:
		resp.Error = &JSONRPCError{
			Code:    jsonRPCCodeMethodNotFound,
			Message: "method not found",
		}
	}

	return resp, true
}
