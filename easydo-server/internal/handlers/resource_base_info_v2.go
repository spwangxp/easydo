package handlers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ResourceBaseInfoV3 struct {
	SchemaVersion     int                            `json:"schemaVersion"`
	ResourceID        string                         `json:"resourceId"`
	Status            string                         `json:"status"`
	Source            string                         `json:"source"`
	CollectedAt       string                         `json:"collectedAt"`
	Labels            map[string]interface{}         `json:"labels,omitempty"`
	Entities          []ResourceBaseInfoEntity       `json:"entities,omitempty"`
	ResourceTypes     []ResourceBaseInfoResourceType `json:"resourceTypes,omitempty"`
	ResourceInstances []ResourceBaseInfoResourceInstance `json:"resourceInstances,omitempty"`
	Services          []ResourceBaseInfoService      `json:"services,omitempty"`
	Allocations       []ResourceBaseInfoAllocation   `json:"allocations,omitempty"`
}

type ResourceBaseInfoV2 struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Status        string                      `json:"status"`
	Source        string                      `json:"source"`
	CollectedAt   int64                       `json:"collectedAt"`
	Summary       *ResourceBaseInfoSummary     `json:"summary,omitempty"`
	Labels        map[string]interface{}      `json:"labels,omitempty"`
	Machine       *ResourceBaseInfoMachine     `json:"machine,omitempty"`
	VM            *ResourceBaseInfoVMSection   `json:"vm,omitempty"`
	K8s           *ResourceBaseInfoK8sSection  `json:"k8s,omitempty"`
}

type ResourceBaseInfoSummary struct {
	NodeCount              int   `json:"nodeCount,omitempty"`
	GPUCount               int   `json:"gpuCount,omitempty"`
	CarrierCount           int   `json:"carrierCount,omitempty"`
	CPUCapacityMilli       int64 `json:"cpuCapacityMilli,omitempty"`
	CPUAllocatableMilli    int64 `json:"cpuAllocatableMilli,omitempty"`
	MemoryCapacityBytes    int64 `json:"memoryCapacityBytes,omitempty"`
	MemoryAllocatableBytes int64 `json:"memoryAllocatableBytes,omitempty"`
	PodAllocatable         int64 `json:"podAllocatable,omitempty"`
	GPUAllocatable         int64 `json:"gpuAllocatable,omitempty"`
}

type ResourceBaseInfoMachine struct {
	Hostname      string                  `json:"hostname,omitempty"`
	PrimaryIPv4   string                  `json:"primaryIpv4,omitempty"`
	OS            ResourceBaseInfoOS      `json:"os,omitempty"`
	KernelVersion string                  `json:"kernelVersion,omitempty"`
	Arch          string                  `json:"arch,omitempty"`
	CPU           *ResourceBaseInfoCPU    `json:"cpu,omitempty"`
	Memory        *ResourceBaseInfoMemory `json:"memory,omitempty"`
	Storage       *ResourceBaseInfoStorage `json:"storage,omitempty"`
	GPU           *ResourceBaseInfoGPUSet  `json:"gpu,omitempty"`
}

type ResourceBaseInfoOS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type ResourceBaseInfoCPU struct {
	Model        string `json:"model,omitempty"`
	LogicalCores int    `json:"logicalCores,omitempty"`
}

type ResourceBaseInfoMemory struct {
	TotalBytes int64 `json:"totalBytes,omitempty"`
}

type ResourceBaseInfoStorage struct {
	RootTotalBytes int64                    `json:"rootTotalBytes,omitempty"`
	TotalDiskBytes int64                    `json:"totalDiskBytes,omitempty"`
	Disks          []map[string]interface{} `json:"disks,omitempty"`
}

type ResourceBaseInfoGPU struct {
	ID          string `json:"id,omitempty"`
	Index       int    `json:"index,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Model       string `json:"model,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	BusID       string `json:"busId,omitempty"`
	MemoryBytes int64  `json:"memoryBytes,omitempty"`
}

type ResourceBaseInfoGPUSet struct {
	Count   int                  `json:"count,omitempty"`
	Devices []ResourceBaseInfoGPU `json:"devices,omitempty"`
}

type ResourceBaseInfoCarrier struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Model string `json:"model,omitempty"`
}

type ResourceBaseInfoMatrixSegment struct {
	Lane       int `json:"lane,omitempty"`
	StartIndex int `json:"startIndex"`
	EndIndex   int `json:"endIndex"`
}

type ResourceBaseInfoMatrixRow struct {
	ID       string                         `json:"id,omitempty"`
	Lane     int                            `json:"lane"`
	Segments []ResourceBaseInfoMatrixSegment `json:"segments,omitempty"`
}

type ResourceBaseInfoNode struct {
	ID             string   `json:"id,omitempty"`
	Name           string   `json:"name,omitempty"`
	Roles          []string `json:"roles,omitempty"`
	Arch           string   `json:"arch,omitempty"`
	OSImage        string   `json:"osImage,omitempty"`
	KubeletVersion string   `json:"kubeletVersion,omitempty"`
}

type ResourceBaseInfoVMSection struct {
	Count    int                         `json:"count,omitempty"`
	Machine  *ResourceBaseInfoMachine    `json:"machine,omitempty"`
	Summary  *ResourceBaseInfoSummary    `json:"summary,omitempty"`
	Nodes    []ResourceBaseInfoNode      `json:"nodes,omitempty"`
	GPUs     []ResourceBaseInfoGPU       `json:"gpus,omitempty"`
	Carriers []ResourceBaseInfoCarrier   `json:"carriers,omitempty"`
	Matrix   []ResourceBaseInfoMatrixRow `json:"matrix,omitempty"`
	Labels   map[string]interface{}      `json:"labels,omitempty"`
}

type ResourceBaseInfoK8sCluster struct {
	ServerVersion string `json:"serverVersion,omitempty"`
}

type ResourceBaseInfoK8sNode struct {
	ID                    string   `json:"id,omitempty"`
	Name                  string   `json:"name,omitempty"`
	Roles                 []string `json:"roles,omitempty"`
	Arch                  string   `json:"arch,omitempty"`
	OSImage               string   `json:"osImage,omitempty"`
	KubeletVersion        string   `json:"kubeletVersion,omitempty"`
	CPUAllocatableMilli   int64    `json:"cpuAllocatableMilli,omitempty"`
	MemoryAllocatableBytes int64   `json:"memoryAllocatableBytes,omitempty"`
	PodAllocatable        int64    `json:"podAllocatable,omitempty"`
	GPUAllocatable        int64    `json:"gpuAllocatable,omitempty"`
}

type ResourceBaseInfoK8sSection struct {
	Cluster *ResourceBaseInfoK8sCluster `json:"cluster,omitempty"`
	Summary *ResourceBaseInfoSummary     `json:"summary,omitempty"`
	Nodes   []ResourceBaseInfoK8sNode    `json:"nodes,omitempty"`
	GPUs    []ResourceBaseInfoGPU        `json:"gpus,omitempty"`
	Matrix  []ResourceBaseInfoMatrixRow   `json:"matrix,omitempty"`
	Labels  map[string]interface{}       `json:"labels,omitempty"`
}

type ResourceBaseInfoEntity struct {
	ID     string                 `json:"id"`
	Kind   string                 `json:"kind,omitempty"`
	Name   string                 `json:"name,omitempty"`
	Fields []ResourceBaseInfoField `json:"fields,omitempty"`
}

type ResourceBaseInfoResourceType struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type ResourceBaseInfoResourceInstance struct {
	ID             string                   `json:"id"`
	ResourceTypeID string                   `json:"resourceTypeId"`
	EntityID       string                   `json:"entityId,omitempty"`
	Identity       []ResourceBaseInfoField   `json:"identity,omitempty"`
	Spec           []ResourceBaseInfoField   `json:"spec,omitempty"`
	Capacity       []ResourceBaseInfoMeasure `json:"capacity,omitempty"`
	Metrics        []ResourceBaseInfoMeasure `json:"metrics,omitempty"`
}

type ResourceBaseInfoService struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name,omitempty"`
	EntityID            string                 `json:"entityId,omitempty"`
	ResourceInstanceIDs []string               `json:"resourceInstanceIds,omitempty"`
	Fields              []ResourceBaseInfoField `json:"fields,omitempty"`
}

type ResourceBaseInfoAllocation struct {
	ID     string                          `json:"id"`
	Claims []ResourceBaseInfoAllocationClaim `json:"claims,omitempty"`
}

type ResourceBaseInfoAllocationClaim struct {
	ServiceID         string                 `json:"serviceId"`
	ResourceInstanceID string                 `json:"resourceInstanceId"`
	Dimensions        []ResourceBaseInfoField `json:"dimensions,omitempty"`
}

type ResourceBaseInfoField struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value,omitempty"`
}

type ResourceBaseInfoMeasure struct {
	Name           string   `json:"name"`
	Value          *float64 `json:"value,omitempty"`
	Used           *float64 `json:"used,omitempty"`
	Total          *float64 `json:"total,omitempty"`
	Free           *float64 `json:"free,omitempty"`
	Allocatable    *float64 `json:"allocatable,omitempty"`
	Capacity       *float64 `json:"capacity,omitempty"`
	Available      *float64 `json:"available,omitempty"`
	SourceReported bool     `json:"sourceReported,omitempty"`
}

func (m ResourceBaseInfoMeasure) MarshalJSON() ([]byte, error) {
	if err := validateResourceBaseInfoMeasureShape(m); err != nil {
		return nil, err
	}
	type alias ResourceBaseInfoMeasure
	return json.Marshal(alias(m))
}

func buildResourceBaseInfoResourcePrefix(resourceID uint64) string {
	if resourceID == 0 {
		return "resource:unscoped"
	}
	return fmt.Sprintf("resource:%d", resourceID)
}

func normalizeResourceBaseInfoKeyPart(key, fallback string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		trimmed = fallback
	}
	escaped := url.PathEscape(trimmed)
	if strings.Contains(trimmed, ":") {
		escaped = strings.ReplaceAll(escaped, "%3A", ":")
	}
	return strings.ReplaceAll(escaped, ":", "%3A")
}

func buildResourceBaseInfoEntityID(resourceID uint64, key string) string {
	return fmt.Sprintf("%s:entity:%s", buildResourceBaseInfoResourcePrefix(resourceID), normalizeResourceBaseInfoKeyPart(key, "entity"))
}

func buildResourceBaseInfoResourceInstanceID(resourceID uint64, resourceTypeID, key string) string {
	return fmt.Sprintf("%s:resource-instance:%s:%s", buildResourceBaseInfoResourcePrefix(resourceID), normalizeResourceBaseInfoKeyPart(resourceTypeID, "resource"), normalizeResourceBaseInfoKeyPart(key, "instance"))
}

func buildResourceBaseInfoGPUID(resourceID uint64, key string) string {
	return buildResourceBaseInfoResourceInstanceID(resourceID, "gpu", key)
}

func buildResourceBaseInfoServiceID(resourceID uint64, key string) string {
	return fmt.Sprintf("%s:service:%s", buildResourceBaseInfoResourcePrefix(resourceID), normalizeResourceBaseInfoKeyPart(key, "service"))
}

func buildResourceBaseInfoAllocationID(resourceID uint64, serviceID, resourceInstanceID string) string {
	return fmt.Sprintf("%s:allocation:%s:%s", buildResourceBaseInfoResourcePrefix(resourceID), normalizeResourceBaseInfoKeyPart(serviceID, "service"), normalizeResourceBaseInfoKeyPart(resourceInstanceID, "resource-instance"))
}

func approvedResourceBaseInfoResourceTypeIDs() []string {
	return []string{"cpu", "memory", "gpu", "port", "storage", "network"}
}

func approvedResourceBaseInfoResourceTypes() []ResourceBaseInfoResourceType {
	return []ResourceBaseInfoResourceType{
		{ID: "cpu", Name: "CPU"},
		{ID: "memory", Name: "Memory"},
		{ID: "gpu", Name: "GPU"},
		{ID: "port", Name: "Port"},
		{ID: "storage", Name: "Storage"},
		{ID: "network", Name: "Network"},
	}
}

func newResourceBaseInfoField(name string, value interface{}) ResourceBaseInfoField {
	return ResourceBaseInfoField{Name: name, Value: value}
}

func newResourceBaseInfoMeasureValue(name string, value float64) ResourceBaseInfoMeasure {
	return ResourceBaseInfoMeasure{Name: name, Value: float64Ptr(value)}
}

func newResourceBaseInfoMeasureCapacity(name string, value float64) ResourceBaseInfoMeasure {
	return ResourceBaseInfoMeasure{Name: name, Capacity: float64Ptr(value)}
}

func newResourceBaseInfoMeasureAvailable(name string, value float64) ResourceBaseInfoMeasure {
	return ResourceBaseInfoMeasure{Name: name, Available: float64Ptr(value), SourceReported: true}
}

func validateResourceBaseInfoMeasureShape(m ResourceBaseInfoMeasure) error {
	setCount := 0
	for _, ptr := range []*float64{m.Value, m.Used, m.Total, m.Free, m.Allocatable, m.Capacity, m.Available} {
		if ptr != nil {
			setCount++
		}
	}
	if setCount > 1 {
		return fmt.Errorf("invalid measure shape for %q: only one of value/used/total/free/allocatable/capacity/available may be set", m.Name)
	}
	if m.Available != nil && !m.SourceReported {
		return fmt.Errorf("invalid measure shape for %q: available requires sourceReported", m.Name)
	}
	return nil
}

func mergeResourceRuntimeLabels(base map[string]interface{}, runtimeLabels map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(runtimeLabels) == 0 {
		return map[string]interface{}{}
	}
	merged := make(map[string]interface{}, len(base)+len(runtimeLabels))
	for key, value := range runtimeLabels {
		merged[key] = value
	}
	for key, value := range base {
		if current, ok := merged[key]; ok {
			currentMap, currentOK := current.(map[string]interface{})
			nextMap, nextOK := value.(map[string]interface{})
			if currentOK && nextOK {
				merged[key] = mergeResourceRuntimeLabels(nextMap, currentMap)
				continue
			}
		}
		merged[key] = value
	}
	return merged
}

func decodeResourceRuntimeLabels(raw string) map[string]interface{} {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]interface{}{}
	}
	var value map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return map[string]interface{}{}
	}
	return value
}

func isReservedResourceBaseInfoLabelKey(key string) bool {
	switch key {
	case "resourceId", "resourceType", "vm", "k8s", "machine", "nodes", "gpus", "carriers", "matrix", "axes", "cells":
		return true
	default:
		return false
	}
}

func loadMergedResourceRuntimeLabels(db *gorm.DB, workspaceID, resourceID uint64) map[string]interface{} {
	if db == nil || resourceID == 0 {
		return map[string]interface{}{}
	}
	var labels []models.ResourceRuntimeLabel
	if err := db.Where("workspace_id = ? AND resource_id = ? AND target_type = ?", workspaceID, resourceID, "resource").Order("created_at ASC, id ASC").Find(&labels).Error; err != nil {
		return map[string]interface{}{}
	}
	merged := map[string]interface{}{}
	for _, item := range labels {
		for key, value := range decodeResourceRuntimeLabels(item.LabelsJSON) {
			if isReservedResourceBaseInfoLabelKey(key) {
				continue
			}
			merged[key] = value
		}
	}
	return merged
}

func mergeResourceBaseInfoV2Labels(baseInfo ResourceBaseInfoV2, labels map[string]interface{}) ResourceBaseInfoV2 {
	if len(labels) == 0 {
		return baseInfo
	}
	baseInfo.Labels = mergeResourceRuntimeLabels(baseInfo.Labels, labels)
	return baseInfo
}

func mergeResourceBaseInfoV3Labels(baseInfo ResourceBaseInfoV3, labels map[string]interface{}) ResourceBaseInfoV3 {
	if len(labels) == 0 {
		return baseInfo
	}
	baseInfo.Labels = mergeResourceRuntimeLabels(baseInfo.Labels, labels)
	return baseInfo
}

func buildResourceBaseInfoResponse(resource models.Resource, db *gorm.DB) gin.H {
	resp := buildResourceResponse(resource)
	baseInfo := decodeJSONObjectField(resource.BaseInfo, map[string]interface{}{})
	baseMap, _ := baseInfo.(map[string]interface{})
	labels := loadMergedResourceRuntimeLabels(db, resource.WorkspaceID, resource.ID)
	if resourceBaseInfoParseIntValue(fmt.Sprint(baseMap["schemaVersion"])) >= 3 {
		var typed ResourceBaseInfoV3
		if err := json.Unmarshal([]byte(resource.BaseInfo), &typed); err == nil {
			typed = sanitizeResourceBaseInfoV3(typed)
			typed.Labels = mergeResourceRuntimeLabels(typed.Labels, labels)
			resp["base_info"] = typed
			return resp
		}
	}
	if len(labels) == 0 {
		resp["base_info"] = baseInfo
		return resp
	}
	merged := map[string]interface{}{}
	for key, value := range baseMap {
		merged[key] = value
	}
	existingLabels, _ := merged["labels"].(map[string]interface{})
	merged["labels"] = mergeResourceRuntimeLabels(existingLabels, labels)
	resp["base_info"] = merged
	return resp
}

func sanitizeResourceBaseInfoV3(baseInfo ResourceBaseInfoV3) ResourceBaseInfoV3 {
	for i := range baseInfo.ResourceInstances {
		baseInfo.ResourceInstances[i].Capacity = sanitizeResourceBaseInfoMeasures(baseInfo.ResourceInstances[i].Capacity)
		baseInfo.ResourceInstances[i].Metrics = sanitizeResourceBaseInfoMeasures(baseInfo.ResourceInstances[i].Metrics)
	}
	return baseInfo
}

func sanitizeResourceBaseInfoMeasures(measures []ResourceBaseInfoMeasure) []ResourceBaseInfoMeasure {
	if len(measures) == 0 {
		return measures
	}
	filtered := make([]ResourceBaseInfoMeasure, 0, len(measures))
	for _, measure := range measures {
		if err := validateResourceBaseInfoMeasureShape(measure); err != nil {
			continue
		}
		filtered = append(filtered, measure)
	}
	return filtered
}

func buildResourceBaseInfoV3(resourceID uint64, source string) ResourceBaseInfoV3 {
	return ResourceBaseInfoV3{
		SchemaVersion: 3,
		ResourceID:    buildResourceBaseInfoResourcePrefix(resourceID),
		Status:        "success",
		Source:        resourceBaseInfoDefaultIfEmpty(source, "remote_task"),
		CollectedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Labels:        map[string]interface{}{},
	}
}

func buildResourceBaseInfoLegacyV2(resourceID uint64, source string, collectedAt int64, labels map[string]interface{}, machine *ResourceBaseInfoMachine, summary *ResourceBaseInfoSummary, vm *ResourceBaseInfoVMSection, k8s *ResourceBaseInfoK8sSection) ResourceBaseInfoV2 {
	_ = resourceID
	return ResourceBaseInfoV2{
		SchemaVersion: 2,
		Status:        "success",
		Source:        resourceBaseInfoDefaultIfEmpty(source, "remote_task"),
		CollectedAt:   collectedAt,
		Summary:       summary,
		Labels:        labels,
		Machine:       machine,
		VM:            vm,
		K8s:           k8s,
	}
}

func buildResourceBaseInfoV2FromVM(resourceID uint64, stdout, source string) (ResourceBaseInfoV2, int64, error) {
	sections := resourceBaseInfoParseMarkedCollectorOutput(stdout)
	if !sections.hasMainBlock {
		return ResourceBaseInfoV2{}, 0, fmt.Errorf("基础资源采集结果缺少主标记")
	}
	collectedAtTime := time.Now().UTC()
	collectedAt := collectedAtTime.Unix()
	machine := &ResourceBaseInfoMachine{
		Hostname:      sections.scalars["EASYDO_HOSTNAME"],
		PrimaryIPv4:   sections.scalars["EASYDO_PRIMARY_IPV4"],
		OS:            ResourceBaseInfoOS{Name: sections.scalars["EASYDO_OS_NAME"], Version: sections.scalars["EASYDO_OS_VERSION"]},
		KernelVersion: sections.scalars["EASYDO_KERNEL_VERSION"],
		Arch:          sections.scalars["EASYDO_ARCH"],
		CPU:           &ResourceBaseInfoCPU{Model: sections.scalars["EASYDO_CPU_MODEL"], LogicalCores: resourceBaseInfoParseIntValue(sections.scalars["EASYDO_CPU_LOGICAL_CORES"])},
		Memory:        &ResourceBaseInfoMemory{TotalBytes: resourceBaseInfoParseInt64Value(sections.scalars["EASYDO_MEMORY_TOTAL_BYTES"])},
	}
	labels := map[string]interface{}{
		"resourceId":   buildResourceBaseInfoResourcePrefix(resourceID),
		"resourceType": "vm",
		"hostname":     machine.Hostname,
		"primaryIpv4":  machine.PrimaryIPv4,
		"arch":         machine.Arch,
		"osName":       machine.OS.Name,
		"osVersion":    machine.OS.Version,
	}
	for key, value := range decodeResourceRuntimeLabels(sections.scalars["EASYDO_RESOURCE_LABELS_JSON"]) {
		if key == "resourceId" || key == "resourceType" {
			continue
		}
		labels[key] = value
	}
	gpuDevices := make([]ResourceBaseInfoGPU, 0, len(sections.gpuRows))
	for _, device := range resourceBaseInfoParseVMBaseInfoGPUDevices(sections.gpuRows) {
		index := resourceBaseInfoParseIntValue(fmt.Sprint(device["index"]))
		gpu := ResourceBaseInfoGPU{ID: buildResourceBaseInfoGPUID(resourceID, fmt.Sprintf("%d", index)), Index: index, Model: resourceBaseInfoStringValue(device["model"]), UUID: resourceBaseInfoStringValue(device["uuid"]), BusID: resourceBaseInfoStringValue(device["busId"]), Vendor: resourceBaseInfoStringValue(device["vendor"]), MemoryBytes: resourceBaseInfoParseInt64Value(fmt.Sprint(device["memoryBytes"]))}
		gpuDevices = append(gpuDevices, gpu)
	}
	legacySummary := &ResourceBaseInfoSummary{GPUCount: len(gpuDevices)}
	legacyVM := &ResourceBaseInfoVMSection{Count: len(gpuDevices), Machine: machine, Summary: legacySummary, GPUs: gpuDevices, Labels: labels}
	return buildResourceBaseInfoLegacyV2(resourceID, source, collectedAt, labels, machine, legacySummary, legacyVM, nil), collectedAt, nil
}

func buildResourceBaseInfoV2FromK8s(resourceID uint64, stdout, source string) (ResourceBaseInfoV2, int64, error) {
	versionRaw := resourceBaseInfoExtractMarkedSection(stdout, "EASYDO_K8S_VERSION_BEGIN", "EASYDO_K8S_VERSION_END")
	nodesRaw := resourceBaseInfoExtractMarkedSection(stdout, "EASYDO_K8S_NODES_BEGIN", "EASYDO_K8S_NODES_END")
	if versionRaw == "" || nodesRaw == "" {
		return ResourceBaseInfoV2{}, 0, fmt.Errorf("K8s 采集结果缺少必要数据")
	}
	var versionDoc map[string]interface{}
	if err := json.Unmarshal([]byte(versionRaw), &versionDoc); err != nil {
		return ResourceBaseInfoV2{}, 0, fmt.Errorf("K8s version 数据无效")
	}
	var nodesDoc map[string]interface{}
	if err := json.Unmarshal([]byte(nodesRaw), &nodesDoc); err != nil {
		return ResourceBaseInfoV2{}, 0, fmt.Errorf("K8s nodes 数据无效")
	}
	items, _ := nodesDoc["items"].([]interface{})
	collectedAtTime := time.Now().UTC()
	nodeCount := 0
	for _, item := range items {
		node, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		metadata, _ := node["metadata"].(map[string]interface{})
		if resourceBaseInfoStringValue(metadata["name"]) != "" {
			nodeCount++
		}
	}
	k8sSection := &ResourceBaseInfoK8sSection{Cluster: &ResourceBaseInfoK8sCluster{ServerVersion: resourceBaseInfoStringValue(versionDoc["serverVersion"])}, Labels: map[string]interface{}{"resourceId": buildResourceBaseInfoResourcePrefix(resourceID), "resourceType": "k8s"}}
	return buildResourceBaseInfoLegacyV2(resourceID, source, collectedAtTime.Unix(), map[string]interface{}{"resourceId": buildResourceBaseInfoResourcePrefix(resourceID), "resourceType": "k8s"}, nil, &ResourceBaseInfoSummary{NodeCount: nodeCount}, nil, k8sSection), collectedAtTime.Unix(), nil
}

func float64Ptr(v float64) *float64 { return &v }

func resourceBaseInfoDefaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func resourceBaseInfoStringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func resourceBaseInfoParseIntValue(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}

func resourceBaseInfoParseInt64Value(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if value, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return int64(value)
	}
	return 0
}

func resourceBaseInfoParseMarkedCollectorOutput(stdout string) struct {
	hasMainBlock bool
	scalars      map[string]string
	gpuRows      []string
	diskRows     []string
} {
	sections := struct {
		hasMainBlock bool
		scalars      map[string]string
		gpuRows      []string
		diskRows     []string
	}{scalars: map[string]string{}}
	lines := strings.Split(stdout, "\n")
	block := ""
	collecting := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		switch line {
		case "EASYDO_BASE_INFO_BEGIN":
			sections.hasMainBlock = true
			collecting = true
			block = "main"
			continue
		case "EASYDO_BASE_INFO_END":
			collecting = false
			block = ""
			continue
		case "EASYDO_DISK_ROWS_BEGIN":
			block = "disk"
			continue
		case "EASYDO_DISK_ROWS_END":
			block = ""
			continue
		case "EASYDO_GPU_CSV_BEGIN":
			block = "gpu"
			continue
		case "EASYDO_GPU_CSV_END":
			block = ""
			continue
		}
		if !collecting {
			continue
		}
		if block == "main" {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				sections.scalars[parts[0]] = parts[1]
			}
			continue
		}
		if block == "gpu" {
			sections.gpuRows = append(sections.gpuRows, line)
			continue
		}
		if block == "disk" {
			sections.diskRows = append(sections.diskRows, line)
		}
	}
	return sections
}

func resourceBaseInfoParseVMBaseInfoGPUDevices(rows []string) []map[string]interface{} {
	devices := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 3 {
			continue
		}
		device := map[string]interface{}{
			"index":       resourceBaseInfoParseIntValue(strings.TrimSpace(parts[0])),
			"model":       strings.TrimSpace(parts[1]),
			"memoryBytes": resourceBaseInfoParseInt64Value(strings.TrimSpace(parts[2])) * 1024 * 1024,
		}
		if len(parts) > 3 {
			if uuid := strings.TrimSpace(parts[3]); uuid != "" {
				device["uuid"] = uuid
			}
		}
		if len(parts) > 4 {
			if busID := strings.TrimSpace(parts[4]); busID != "" {
				device["busId"] = busID
			}
		}
		if len(parts) > 5 {
			if vendor := strings.TrimSpace(parts[5]); vendor != "" {
				device["vendor"] = vendor
			}
		}
		devices = append(devices, device)
	}
	return devices
}

func resourceBaseInfoExtractMarkedSection(stdout, begin, end string) string {
	lines := strings.Split(stdout, "\n")
	collecting := false
	collected := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == begin {
			collecting = true
			continue
		}
		if line == end {
			break
		}
		if collecting {
			collected = append(collected, rawLine)
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

func resourceBaseInfoParseK8sCPUMilli(raw interface{}) int64 {
	text := strings.TrimSpace(resourceBaseInfoConvertToString(raw))
	if text == "" {
		return 0
	}
	if strings.HasSuffix(text, "m") {
		return resourceBaseInfoParseInt64Value(strings.TrimSuffix(text, "m"))
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return int64(value * 1000)
	}
	return resourceBaseInfoParseInt64Value(text)
}

func resourceBaseInfoParseK8sBytes(raw interface{}) int64 {
	text := strings.TrimSpace(resourceBaseInfoConvertToString(raw))
	if text == "" {
		return 0
	}
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"Ki", 1024},
		{"Mi", 1024 * 1024},
		{"Gi", 1024 * 1024 * 1024},
		{"Ti", 1024 * 1024 * 1024 * 1024},
	}
	for _, item := range multipliers {
		if strings.HasSuffix(text, item.suffix) {
			return resourceBaseInfoParseInt64Value(strings.TrimSuffix(text, item.suffix)) * item.mult
		}
	}
	return resourceBaseInfoParseInt64Value(text)
}

func resourceBaseInfoConvertToString(value interface{}) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}
