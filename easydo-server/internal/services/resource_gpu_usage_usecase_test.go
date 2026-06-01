package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestResourceGPUUsageReturnsSummaryDetailsAndAllocations(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-gpu-user", models.WorkspaceRoleViewer)
	baseInfo := `{
		"schemaVersion":"3.0",
		"source":"remote_task",
		"collectedAt":"2026-06-01T08:00:00Z",
		"resourceInstances":[
			{
				"id":"raw-gpu-instance-0",
				"resourceTypeId":"gpu",
				"nodeId":"host-1",
				"identity":[{"key":"index","value":0},{"key":"uuid","value":"GPU-abc"},{"key":"busId","value":"0000:01:00.0"}],
				"spec":[{"key":"vendor","value":"NVIDIA"},{"key":"model","value":"RTX 4090"}],
				"capacity":[{"key":"memoryBytes","value":25769803776},{"key":"memoryBytesAvailable","value":21474836480}],
				"metrics":[{"key":"memoryBytesUsed","value":4294967296},{"key":"utilizationGpuPercent","value":37},{"key":"temperatureGpuCelsius","value":61}]
			},
			{
				"id":"raw-gpu-instance-1",
				"resourceTypeId":"gpu",
				"nodeId":"host-1",
				"identity":[{"key":"index","value":1},{"key":"uuid","value":"GPU-def"}],
				"spec":[{"key":"vendor","value":"NVIDIA"},{"key":"model","value":"RTX 4090"}],
				"capacity":[{"key":"memoryBytes","value":25769803776}],
				"metrics":[{"key":"memoryBytesUsed","value":0}]
			}
		],
		"services":[
			{
				"id":"raw-service-1",
				"name":"trainer",
				"serviceType":"process",
				"fields":[{"key":"pid","value":1234},{"key":"runtime","value":"docker"},{"key":"containerName","value":"train-container"}]
			}
		],
		"allocations":[
			{
				"id":"raw-allocation-1",
				"claims":[{"serviceId":"raw-service-1","resourceInstanceId":"raw-gpu-instance-0","dimensions":[{"key":"pid","value":1234},{"key":"memoryUsedBytes","value":4294967296}]}]
			}
		]
	}`
	resource := models.Resource{
		WorkspaceID:         workspace.ID,
		Name:                "vm-gpu-resource",
		Type:                models.ResourceTypeVM,
		Status:              models.ResourceStatusOnline,
		BaseInfo:            baseInfo,
		BaseInfoStatus:      "success",
		BaseInfoSource:      "remote_task",
		BaseInfoCollectedAt: 1780298244,
		CreatedBy:           user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResourceGPUUsage(context.Background(), GetResourceGPUUsageRequest{
		Actor:              ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID:        workspace.ID,
		ResourceID:         resource.ID,
		IncludeAllocations: true,
	})
	if err != nil {
		t.Fatalf("GetResourceGPUUsage returned error: %v", err)
	}

	if result.ResourceID != resource.ID || result.WorkspaceID != workspace.ID {
		t.Fatalf("result scope=%+v, want resource/workspace ids", result)
	}
	if result.Freshness.Status != "success" || result.Freshness.CollectedAtUnix != 1780298244 || result.Freshness.CollectedAtText == "" {
		t.Fatalf("freshness=%+v, want status and collection time", result.Freshness)
	}
	if result.Summary.TotalGPUs != 2 || result.Summary.UsedGPUs != 1 || result.Summary.FreeGPUs != 1 {
		t.Fatalf("summary counts=%+v, want 2 total/1 used/1 free", result.Summary)
	}
	if result.Summary.TotalMemoryHuman == "" || result.Summary.UsedMemoryHuman == "" || result.Summary.FreeMemoryHuman == "" {
		t.Fatalf("summary missing human memory fields: %+v", result.Summary)
	}
	if result.Summary.MemoryUsedPercent == nil || *result.Summary.MemoryUsedPercent <= 0 {
		t.Fatalf("summary missing memory used percent: %+v", result.Summary)
	}
	if len(result.GPUs) != 2 {
		t.Fatalf("gpu count=%d, want 2", len(result.GPUs))
	}
	first := result.GPUs[0]
	if first.Index == nil || *first.Index != 0 || first.UUID != "GPU-abc" || first.Model != "RTX 4090" || first.MemoryUsedHuman == "" {
		t.Fatalf("first gpu=%+v, want identity/spec/memory details", first)
	}
	if first.MemoryUsedPercent == nil || *first.MemoryUsedPercent <= 0 {
		t.Fatalf("first gpu missing memory used percent: %+v", first)
	}
	if first.UtilizationGPUPercent == nil || *first.UtilizationGPUPercent != 37 {
		t.Fatalf("first utilization=%v, want 37", first.UtilizationGPUPercent)
	}
	if first.Occupancy == nil || first.Occupancy.ServiceCount != 1 || first.Occupancy.MemoryUsedHuman == "" || first.Occupancy.MemoryUsedPercent == nil || *first.Occupancy.MemoryUsedPercent <= 0 {
		t.Fatalf("first occupancy=%+v, want per-gpu occupancy summary", first.Occupancy)
	}
	if len(first.Services) != 1 || first.Services[0].DisplayName != "train-container" || len(first.Services[0].Processes) != 1 || first.Services[0].GPUMemoryPercent == nil || *first.Services[0].GPUMemoryPercent <= 0 || first.Services[0].OccupancyMemoryPercent == nil || *first.Services[0].OccupancyMemoryPercent != 100 {
		t.Fatalf("first services=%+v, want service usage embedded under GPU", first.Services)
	}
	if first.Services[0].Processes[0].PID == nil || *first.Services[0].Processes[0].PID != 1234 || first.Services[0].Processes[0].MemoryUsedHuman == "" {
		t.Fatalf("first service processes=%+v, want PID and memory usage", first.Services[0].Processes)
	}
	if len(result.Allocations) != 1 {
		t.Fatalf("allocations=%+v, want one allocation", result.Allocations)
	}
	allocation := result.Allocations[0]
	if allocation.GPUUUID != "GPU-abc" || allocation.ServiceName != "trainer" || allocation.PID == nil || *allocation.PID != 1234 || allocation.ContainerName != "train-container" || allocation.MemoryUsedHuman == "" {
		t.Fatalf("allocation=%+v, want gpu/service/process details", allocation)
	}

	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal gpu usage failed: %v", err)
	}
	body := strings.ToLower(string(serialized))
	for _, forbidden := range []string{"raw-gpu-instance-0", "raw-gpu-instance-1", "raw-service-1", "raw-allocation-1", "password", "token", "authorization"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("gpu usage leaked forbidden/raw id text %q: %s", forbidden, string(serialized))
		}
	}
}

func TestResourceGPUUsageTruthfullyReportsNoGPUAndFailedCollection(t *testing.T) {
	db := openResourceQueryTestDB(t)
	usecase := &ResourceUseCase{DB: db}
	user, workspace := seedResourceQueryWorkspaceMember(t, db, "resource-gpu-empty-user", models.WorkspaceRoleViewer)
	resource := models.Resource{
		WorkspaceID:       workspace.ID,
		Name:              "failed-gpu-resource",
		Type:              models.ResourceTypeVM,
		Status:            models.ResourceStatusOnline,
		BaseInfo:          `{"schemaVersion":"3.0","resourceInstances":[]}`,
		BaseInfoStatus:    "failed",
		BaseInfoSource:    "remote_task",
		BaseInfoLastError: "password=hidden",
		CreatedBy:         user.ID,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}

	result, err := usecase.GetResourceGPUUsage(context.Background(), GetResourceGPUUsageRequest{
		Actor:       ActorContext{UserID: user.ID, Username: user.Username, SystemRole: user.Role},
		WorkspaceID: workspace.ID,
		ResourceID:  resource.ID,
	})
	if err != nil {
		t.Fatalf("GetResourceGPUUsage returned error: %v", err)
	}
	if result.Summary.TotalGPUs != 0 || result.Freshness.Status != "failed" {
		t.Fatalf("result=%+v, want no gpu and failed freshness", result)
	}
	if strings.Contains(strings.ToLower(result.Freshness.LastError), "password=hidden") {
		t.Fatalf("last error was not sanitized: %+v", result.Freshness)
	}
}
