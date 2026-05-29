package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"easydo-server/internal/services"
)

func noopToolHandler(context.Context, Invocation) (ToolResult, error) {
	return ToolResult{}, nil
}

func TestRegistryListsOnlyRegisteredTools(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Tool{Name: "pipelines.list", Description: "List pipelines", Handler: noopToolHandler}); err != nil {
		t.Fatalf("register pipelines.list: %v", err)
	}
	if err := registry.Register(Tool{Name: "pipelines.get", Description: "Get pipeline", Handler: noopToolHandler}); err != nil {
		t.Fatalf("register pipelines.get: %v", err)
	}

	tools := registry.List()
	if len(tools) != 2 {
		t.Fatalf("len(tools)=%d, want 2", len(tools))
	}
	if tools[0].Name != "pipelines.list" {
		t.Fatalf("tools[0].Name=%q, want pipelines.list", tools[0].Name)
	}
	if tools[1].Name != "pipelines.get" {
		t.Fatalf("tools[1].Name=%q, want pipelines.get", tools[1].Name)
	}
}

func TestRegistryRejectsUnknownTool(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Invoke(context.Background(), "missing.tool", Invocation{})
	if err == nil {
		t.Fatal("expected not found error")
	}

	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want services.ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeNotFound {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeNotFound)
	}
	if svcErr.Message != "tool not found" {
		t.Fatalf("error message=%q, want tool not found", svcErr.Message)
	}
}

func TestRegistryRejectsNilHandler(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(Tool{Name: "pipelines.list"})
	if err == nil {
		t.Fatal("expected invalid argument error")
	}

	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want services.ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeInvalidArgument)
	}
	if svcErr.Message != "tool handler is required" {
		t.Fatalf("error message=%q, want tool handler is required", svcErr.Message)
	}
}

func TestRegistryGetReturnsIndependentInputSchema(t *testing.T) {
	registry := NewRegistry()
	originalSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string"},
		},
	}

	if err := registry.Register(Tool{Name: "echo", InputSchema: originalSchema, Handler: noopToolHandler}); err != nil {
		t.Fatalf("register echo: %v", err)
	}

	tool, ok := registry.Get("echo")
	if !ok {
		t.Fatal("expected registered tool")
	}
	tool.InputSchema["properties"].(map[string]any)["message"].(map[string]any)["type"] = "number"

	second, ok := registry.Get("echo")
	if !ok {
		t.Fatal("expected registered tool")
	}
	if got := second.InputSchema["properties"].(map[string]any)["message"].(map[string]any)["type"]; got != "string" {
		t.Fatalf("schema type=%v, want string", got)
	}
}

func TestRegistryGetRejectsUnknownTool(t *testing.T) {
	registry := NewRegistry()

	if _, ok := registry.Get("missing"); ok {
		t.Fatal("expected missing tool")
	}
}

func TestRegistryListReturnsIndependentInputSchemas(t *testing.T) {
	registry := NewRegistry()
	originalSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}

	if err := registry.Register(Tool{
		Name:        "pipelines.search",
		Description: "Search pipelines",
		InputSchema: originalSchema,
		Handler:     noopToolHandler,
	}); err != nil {
		t.Fatalf("register pipelines.search: %v", err)
	}

	originalSchema["properties"].(map[string]any)["query"].(map[string]any)["type"] = "number"

	firstList := registry.List()
	firstList[0].InputSchema["properties"].(map[string]any)["query"].(map[string]any)["type"] = "boolean"

	secondList := registry.List()
	got := secondList[0].InputSchema
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema=%#v, want %#v", got, want)
	}
}

func TestRegistryListReturnsIndependentTypedInputSchemaValues(t *testing.T) {
	registry := NewRegistry()
	originalRequired := []string{"query"}
	originalEnums := map[string]string{"fast": "Fast"}
	originalVariants := []map[string]any{{"type": "string"}}
	originalSchema := map[string]any{
		"required": originalRequired,
		"labels":   originalEnums,
		"oneOf":    originalVariants,
	}

	if err := registry.Register(Tool{
		Name:        "pipelines.typed_search",
		Description: "Search pipelines with typed schema values",
		InputSchema: originalSchema,
		Handler:     noopToolHandler,
	}); err != nil {
		t.Fatalf("register pipelines.typed_search: %v", err)
	}

	originalRequired[0] = "mutated-register"
	originalEnums["fast"] = "Mutated Register"
	originalVariants[0]["type"] = "number"

	firstList := registry.List()
	firstList[0].InputSchema["required"].([]string)[0] = "mutated-list"
	firstList[0].InputSchema["labels"].(map[string]string)["fast"] = "Mutated List"
	firstList[0].InputSchema["oneOf"].([]map[string]any)[0]["type"] = "boolean"

	secondList := registry.List()
	got := secondList[0].InputSchema
	want := map[string]any{
		"required": []string{"query"},
		"labels":   map[string]string{"fast": "Fast"},
		"oneOf":    []map[string]any{{"type": "string"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema=%#v, want %#v", got, want)
	}
}

func TestPaginationLimitIsCapped(t *testing.T) {
	page, limit := NormalizePagination(0, 0)
	if page != 1 {
		t.Fatalf("page=%d, want 1", page)
	}
	if limit != DefaultPageLimit {
		t.Fatalf("limit=%d, want %d", limit, DefaultPageLimit)
	}

	page, limit = NormalizePagination(-5, MaxPageLimit+50)
	if page != 1 {
		t.Fatalf("page=%d, want 1", page)
	}
	if limit != MaxPageLimit {
		t.Fatalf("limit=%d, want %d", limit, MaxPageLimit)
	}
}
