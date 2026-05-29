package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
)

func TestWorkspaceListToolDoesNotRequireWorkspaceID(t *testing.T) {
	db := openAuditTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}); err != nil {
		t.Fatalf("auto migrate workspace models failed: %v", err)
	}
	usecase := &services.WorkspaceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterWorkspaceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterWorkspaceTools returned error: %v", err)
	}

	actor := services.ActorContext{UserID: 1001, Username: "admin-user", SystemRole: "admin"}
	workspace := models.Workspace{Name: "admin-space", Slug: "admin-space", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindAdmin, CreatedBy: actor.UserID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}

	result, err := registry.Invoke(context.Background(), "easydo_workspace_list", Invocation{Actor: actor, Arguments: map[string]any{"page": 1.0, "limit": 20.0}})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type=%T, want map[string]any", result.StructuredContent)
	}
	if _, exists := content["current_workspace_id"]; !exists {
		t.Fatalf("structured content=%v, want current_workspace_id", content)
	}
}

func TestWorkspaceGetToolRequiresWorkspaceID(t *testing.T) {
	registry := NewRegistry()
	if err := RegisterWorkspaceTools(registry, &services.WorkspaceUseCase{}); err != nil {
		t.Fatalf("RegisterWorkspaceTools returned error: %v", err)
	}

	_, err := registry.Invoke(context.Background(), "easydo_workspace_get", Invocation{Actor: services.ActorContext{UserID: 1001, Username: "user", SystemRole: "user"}, Arguments: map[string]any{}})
	if err == nil {
		t.Fatal("expected invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T, want ServiceError", err)
	}
	if svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error code=%q, want %q", svcErr.Code, services.ErrorCodeInvalidArgument)
	}
}

func TestWorkspaceToolsReturnStructuredContent(t *testing.T) {
	db := openAuditTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}); err != nil {
		t.Fatalf("auto migrate workspace models failed: %v", err)
	}
	usecase := &services.WorkspaceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterWorkspaceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterWorkspaceTools returned error: %v", err)
	}

	user := models.User{Username: "workspace-tool-user", Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: "team-space", Slug: "team-space", Description: "workspace description", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: "", CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: models.WorkspaceRoleDeveloper, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}
	actor := services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}

	listResult, err := registry.Invoke(context.Background(), "easydo_workspace_list", Invocation{Actor: actor, Arguments: map[string]any{"query": "workspace", "page": json.Number("1"), "limit": "5"}})
	if err != nil {
		t.Fatalf("list invoke returned error: %v", err)
	}
	listContent, ok := listResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("list structured content type=%T, want map[string]any", listResult.StructuredContent)
	}
	listItems, ok := listContent["list"].([]services.WorkspaceSummary)
	if !ok {
		t.Fatalf("list content list type=%T, want []services.WorkspaceSummary", listContent["list"])
	}
	if len(listItems) != 1 {
		t.Fatalf("list size=%d, want 1", len(listItems))
	}
	if listItems[0].Kind != models.WorkspaceKindNormal {
		t.Fatalf("list kind=%q, want %q", listItems[0].Kind, models.WorkspaceKindNormal)
	}
	if listItems[0].Role != models.WorkspaceRoleDeveloper {
		t.Fatalf("list role=%q, want %q", listItems[0].Role, models.WorkspaceRoleDeveloper)
	}

	getResult, err := registry.Invoke(context.Background(), "easydo_workspace_get", Invocation{Actor: actor, Arguments: map[string]any{"workspace_id": workspace.ID}})
	if err != nil {
		t.Fatalf("get invoke returned error: %v", err)
	}
	getContent, ok := getResult.StructuredContent.(services.WorkspaceSummary)
	if !ok {
		t.Fatalf("get structured content type=%T, want services.WorkspaceSummary", getResult.StructuredContent)
	}
	if getContent.ID != workspace.ID {
		t.Fatalf("workspace id=%d, want %d", getContent.ID, workspace.ID)
	}
	if getContent.Kind != models.WorkspaceKindNormal {
		t.Fatalf("workspace kind=%q, want %q", getContent.Kind, models.WorkspaceKindNormal)
	}
	if len(getContent.Capabilities) == 0 {
		t.Fatal("expected capabilities in get result")
	}
	serialized, err := json.Marshal(getResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(serialized, &payload); err != nil {
		t.Fatalf("unmarshal structured content failed: %v", err)
	}
	expectedKeys := map[string]bool{
		"id":           true,
		"name":         true,
		"description":  true,
		"status":       true,
		"visibility":   true,
		"kind":         true,
		"role":         true,
		"capabilities": true,
	}
	if len(payload) != len(expectedKeys) {
		t.Fatalf("serialized keys=%v, want only %v", payload, expectedKeys)
	}
	for key := range expectedKeys {
		if _, exists := payload[key]; !exists {
			t.Fatalf("serialized structured content missing key %q: %v", key, payload)
		}
	}
	for _, forbiddenKey := range []string{"created_at", "updated_at", "created_by", "members", "workspace", "error", "error_details"} {
		if _, exists := payload[forbiddenKey]; exists {
			t.Fatalf("serialized structured content exposed %q: %v", forbiddenKey, payload)
		}
	}
}
