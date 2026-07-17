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

type fakeResourceRefreshService struct {
	lastRequest services.RefreshResourceBaseInfoRequest
	result      services.RefreshResourceBaseInfoResult
	err         error
}

func (f *fakeResourceRefreshService) RequestResourceBaseInfoRefresh(ctx context.Context, req services.RefreshResourceBaseInfoRequest) (services.RefreshResourceBaseInfoResult, error) {
	f.lastRequest = req
	if f.err != nil {
		return services.RefreshResourceBaseInfoResult{}, f.err
	}
	return f.result, nil
}

type fakeResolvingResourceRefreshService struct {
	*fakeResourceRefreshService
	resolvedResourceID uint64
}

func (f *fakeResolvingResourceRefreshService) ResolveMCPResourceID(_ context.Context, _ services.ActorContext, _ uint64, _ any) (uint64, error) {
	return f.resolvedResourceID, nil
}

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

func TestResourceGPUUsageToolReturnsStructuredGPUUsage(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, workspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-gpu-user", models.WorkspaceRoleViewer)
	resource := models.Resource{
		WorkspaceID:         workspace.ID,
		Name:                "tool-gpu-resource",
		Type:                models.ResourceTypeVM,
		Status:              models.ResourceStatusOnline,
		BaseInfoStatus:      "success",
		BaseInfoSource:      "remote_task",
		BaseInfoCollectedAt: 1780298244,
		BaseInfo: `{
			"schemaVersion":3,
			"source":"remote_task",
			"resourceInstances":[{
				"id":"raw-gpu-instance-0",
				"resourceTypeId":"gpu",
				"identity":[{"name":"index","value":0},{"name":"uuid","value":"GPU-tool"}],
				"spec":[{"name":"vendor","value":"NVIDIA"},{"name":"model","value":"A100"}],
				"capacity":[{"name":"memoryBytes","capacity":85899345920}],
				"metrics":[{"name":"memoryBytesUsed","value":10737418240}]
			}],
			"services":[{"id":"raw-service-1","name":"trainer","fields":[{"name":"pid","value":4321},{"name":"runtime","value":"docker"},{"name":"containerName","value":"tool-container"}]}],
			"allocations":[{"id":"raw-allocation-1","claims":[{"serviceId":"raw-service-1","resourceInstanceId":"raw-gpu-instance-0","dimensions":[{"name":"pid","value":4321},{"name":"memoryUsedBytes","value":10737418240}]}]}]
		}`,
		CreatedBy: user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := registry.Invoke(context.Background(), "easydo_resource_gpu_usage", Invocation{
		Actor:     services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		Arguments: map[string]any{"workspace_id": workspace.ID, "resource_id": resource.ID},
	})
	if err != nil {
		t.Fatalf("gpu usage invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(services.ResourceGPUUsageResult)
	if !ok {
		t.Fatalf("structured content type=%T, want services.ResourceGPUUsageResult", result.StructuredContent)
	}
	if content.Summary.TotalGPUs != 1 || len(content.GPUs) != 1 || content.GPUs[0].UUID != "GPU-tool" {
		t.Fatalf("content=%+v, want one GPU", content)
	}
	if content.Summary.MemoryUsedPercent == nil || *content.Summary.MemoryUsedPercent <= 0 || content.GPUs[0].MemoryUsedPercent == nil || *content.GPUs[0].MemoryUsedPercent <= 0 {
		t.Fatalf("content=%+v, want summary and per-gpu memory percentages", content)
	}
	if content.GPUs[0].Occupancy == nil || len(content.GPUs[0].Services) != 1 || content.GPUs[0].Services[0].DisplayName != "tool-container" {
		t.Fatalf("gpu detail=%+v, want embedded service occupancy", content.GPUs[0])
	}
	if content.GPUs[0].Occupancy.MemoryUsedPercent == nil || *content.GPUs[0].Occupancy.MemoryUsedPercent <= 0 || content.GPUs[0].Services[0].GPUMemoryPercent == nil || *content.GPUs[0].Services[0].GPUMemoryPercent <= 0 || content.GPUs[0].Services[0].OccupancyMemoryPercent == nil || *content.GPUs[0].Services[0].OccupancyMemoryPercent != 100 {
		t.Fatalf("gpu detail=%+v, want occupancy and service percentage fields", content.GPUs[0])
	}
}

func TestResourceGPUUsageToolResolvesResourceNameLabel(t *testing.T) {
	db := openResourceToolTestDB(t)
	usecase := &services.ResourceUseCase{DB: db}
	registry := NewRegistry()
	if err := RegisterResourceTools(registry, usecase); err != nil {
		t.Fatalf("RegisterResourceTools returned error: %v", err)
	}
	user, workspace := seedResourceToolWorkspaceMember(t, db, "resource-tool-gpu-label-user", models.WorkspaceRoleViewer)
	resource := models.Resource{
		WorkspaceID:    workspace.ID,
		Name:           "7022",
		Type:           models.ResourceTypeVM,
		Status:         models.ResourceStatusOnline,
		BaseInfoStatus: "success",
		BaseInfo:       `{"schemaVersion":3,"resourceInstances":[{"id":"gpu-0","resourceTypeId":"gpu","identity":[{"name":"uuid","value":"GPU-label"}],"capacity":[{"name":"memoryBytes","capacity":100}],"metrics":[{"name":"memoryBytesUsed","value":25}]}]}`,
		CreatedBy:      user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := registry.Invoke(context.Background(), "easydo_resource_gpu_usage", Invocation{
		Actor:     services.ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		Arguments: map[string]any{"workspace_id": workspace.ID, "resource_id": "7022"},
	})
	if err != nil {
		t.Fatalf("gpu usage invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(services.ResourceGPUUsageResult)
	if !ok {
		t.Fatalf("structured content type=%T, want services.ResourceGPUUsageResult", result.StructuredContent)
	}
	if content.ResourceID != resource.ID || len(content.GPUs) != 1 || content.GPUs[0].UUID != "GPU-label" {
		t.Fatalf("content=%+v, want resolved internal resource %d", content, resource.ID)
	}
}

func TestResourceBaseInfoRefreshToolPassesRequestToService(t *testing.T) {
	fake := &fakeResourceRefreshService{result: services.RefreshResourceBaseInfoResult{TaskID: 77, Status: models.TaskStatusQueued, AgentID: 9}}
	registry := NewRegistry()
	if err := RegisterResourceOperationTools(registry, fake); err != nil {
		t.Fatalf("RegisterResourceOperationTools returned error: %v", err)
	}
	actor := services.ActorContext{UserID: 12, Username: "refresh-user", SystemRole: "user"}

	result, err := registry.Invoke(context.Background(), "easydo_resource_base_info_refresh", Invocation{
		Actor:     actor,
		Arguments: map[string]any{"workspace_id": 34, "resource_id": 56},
		Protocol:  streamableHTTPProtocol,
	})
	if err != nil {
		t.Fatalf("refresh invoke returned error: %v", err)
	}
	content, ok := result.StructuredContent.(services.RefreshResourceBaseInfoResult)
	if !ok {
		t.Fatalf("structured content type=%T, want services.RefreshResourceBaseInfoResult", result.StructuredContent)
	}
	if content.TaskID != 77 || content.Status != models.TaskStatusQueued || content.AgentID != 9 {
		t.Fatalf("content=%+v, want fake result", content)
	}
	if fake.lastRequest.WorkspaceID != 34 || fake.lastRequest.ResourceID != 56 || fake.lastRequest.Actor.UserID != actor.UserID {
		t.Fatalf("last request=%+v, want parsed actor and ids", fake.lastRequest)
	}
}

func TestResourceBaseInfoRefreshToolUsesResolverWhenAvailable(t *testing.T) {
	fake := &fakeResolvingResourceRefreshService{
		fakeResourceRefreshService: &fakeResourceRefreshService{result: services.RefreshResourceBaseInfoResult{TaskID: 77, Status: models.TaskStatusQueued, AgentID: 9}},
		resolvedResourceID:         2,
	}
	registry := NewRegistry()
	if err := RegisterResourceOperationTools(registry, fake); err != nil {
		t.Fatalf("RegisterResourceOperationTools returned error: %v", err)
	}
	actor := services.ActorContext{UserID: 12, Username: "refresh-user", SystemRole: "user"}

	_, err := registry.Invoke(context.Background(), "easydo_resource_base_info_refresh", Invocation{
		Actor:     actor,
		Arguments: map[string]any{"workspace_id": 34, "resource_id": "7022"},
	})
	if err != nil {
		t.Fatalf("refresh invoke returned error: %v", err)
	}
	if fake.lastRequest.ResourceID != 2 {
		t.Fatalf("last request resource_id=%d, want resolved internal id 2", fake.lastRequest.ResourceID)
	}
}

func TestResourceBaseInfoRefreshToolRequiresIDs(t *testing.T) {
	registry := NewRegistry()
	if err := RegisterResourceOperationTools(registry, &fakeResourceRefreshService{}); err != nil {
		t.Fatalf("RegisterResourceOperationTools returned error: %v", err)
	}
	_, err := registry.Invoke(context.Background(), "easydo_resource_base_info_refresh", Invocation{
		Actor:     services.ActorContext{UserID: 12, Username: "refresh-user", SystemRole: "user"},
		Arguments: map[string]any{"workspace_id": 34},
	})
	if err == nil {
		t.Fatal("expected resource_id invalid argument error")
	}
	var svcErr services.ServiceError
	if !errors.As(err, &svcErr) || svcErr.Code != services.ErrorCodeInvalidArgument {
		t.Fatalf("error=%v, want invalid argument ServiceError", err)
	}
}
