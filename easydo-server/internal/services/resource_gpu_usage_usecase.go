package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easydo-server/internal/models"
)

type GetResourceGPUUsageRequest struct {
	Actor              ActorContext
	WorkspaceID        uint64
	ResourceID         uint64
	IncludeAllocations bool
}

type ResourceGPUUsageResult struct {
	ResourceID       uint64                  `json:"resource_id"`
	WorkspaceID      uint64                  `json:"workspace_id"`
	ResourceName     string                  `json:"resource_name"`
	ResourceType     models.ResourceType     `json:"resource_type"`
	ResourceStatus   models.ResourceStatus   `json:"resource_status"`
	SchemaVersion    string                  `json:"schema_version,omitempty"`
	Freshness        ResourceGPUFreshness    `json:"freshness"`
	Summary          ResourceGPUSummary      `json:"summary"`
	GPUs             []ResourceGPUDetail     `json:"gpus"`
	Allocations      []ResourceGPUAllocation `json:"allocations,omitempty"`
	AllocationStatus string                  `json:"allocation_status,omitempty"`
}

type ResourceGPUFreshness struct {
	Status          string `json:"status"`
	Source          string `json:"source,omitempty"`
	CollectedAtUnix int64  `json:"collected_at_unix,omitempty"`
	CollectedAtText string `json:"collected_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

type ResourceGPUSummary struct {
	TotalGPUs                    int      `json:"total_gpus"`
	UsedGPUs                     int      `json:"used_gpus"`
	FreeGPUs                     int      `json:"free_gpus"`
	TotalMemoryBytes             int64    `json:"total_memory_bytes,omitempty"`
	UsedMemoryBytes              int64    `json:"used_memory_bytes,omitempty"`
	FreeMemoryBytes              int64    `json:"free_memory_bytes,omitempty"`
	TotalMemoryHuman             string   `json:"total_memory_human,omitempty"`
	UsedMemoryHuman              string   `json:"used_memory_human,omitempty"`
	FreeMemoryHuman              string   `json:"free_memory_human,omitempty"`
	MemoryUsedPercent            *float64 `json:"memory_used_percent,omitempty"`
	AverageUtilizationGPUPercent *float64 `json:"average_utilization_gpu_percent,omitempty"`
}

type ResourceGPUDetail struct {
	Index                 *int                      `json:"index,omitempty"`
	UUID                  string                    `json:"uuid,omitempty"`
	BusID                 string                    `json:"bus_id,omitempty"`
	Name                  string                    `json:"name,omitempty"`
	NodeID                string                    `json:"node_id,omitempty"`
	Vendor                string                    `json:"vendor,omitempty"`
	Model                 string                    `json:"model,omitempty"`
	MemoryBytes           int64                     `json:"memory_bytes,omitempty"`
	MemoryHuman           string                    `json:"memory_human,omitempty"`
	MemoryUsedBytes       int64                     `json:"memory_used_bytes,omitempty"`
	MemoryUsedHuman       string                    `json:"memory_used_human,omitempty"`
	MemoryUsedPercent     *float64                  `json:"memory_used_percent,omitempty"`
	MemoryAvailableBytes  int64                     `json:"memory_available_bytes,omitempty"`
	MemoryAvailableHuman  string                    `json:"memory_available_human,omitempty"`
	MemoryFreeBytes       int64                     `json:"memory_free_bytes,omitempty"`
	MemoryFreeHuman       string                    `json:"memory_free_human,omitempty"`
	MemoryFreePercent     *float64                  `json:"memory_free_percent,omitempty"`
	UtilizationGPUPercent *float64                  `json:"utilization_gpu_percent,omitempty"`
	TemperatureGPUCelsius *float64                  `json:"temperature_gpu_celsius,omitempty"`
	Status                string                    `json:"status"`
	MetricsSource         string                    `json:"metrics_source,omitempty"`
	Occupancy             *ResourceGPUOccupancy     `json:"occupancy,omitempty"`
	Services              []ResourceGPUServiceUsage `json:"services,omitempty"`
}

type ResourceGPUOccupancy struct {
	ServiceCount      int      `json:"service_count"`
	ClaimCount        int      `json:"claim_count"`
	MemoryUsedBytes   int64    `json:"memory_used_bytes,omitempty"`
	MemoryUsedHuman   string   `json:"memory_used_human,omitempty"`
	MemoryUsedPercent *float64 `json:"memory_used_percent,omitempty"`
}

type ResourceGPUServiceUsage struct {
	DisplayName            string                    `json:"display_name,omitempty"`
	ServiceName            string                    `json:"service_name,omitempty"`
	ServiceType            string                    `json:"service_type,omitempty"`
	Runtime                string                    `json:"runtime,omitempty"`
	PID                    *int                      `json:"pid,omitempty"`
	PIDs                   []int                     `json:"pids,omitempty"`
	ObservedPIDs           []int                     `json:"observed_pids,omitempty"`
	ContainerID            string                    `json:"container_id,omitempty"`
	ContainerName          string                    `json:"container_name,omitempty"`
	PodUID                 string                    `json:"pod_uid,omitempty"`
	PodNamespace           string                    `json:"pod_namespace,omitempty"`
	PodName                string                    `json:"pod_name,omitempty"`
	NodeName               string                    `json:"node_name,omitempty"`
	Phase                  string                    `json:"phase,omitempty"`
	OwnerKind              string                    `json:"owner_kind,omitempty"`
	OwnerName              string                    `json:"owner_name,omitempty"`
	OwnerDisplayName       string                    `json:"owner_display_name,omitempty"`
	MemoryUsedBytes        int64                     `json:"memory_used_bytes,omitempty"`
	MemoryUsedHuman        string                    `json:"memory_used_human,omitempty"`
	GPUMemoryPercent       *float64                  `json:"gpu_memory_percent,omitempty"`
	OccupancyMemoryPercent *float64                  `json:"occupancy_memory_percent,omitempty"`
	ProcessCount           int                       `json:"process_count,omitempty"`
	DescendantProcessCount int                       `json:"descendant_process_count,omitempty"`
	Processes              []ResourceGPUProcessUsage `json:"processes,omitempty"`
}

type ResourceGPUProcessUsage struct {
	PID              *int     `json:"pid,omitempty"`
	ObservedPID      *int     `json:"observed_pid,omitempty"`
	MemoryUsedBytes  int64    `json:"memory_used_bytes,omitempty"`
	MemoryUsedHuman  string   `json:"memory_used_human,omitempty"`
	GPUMemoryPercent *float64 `json:"gpu_memory_percent,omitempty"`
	IsDescendant     bool     `json:"is_descendant,omitempty"`
}

type ResourceGPUAllocation struct {
	GPUIndex        *int   `json:"gpu_index,omitempty"`
	GPUUUID         string `json:"gpu_uuid,omitempty"`
	GPUBusID        string `json:"gpu_bus_id,omitempty"`
	ServiceName     string `json:"service_name,omitempty"`
	ServiceType     string `json:"service_type,omitempty"`
	PID             *int   `json:"pid,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	ContainerID     string `json:"container_id,omitempty"`
	ContainerName   string `json:"container_name,omitempty"`
	PodUID          string `json:"pod_uid,omitempty"`
	PodNamespace    string `json:"pod_namespace,omitempty"`
	PodName         string `json:"pod_name,omitempty"`
	NodeName        string `json:"node_name,omitempty"`
	OwnerKind       string `json:"owner_kind,omitempty"`
	OwnerName       string `json:"owner_name,omitempty"`
	MemoryUsedBytes int64  `json:"memory_used_bytes,omitempty"`
	MemoryUsedHuman string `json:"memory_used_human,omitempty"`
}

type resourceGPUDetailWithRawID struct {
	rawID  string
	detail ResourceGPUDetail
}

type resourceGPUAllocationBuildResult struct {
	allocations         []ResourceGPUAllocation
	servicesByGPURawID  map[string][]ResourceGPUServiceUsage
	occupancyByGPURawID map[string]ResourceGPUOccupancy
}

type resourceGPUServiceUsageAccumulator struct {
	usage        ResourceGPUServiceUsage
	claimCount   int
	pids         map[int]struct{}
	observedPIDs map[int]struct{}
}

type resourceBaseInfoV3GPUView struct {
	SchemaVersion     any                                    `json:"schemaVersion"`
	Source            string                                 `json:"source"`
	CollectedAt       string                                 `json:"collectedAt"`
	ResourceInstances []resourceBaseInfoResourceInstanceView `json:"resourceInstances"`
	Services          []resourceBaseInfoServiceView          `json:"services"`
	Allocations       []resourceBaseInfoAllocationView       `json:"allocations"`
}

type resourceBaseInfoResourceInstanceView struct {
	ID             string                        `json:"id"`
	ResourceTypeID string                        `json:"resourceTypeId"`
	NodeID         string                        `json:"nodeId"`
	EntityID       string                        `json:"entityId"`
	Identity       []resourceBaseInfoFieldView   `json:"identity"`
	Spec           []resourceBaseInfoFieldView   `json:"spec"`
	Capacity       []resourceBaseInfoMeasureView `json:"capacity"`
	Metrics        []resourceBaseInfoMeasureView `json:"metrics"`
}

type resourceBaseInfoServiceView struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name"`
	EntityID            string                      `json:"entityId"`
	ResourceInstanceIDs []string                    `json:"resourceInstanceIds"`
	Fields              []resourceBaseInfoFieldView `json:"fields"`
}

type resourceBaseInfoAllocationView struct {
	ID     string                                `json:"id"`
	Claims []resourceBaseInfoAllocationClaimView `json:"claims"`
}

type resourceBaseInfoAllocationClaimView struct {
	ServiceID          string                      `json:"serviceId"`
	ResourceInstanceID string                      `json:"resourceInstanceId"`
	Dimensions         []resourceBaseInfoFieldView `json:"dimensions"`
}

type resourceBaseInfoFieldView struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type resourceBaseInfoMeasureView struct {
	Name        string   `json:"name"`
	Key         string   `json:"key"`
	Value       *float64 `json:"value"`
	Used        *float64 `json:"used"`
	Total       *float64 `json:"total"`
	Free        *float64 `json:"free"`
	Allocatable *float64 `json:"allocatable"`
	Capacity    *float64 `json:"capacity"`
	Available   *float64 `json:"available"`
}

func (u *ResourceUseCase) GetResourceGPUUsage(ctx context.Context, req GetResourceGPUUsageRequest) (ResourceGPUUsageResult, error) {
	if err := validateResourceQueryActor(req.Actor); err != nil {
		return ResourceGPUUsageResult{}, err
	}
	if u == nil || u.DB == nil {
		return ResourceGPUUsageResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.ResourceID == 0 {
		return ResourceGPUUsageResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "resource_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return ResourceGPUUsageResult{}, err
	}
	resource, err := u.loadResource(ctx, req.WorkspaceID, req.ResourceID)
	if err != nil {
		return ResourceGPUUsageResult{}, err
	}
	return buildResourceGPUUsageResult(*resource, req.IncludeAllocations), nil
}

func buildResourceGPUUsageResult(resource models.Resource, includeAllocations bool) ResourceGPUUsageResult {
	baseInfo := parseResourceBaseInfoV3GPUView(resource.BaseInfo)
	result := ResourceGPUUsageResult{
		ResourceID:     resource.ID,
		WorkspaceID:    resource.WorkspaceID,
		ResourceName:   sanitizeResourceText(resource.Name),
		ResourceType:   resource.Type,
		ResourceStatus: resource.Status,
		SchemaVersion:  sanitizeResourceText(schemaVersionText(baseInfo.SchemaVersion)),
		Freshness: ResourceGPUFreshness{
			Status:          resourceBaseInfoFreshnessStatus(resource.BaseInfoStatus),
			Source:          sanitizeResourceText(firstNonEmptyResourceGPUText(resource.BaseInfoSource, baseInfo.Source)),
			CollectedAtUnix: resource.BaseInfoCollectedAt,
			LastError:       sanitizeResourceText(resource.BaseInfoLastError),
		},
		GPUs: []ResourceGPUDetail{},
	}
	result.Freshness.CollectedAtText = resourceGPUCollectedAtText(resource.BaseInfoCollectedAt, baseInfo.CollectedAt)

	gpuByRawID := make(map[string]ResourceGPUDetail)
	gpuItems := make([]resourceGPUDetailWithRawID, 0)
	usedByRawID := make(map[string]bool)
	for _, instance := range baseInfo.ResourceInstances {
		if !strings.EqualFold(strings.TrimSpace(instance.ResourceTypeID), "gpu") {
			continue
		}
		detail := buildResourceGPUDetail(instance, result.Freshness.Source)
		gpuItems = append(gpuItems, resourceGPUDetailWithRawID{rawID: instance.ID, detail: detail})
		if strings.TrimSpace(instance.ID) != "" {
			gpuByRawID[instance.ID] = detail
		}
	}
	sort.SliceStable(gpuItems, func(i, j int) bool {
		left, right := gpuItems[i].detail.Index, gpuItems[j].detail.Index
		if left != nil && right != nil {
			return *left < *right
		}
		return gpuItems[i].detail.UUID < gpuItems[j].detail.UUID
	})
	serviceByRawID := make(map[string]resourceBaseInfoServiceView)
	for _, service := range baseInfo.Services {
		if strings.TrimSpace(service.ID) != "" {
			serviceByRawID[service.ID] = service
		}
	}
	allocationBuild := buildResourceGPUAllocations(baseInfo.Allocations, gpuByRawID, serviceByRawID, usedByRawID)
	for _, item := range gpuItems {
		if services := allocationBuild.servicesByGPURawID[item.rawID]; len(services) > 0 {
			item.detail.Services = services
			occupancy := allocationBuild.occupancyByGPURawID[item.rawID]
			item.detail.Occupancy = &occupancy
			item.detail.Status = "used"
		}
		result.GPUs = append(result.GPUs, item.detail)
	}
	if includeAllocations {
		result.Allocations = allocationBuild.allocations
	}
	result.Summary = summarizeResourceGPUs(gpuItems, usedByRawID)
	if result.Summary.TotalGPUs == 0 {
		result.AllocationStatus = "no_gpu_detected"
	} else if len(allocationBuild.allocations) == 0 {
		result.AllocationStatus = "no_allocations_reported"
	}
	return result
}

func buildResourceGPUDetail(instance resourceBaseInfoResourceInstanceView, metricsSource string) ResourceGPUDetail {
	identity := resourceBaseInfoFieldMap(instance.Identity)
	spec := resourceBaseInfoFieldMap(instance.Spec)
	capacity := resourceBaseInfoMeasureMap(instance.Capacity)
	metrics := resourceBaseInfoMeasureMap(instance.Metrics)
	index := optionalIntFromAny(identity["index"])
	memoryBytes := int64FromFloat(resourceBaseInfoMeasureNumber(capacity["memoryBytes"]))
	usedBytes := int64FromFloat(resourceBaseInfoMeasureNumber(metrics["memoryBytesUsed"]))
	availableBytes := int64FromFloat(resourceBaseInfoMeasureAvailableNumber(capacity["memoryBytesAvailable"]))
	freeBytes := availableBytes
	if freeBytes == 0 && memoryBytes > 0 {
		freeBytes = memoryBytes - usedBytes
		if freeBytes < 0 {
			freeBytes = 0
		}
	}
	status := "free"
	if usedBytes > 0 {
		status = "used"
	}
	return ResourceGPUDetail{
		Index:                 index,
		UUID:                  sanitizeResourceText(stringFromAny(identity["uuid"])),
		BusID:                 sanitizeResourceText(stringFromAny(identity["busId"])),
		Name:                  sanitizeResourceText(stringFromAny(identity["name"])),
		NodeID:                sanitizeResourceText(firstNonEmptyResourceGPUText(instance.NodeID, instance.EntityID)),
		Vendor:                sanitizeResourceText(stringFromAny(spec["vendor"])),
		Model:                 sanitizeResourceText(stringFromAny(spec["model"])),
		MemoryBytes:           memoryBytes,
		MemoryHuman:           formatResourceGPUBytes(memoryBytes),
		MemoryUsedBytes:       usedBytes,
		MemoryUsedHuman:       formatResourceGPUBytes(usedBytes),
		MemoryUsedPercent:     resourceGPUPercentPtr(usedBytes, memoryBytes),
		MemoryAvailableBytes:  availableBytes,
		MemoryAvailableHuman:  formatResourceGPUBytes(availableBytes),
		MemoryFreeBytes:       freeBytes,
		MemoryFreeHuman:       formatResourceGPUBytes(freeBytes),
		MemoryFreePercent:     resourceGPUPercentPtr(freeBytes, memoryBytes),
		UtilizationGPUPercent: resourceBaseInfoMeasureNumberPtr(metrics["utilizationGpuPercent"]),
		TemperatureGPUCelsius: resourceBaseInfoMeasureNumberPtr(metrics["temperatureGpuCelsius"]),
		Status:                status,
		MetricsSource:         sanitizeResourceText(metricsSource),
	}
}

func buildResourceGPUAllocations(allocations []resourceBaseInfoAllocationView, gpuByRawID map[string]ResourceGPUDetail, serviceByRawID map[string]resourceBaseInfoServiceView, usedByRawID map[string]bool) resourceGPUAllocationBuildResult {
	result := resourceGPUAllocationBuildResult{
		allocations:         []ResourceGPUAllocation{},
		servicesByGPURawID:  map[string][]ResourceGPUServiceUsage{},
		occupancyByGPURawID: map[string]ResourceGPUOccupancy{},
	}
	serviceAccumulators := map[string]map[string]*resourceGPUServiceUsageAccumulator{}
	for _, allocation := range allocations {
		for _, claim := range allocation.Claims {
			gpu, ok := gpuByRawID[claim.ResourceInstanceID]
			if !ok {
				continue
			}
			usedByRawID[claim.ResourceInstanceID] = true
			service := serviceByRawID[claim.ServiceID]
			serviceFields := resourceBaseInfoFieldMap(service.Fields)
			dimensions := resourceBaseInfoFieldMap(claim.Dimensions)
			pid := optionalIntFromAny(firstNonNilResourceGPUValue(dimensions["pid"], serviceFields["pid"]))
			observedPID := optionalIntFromAny(dimensions["observedPid"])
			memoryUsed := int64FromAny(firstNonNilResourceGPUValue(dimensions["memoryUsedBytes"], dimensions["memoryBytesUsed"]))
			item := ResourceGPUAllocation{
				GPUIndex:        gpu.Index,
				GPUUUID:         gpu.UUID,
				GPUBusID:        gpu.BusID,
				ServiceName:     sanitizeResourceText(service.Name),
				ServiceType:     sanitizeResourceText(stringFromAny(firstNonNilResourceGPUValue(serviceFields["runtime"], serviceFields["serviceType"]))),
				PID:             pid,
				Runtime:         sanitizeResourceText(stringFromAny(serviceFields["runtime"])),
				ContainerID:     sanitizeResourceText(stringFromAny(serviceFields["containerId"])),
				ContainerName:   sanitizeResourceText(stringFromAny(serviceFields["containerName"])),
				PodUID:          sanitizeResourceText(stringFromAny(firstNonNilResourceGPUValue(dimensions["podUid"], serviceFields["uid"]))),
				PodNamespace:    sanitizeResourceText(stringFromAny(serviceFields["namespace"])),
				PodName:         sanitizeResourceText(service.Name),
				NodeName:        sanitizeResourceText(stringFromAny(serviceFields["nodeName"])),
				OwnerKind:       sanitizeResourceText(stringFromAny(serviceFields["ownerKind"])),
				OwnerName:       sanitizeResourceText(stringFromAny(serviceFields["ownerName"])),
				MemoryUsedBytes: memoryUsed,
				MemoryUsedHuman: formatResourceGPUBytes(memoryUsed),
			}
			result.allocations = append(result.allocations, item)
			accumulateResourceGPUServiceUsage(serviceAccumulators, claim.ResourceInstanceID, claim.ServiceID, service, serviceFields, dimensions, pid, observedPID, memoryUsed, gpu.MemoryBytes)
		}
	}
	sort.SliceStable(result.allocations, func(i, j int) bool {
		if result.allocations[i].GPUIndex != nil && result.allocations[j].GPUIndex != nil && *result.allocations[i].GPUIndex != *result.allocations[j].GPUIndex {
			return *result.allocations[i].GPUIndex < *result.allocations[j].GPUIndex
		}
		return result.allocations[i].ServiceName < result.allocations[j].ServiceName
	})
	for gpuRawID, byServiceID := range serviceAccumulators {
		serviceIDs := make([]string, 0, len(byServiceID))
		for serviceID := range byServiceID {
			serviceIDs = append(serviceIDs, serviceID)
		}
		sort.Strings(serviceIDs)
		services := make([]ResourceGPUServiceUsage, 0, len(serviceIDs))
		occupancy := ResourceGPUOccupancy{ServiceCount: len(serviceIDs)}
		for _, serviceID := range serviceIDs {
			accumulator := byServiceID[serviceID]
			accumulator.usage.PIDs = sortedResourceGPUInts(accumulator.pids)
			accumulator.usage.ObservedPIDs = sortedResourceGPUInts(accumulator.observedPIDs)
			accumulator.usage.ProcessCount = len(accumulator.usage.Processes)
			accumulator.usage.MemoryUsedHuman = formatResourceGPUBytes(accumulator.usage.MemoryUsedBytes)
			accumulator.usage.GPUMemoryPercent = resourceGPUPercentPtr(accumulator.usage.MemoryUsedBytes, gpuByRawID[gpuRawID].MemoryBytes)
			services = append(services, accumulator.usage)
			occupancy.ClaimCount += accumulator.claimCount
			occupancy.MemoryUsedBytes += accumulator.usage.MemoryUsedBytes
		}
		for index := range services {
			services[index].OccupancyMemoryPercent = resourceGPUPercentPtr(services[index].MemoryUsedBytes, occupancy.MemoryUsedBytes)
		}
		sort.SliceStable(services, func(i, j int) bool {
			return services[i].DisplayName < services[j].DisplayName
		})
		occupancy.MemoryUsedHuman = formatResourceGPUBytes(occupancy.MemoryUsedBytes)
		occupancy.MemoryUsedPercent = resourceGPUPercentPtr(occupancy.MemoryUsedBytes, gpuByRawID[gpuRawID].MemoryBytes)
		result.servicesByGPURawID[gpuRawID] = services
		result.occupancyByGPURawID[gpuRawID] = occupancy
	}
	return result
}

func accumulateResourceGPUServiceUsage(accumulators map[string]map[string]*resourceGPUServiceUsageAccumulator, gpuRawID, serviceID string, service resourceBaseInfoServiceView, serviceFields map[string]any, dimensions map[string]any, pid *int, observedPID *int, memoryUsed int64, gpuMemoryBytes int64) {
	if accumulators[gpuRawID] == nil {
		accumulators[gpuRawID] = map[string]*resourceGPUServiceUsageAccumulator{}
	}
	accumulator := accumulators[gpuRawID][serviceID]
	if accumulator == nil {
		containerName := sanitizeResourceText(stringFromAny(serviceFields["containerName"]))
		serviceName := sanitizeResourceText(service.Name)
		podUID := sanitizeResourceText(stringFromAny(firstNonNilResourceGPUValue(dimensions["podUid"], serviceFields["uid"])))
		ownerKind := sanitizeResourceText(stringFromAny(serviceFields["ownerKind"]))
		ownerName := sanitizeResourceText(stringFromAny(serviceFields["ownerName"]))
		ownerDisplayName := ownerName
		if ownerKind != "" && ownerName != "" {
			ownerDisplayName = ownerKind + "/" + ownerName
		} else if ownerDisplayName == "" {
			ownerDisplayName = ownerKind
		}
		accumulator = &resourceGPUServiceUsageAccumulator{
			usage: ResourceGPUServiceUsage{
				DisplayName:      firstNonEmptyResourceGPUText(containerName, serviceName, podUID),
				ServiceName:      serviceName,
				ServiceType:      sanitizeResourceText(stringFromAny(firstNonNilResourceGPUValue(serviceFields["runtime"], serviceFields["serviceType"]))),
				Runtime:          sanitizeResourceText(stringFromAny(serviceFields["runtime"])),
				PID:              optionalIntFromAny(serviceFields["pid"]),
				ContainerID:      sanitizeResourceText(stringFromAny(serviceFields["containerId"])),
				ContainerName:    containerName,
				PodUID:           podUID,
				PodNamespace:     sanitizeResourceText(stringFromAny(serviceFields["namespace"])),
				PodName:          serviceName,
				NodeName:         sanitizeResourceText(stringFromAny(serviceFields["nodeName"])),
				Phase:            sanitizeResourceText(stringFromAny(serviceFields["phase"])),
				OwnerKind:        ownerKind,
				OwnerName:        ownerName,
				OwnerDisplayName: ownerDisplayName,
				Processes:        []ResourceGPUProcessUsage{},
			},
			pids:         map[int]struct{}{},
			observedPIDs: map[int]struct{}{},
		}
		accumulators[gpuRawID][serviceID] = accumulator
	}
	accumulator.claimCount++
	if pid != nil {
		accumulator.pids[*pid] = struct{}{}
		if accumulator.usage.PID == nil {
			accumulator.usage.PID = pid
		}
	}
	if observedPID != nil {
		accumulator.observedPIDs[*observedPID] = struct{}{}
	}
	process := ResourceGPUProcessUsage{
		PID:              pid,
		ObservedPID:      observedPID,
		MemoryUsedBytes:  memoryUsed,
		MemoryUsedHuman:  formatResourceGPUBytes(memoryUsed),
		GPUMemoryPercent: resourceGPUPercentPtr(memoryUsed, gpuMemoryBytes),
	}
	if pid != nil && observedPID != nil && *pid != *observedPID {
		process.IsDescendant = true
		accumulator.usage.DescendantProcessCount++
	}
	accumulator.usage.Processes = append(accumulator.usage.Processes, process)
	accumulator.usage.MemoryUsedBytes += memoryUsed
}

func summarizeResourceGPUs(gpus []resourceGPUDetailWithRawID, usedByRawID map[string]bool) ResourceGPUSummary {
	summary := ResourceGPUSummary{TotalGPUs: len(gpus)}
	utilizationCount := 0
	var utilizationTotal float64
	for _, item := range gpus {
		gpu := item.detail
		used := gpu.MemoryUsedBytes > 0
		if !used {
			used = usedByRawID[item.rawID]
		}
		if used {
			summary.UsedGPUs++
		}
		summary.TotalMemoryBytes += gpu.MemoryBytes
		summary.UsedMemoryBytes += gpu.MemoryUsedBytes
		if gpu.MemoryFreeBytes > 0 {
			summary.FreeMemoryBytes += gpu.MemoryFreeBytes
		}
		if gpu.UtilizationGPUPercent != nil {
			utilizationTotal += *gpu.UtilizationGPUPercent
			utilizationCount++
		}
	}
	summary.FreeGPUs = summary.TotalGPUs - summary.UsedGPUs
	if summary.FreeGPUs < 0 {
		summary.FreeGPUs = 0
	}
	if summary.FreeMemoryBytes == 0 && summary.TotalMemoryBytes > 0 {
		summary.FreeMemoryBytes = summary.TotalMemoryBytes - summary.UsedMemoryBytes
	}
	summary.TotalMemoryHuman = formatResourceGPUBytes(summary.TotalMemoryBytes)
	summary.UsedMemoryHuman = formatResourceGPUBytes(summary.UsedMemoryBytes)
	summary.FreeMemoryHuman = formatResourceGPUBytes(summary.FreeMemoryBytes)
	summary.MemoryUsedPercent = resourceGPUPercentPtr(summary.UsedMemoryBytes, summary.TotalMemoryBytes)
	if utilizationCount > 0 {
		avg := utilizationTotal / float64(utilizationCount)
		summary.AverageUtilizationGPUPercent = &avg
	}
	return summary
}

func parseResourceBaseInfoV3GPUView(raw string) resourceBaseInfoV3GPUView {
	var baseInfo resourceBaseInfoV3GPUView
	if strings.TrimSpace(raw) == "" {
		return baseInfo
	}
	_ = json.Unmarshal([]byte(raw), &baseInfo)
	return baseInfo
}

func resourceBaseInfoFieldMap(fields []resourceBaseInfoFieldView) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(firstNonEmptyResourceGPUText(field.Name, field.Key))
		if name == "" {
			continue
		}
		result[name] = field.Value
	}
	return result
}

func resourceBaseInfoMeasureMap(measures []resourceBaseInfoMeasureView) map[string]resourceBaseInfoMeasureView {
	result := make(map[string]resourceBaseInfoMeasureView, len(measures))
	for _, measure := range measures {
		name := strings.TrimSpace(firstNonEmptyResourceGPUText(measure.Name, measure.Key))
		if name == "" {
			continue
		}
		result[name] = measure
	}
	return result
}

func resourceBaseInfoMeasureNumber(measure resourceBaseInfoMeasureView) float64 {
	for _, value := range []*float64{measure.Value, measure.Used, measure.Total, measure.Free, measure.Allocatable, measure.Capacity, measure.Available} {
		if value != nil {
			return *value
		}
	}
	return 0
}

func resourceBaseInfoMeasureAvailableNumber(measure resourceBaseInfoMeasureView) float64 {
	for _, value := range []*float64{measure.Available, measure.Free, measure.Value, measure.Capacity, measure.Total} {
		if value != nil {
			return *value
		}
	}
	return 0
}

func resourceBaseInfoMeasureNumberPtr(measure resourceBaseInfoMeasureView) *float64 {
	value := resourceBaseInfoMeasureNumber(measure)
	if value == 0 && measure.Value == nil && measure.Used == nil && measure.Total == nil && measure.Free == nil && measure.Allocatable == nil && measure.Capacity == nil && measure.Available == nil {
		return nil
	}
	return &value
}

func resourceGPUPercentPtr(numerator int64, denominator int64) *float64 {
	if numerator < 0 || denominator <= 0 {
		return nil
	}
	value := (float64(numerator) / float64(denominator)) * 100
	value = math.Round(value*100) / 100
	return &value
}

func resourceBaseInfoFreshnessStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "unknown"
	}
	return sanitizeResourceText(trimmed)
}

func resourceGPUCollectedAtText(unixSeconds int64, collectedAt string) string {
	if unixSeconds > 0 {
		return time.Unix(unixSeconds, 0).UTC().Format(time.RFC3339)
	}
	return sanitizeResourceText(strings.TrimSpace(collectedAt))
}

func schemaVersionText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == math.Trunc(typed) {
			return fmt.Sprintf("%.0f", typed)
		}
		return fmt.Sprintf("%g", typed)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func firstNonEmptyResourceGPUText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonNilResourceGPUValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func optionalIntFromAny(value any) *int {
	parsed := int64FromAny(value)
	if parsed == 0 && strings.TrimSpace(stringFromAny(value)) == "" {
		return nil
	}
	item := int(parsed)
	return &item
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed float64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return int64(parsed)
		}
	}
	return 0
}

func int64FromFloat(value float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value)
}

func sortedResourceGPUInts(items map[int]struct{}) []int {
	if len(items) == 0 {
		return nil
	}
	result := make([]int, 0, len(items))
	for item := range items {
		result = append(result, item)
	}
	sort.Ints(result)
	return result
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return strings.Trim(fmt.Sprint(typed), `"`)
	}
}

func formatResourceGPUBytes(value int64) string {
	if value <= 0 {
		return ""
	}
	const unit = 1024
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	size := float64(value)
	unitIndex := 0
	for size >= unit && unitIndex < len(units)-1 {
		size /= unit
		unitIndex++
	}
	if unitIndex == 0 {
		return fmt.Sprintf("%d %s", value, units[unitIndex])
	}
	return fmt.Sprintf("%.2f %s", size, units[unitIndex])
}
