package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"easydo-server/internal/models"
	"easydo-server/internal/services"

	"gorm.io/gorm"
)

func openResourceToolTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openAuditTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.WorkspaceMember{}, &models.Resource{}, &models.ResourceCredentialBinding{}, &models.ResourceHealthSnapshot{}); err != nil {
		t.Fatalf("auto migrate resource tool models failed: %v", err)
	}
	return db
}

func seedResourceToolWorkspaceMember(t *testing.T, db *gorm.DB, username string, role string) (models.User, models.Workspace) {
	t.Helper()
	user := models.User{Username: username, Role: "user", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	workspace := models.Workspace{Name: username + "-workspace", Slug: username + "-workspace", Status: models.WorkspaceStatusActive, Visibility: models.WorkspaceVisibilityPrivate, Kind: models.WorkspaceKindNormal, CreatedBy: user.ID}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	member := models.WorkspaceMember{WorkspaceID: workspace.ID, UserID: user.ID, Role: role, Status: models.WorkspaceMemberStatusActive}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create membership failed: %v", err)
	}
	return user, workspace
}

func TestResourceListToolRequiresExplicitWorkspaceIDAndCapsLimit(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, workspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-list-user", models.WorkspaceRoleViewer)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "tool-list-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	_, err := registry.Invoke(context.Background(), "easydo_resource_list", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"limit": 500}})
	if err == nil {
		t.Fatal("expected workspace_id invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error=%v, want invalid argument ServiceError", err)
	}

	result, err := registry.Invoke(context.Background(), "easydo_resource_list", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "page": json.Number("1"), "limit": "500"}})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(services.ResourceListResult)
	if !ok {
		t.Fatalf("structured content type=%T, want services.ResourceListResult", result.StructuredContent)
	}
	if content.Limit != 100 {
		t.Fatalf("limit=%d, want 100", content.Limit)
	}
	if len(content.List) != 1 || content.List[0].ID != resource.ID {
		t.Fatalf("list=%+v, want resource %d", content.List, resource.ID)
	}
}

func TestResourceGetToolPassesThroughServiceErrors(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, workspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-get-user", models.WorkspaceRoleViewer)
	_, otherWorkspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-other-user", models.WorkspaceRoleViewer)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "tool-get-resource", Type: models.ResourceTypeVM, Status: models.ResourceStatusOnline, CreatedBy: user.ID}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	_, err := registry.Invoke(context.Background(), "easydo_resource_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": otherWorkspace.ID, "resource_id": resource.ID}})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != services.ErrorCodeForbidden {
		t.Fatalf("error=%v, want forbidden ServiceError", err)
	}

	_, err = registry.Invoke(context.Background(), "easydo_resource_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "resource_id": resource.ID + 999}})
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.As(err, &svcErr) || svcErr.Code != services.ErrorCodeNotFound {
		t.Fatalf("error=%v, want not found ServiceError", err)
	}
}

func TestResourceGetAndStatusToolsReturnSanitizedStructuredContent(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, workspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-status-user", models.WorkspaceRoleViewer)
	large := strings.Repeat("x", 5000)
	resource := models.Resource{WorkspaceID: workspace.ID, Name: "tool-status-resource", Type: models.ResourceTypeK8sCluster, Status: models.ResourceStatusOnline, CreatedBy: user.ID, Endpoint: "https://cluster.example", Metadata: `{"credential_binding":{"token":"hidden"},"safe":"ok"}`, BaseInfo: `{"nodes":[{"name":"node-1","password":"hidden","measure":"` + large + `"}],"authorization":"Bearer hidden"}`, LastCheckResult: "secret=health-secret"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	getResult, err := registry.Invoke(context.Background(), "easydo_resource_get", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "resource_id": resource.ID}})
	if err != nil {
		t.Fatalf("resource get invoke returned error: %v", err)
	}
	if _, ok := getResult.StructuredContent.(services.ResourceDetail); !ok {
		t.Fatalf("get structured content type=%T, want services.ResourceDetail", getResult.StructuredContent)
	}

	statusResult, err := registry.Invoke(context.Background(), "easydo_resource_status", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"workspace_id": workspace.ID, "resource_id": resource.ID}})
	if err != nil {
		t.Fatalf("resource status invoke returned error: %v", err)
	}
	status, ok := statusResult.StructuredContent.(services.ResourceStatusSummary)
	if !ok {
		t.Fatalf("status structured content type=%T, want services.ResourceStatusSummary", statusResult.StructuredContent)
	}
	serializedGet, err := json.Marshal(getResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal get detail failed: %v", err)
	}
	serializedStatus, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status failed: %v", err)
	}
	combined := strings.ToLower(string(serializedGet) + string(serializedStatus))
	for _, forbidden := range []string{"credential", "token", "secret", "password", "authorization", "bearer hidden", "health-secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("resource tools leaked forbidden text %q: get=%s status=%s", forbidden, string(serializedGet), string(serializedStatus))
		}
	}
	if len(serializedStatus) > 4500 || !strings.Contains(string(serializedStatus), "[truncated]") {
		t.Fatalf("status was not capped/truncated, len=%d body=%s", len(serializedStatus), string(serializedStatus))
	}
}

func TestResourceStatusToolRequiresExplicitWorkspaceID(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, _ := seedResourceToolWorkspaceMember(t, db, "resource-tool-required-user", models.WorkspaceRoleViewer)

	_, err := registry.Invoke(context.Background(), "easydo_resource_status", Invocation{Actor: services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role}, Arguments: map[string]any{"resource_id": 1}})
	if err == nil {
		t.Fatal("expected workspace_id invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error=%v, want invalid argument ServiceError", err)
	}
}
