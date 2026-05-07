package handlers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestResourceBaseInfoV3CanonicalShapeAndLabelMerge(t *testing.T) {
	db := openHandlerTestDB(t)
	resource := models.Resource{
		WorkspaceID: 1,
		Name:        "inventory-vm",
		Type:        models.ResourceTypeVM,
		Environment: "production",
		Status:      models.ResourceStatusOnline,
		Endpoint:    "10.0.0.88:22",
		BaseInfo:    `{"schemaVersion":3,"resourceId":"resource:42","status":"success","source":"remote_task","collectedAt":"2026-05-06T00:00:00Z","labels":{"env":"prod"},"entities":[{"id":"resource:42:entity:host","kind":"machine","name":"node-a"}],"resourceTypes":[{"id":"cpu","name":"CPU"},{"id":"gpu","name":"GPU"}],"resourceInstances":[{"id":"resource:42:resource-instance:gpu:gpu%3A0","resourceTypeId":"gpu","entityId":"resource:42:entity:host","identity":[{"name":"index","value":0},{"name":"model","value":"MI300X"}],"spec":[{"name":"vendor","value":"AMD"}],"capacity":[{"name":"memoryBytes","capacity":196608}],"metrics":[{"name":"temperatureC","value":37}]}],"services":[{"id":"resource:42:service:trainer","name":"trainer","entityId":"resource:42:entity:host","resourceInstanceIds":["resource:42:resource-instance:gpu:gpu%3A0"]}],"allocations":[{"id":"resource:42:allocation:trainer:resource:42:resource-instance:gpu:gpu%3A0","claims":[{"serviceId":"resource:42:service:trainer","resourceInstanceId":"resource:42:resource-instance:gpu:gpu%3A0","dimensions":[{"name":"memoryBytes","capacity":196608},{"name":"utilization","value":37}]}]}]}`,
		CreatedBy:   1,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource failed: %v", err)
	}
	if err := db.Create(&models.ResourceRuntimeLabel{
		WorkspaceID: resource.WorkspaceID,
		ResourceID:  resource.ID,
		TargetType:  "resource",
		TargetKey:   buildResourceBaseInfoResourcePrefix(resource.ID),
		LabelsJSON:  `{"env":"staging","owner":"platform","vm":{"bad":true}}`,
		CreatedBy:   1,
	}).Error; err != nil {
		t.Fatalf("create runtime labels failed: %v", err)
	}

	resp := buildResourceBaseInfoResponse(resource, db)
	typed, ok := resp["base_info"].(ResourceBaseInfoV3)
	if !ok {
		t.Fatalf("expected typed canonical base_info, got %#v", resp["base_info"])
	}
	if typed.SchemaVersion != 3 {
		t.Fatalf("schemaVersion=%d, want 3", typed.SchemaVersion)
	}
	if typed.ResourceID != "resource:42" {
		t.Fatalf("resourceId=%q, want resource:42", typed.ResourceID)
	}
	if typed.Status != "success" {
		t.Fatalf("status=%q, want success", typed.Status)
	}
	if typed.Source != "remote_task" {
		t.Fatalf("source=%q, want remote_task", typed.Source)
	}
	if typed.CollectedAt != "2026-05-06T00:00:00Z" {
		t.Fatalf("collectedAt=%q, want RFC3339 string", typed.CollectedAt)
	}
	if len(typed.Entities) != 1 || len(typed.ResourceTypes) != 2 || len(typed.ResourceInstances) != 1 || len(typed.Services) != 1 || len(typed.Allocations) != 1 {
		t.Fatalf("unexpected canonical shape: %+v", typed)
	}
	if typed.Labels["env"] != "prod" {
		t.Fatalf("label merge overwrote existing value: %#v", typed.Labels)
	}
	if typed.Labels["owner"] != "platform" {
		t.Fatalf("label merge missing new key: %#v", typed.Labels)
	}
	if _, ok := typed.Labels["vm"]; ok {
		t.Fatalf("unexpected nested legacy label injected into canonical top-level labels: %#v", typed.Labels)
	}
	if len(typed.Entities) > 0 && len(typed.Entities[0].Fields) != 0 {
		t.Fatalf("unexpected nested legacy label side effects on entity fields: %+v", typed.Entities[0])
	}
}

func TestResourceBaseInfoV3StableIDsNormalizeReservedSeparators(t *testing.T) {
	if got := buildResourceBaseInfoEntityID(42, "zone:us-west/1"); got != "resource:42:entity:zone%3Aus-west%2F1" {
		t.Fatalf("entity id=%q, want %q", got, "resource:42:entity:zone%3Aus-west%2F1")
	}
	if got := buildResourceBaseInfoResourceInstanceID(42, "gpu", "gpu:0/primary"); got != "resource:42:resource-instance:gpu:gpu%3A0%2Fprimary" {
		t.Fatalf("resource instance id=%q, want %q", got, "resource:42:resource-instance:gpu:gpu%3A0%2Fprimary")
	}
	if got := buildResourceBaseInfoServiceID(42, "trainer:alpha"); got != "resource:42:service:trainer%3Aalpha" {
		t.Fatalf("service id=%q, want %q", got, "resource:42:service:trainer%3Aalpha")
	}
	if got := buildResourceBaseInfoAllocationID(42, "trainer:alpha", "gpu:0"); got != "resource:42:allocation:trainer%3Aalpha:gpu%3A0" {
		t.Fatalf("allocation id=%q, want %q", got, "resource:42:allocation:trainer%3Aalpha:gpu%3A0")
	}
}

func TestResourceBaseInfoV3ApprovedResourceTypeIDs(t *testing.T) {
	got := approvedResourceBaseInfoResourceTypeIDs()
	want := []string{"cpu", "memory", "gpu", "port", "storage", "network"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resourceTypeIDs=%v, want %v", got, want)
	}
	types := approvedResourceBaseInfoResourceTypes()
	wantNames := []string{"CPU", "Memory", "GPU", "Port", "Storage", "Network"}
	if len(types) != len(wantNames) {
		t.Fatalf("resourceTypes len=%d, want %d", len(types), len(wantNames))
	}
	for i, wantName := range wantNames {
		if types[i].Name != wantName {
			t.Fatalf("resourceType[%d].Name=%q, want %q", i, types[i].Name, wantName)
		}
	}
}

func TestResourceBaseInfoV3GPUInstanceEncodingAndAllocationClaims(t *testing.T) {
	gpuInstance := ResourceBaseInfoResourceInstance{
		ID:             buildResourceBaseInfoResourceInstanceID(42, "gpu", "0"),
		ResourceTypeID: "gpu",
		EntityID:       buildResourceBaseInfoEntityID(42, "machine"),
		Identity: []ResourceBaseInfoField{
			newResourceBaseInfoField("index", 0),
			newResourceBaseInfoField("model", "MI300X"),
		},
		Spec: []ResourceBaseInfoField{
			newResourceBaseInfoField("vendor", "AMD"),
			newResourceBaseInfoField("uuid", "GPU-123"),
		},
		Capacity: []ResourceBaseInfoMeasure{
			newResourceBaseInfoMeasureCapacity("memoryBytes", 196608),
		},
		Metrics: []ResourceBaseInfoMeasure{
			newResourceBaseInfoMeasureValue("temperatureC", 37),
		},
	}
	allocation := ResourceBaseInfoAllocation{
		ID: buildResourceBaseInfoAllocationID(42, "trainer", gpuInstance.ID),
		Claims: []ResourceBaseInfoAllocationClaim{
			{
				ServiceID:         buildResourceBaseInfoServiceID(42, "trainer"),
				ResourceInstanceID: gpuInstance.ID,
				Dimensions: []ResourceBaseInfoField{
					newResourceBaseInfoField("memoryBytes", 196608),
					newResourceBaseInfoField("smCount", 132),
					newResourceBaseInfoField("temperatureC", 37),
				},
			},
		},
	}

	payload := ResourceBaseInfoV3{
		SchemaVersion:     3,
		ResourceID:        "resource:42",
		Status:            "success",
		Source:            "remote_task",
		CollectedAt:       "2026-05-06T00:00:00Z",
		ResourceTypes:     approvedResourceBaseInfoResourceTypes(),
		ResourceInstances:  []ResourceBaseInfoResourceInstance{gpuInstance},
		Services:          []ResourceBaseInfoService{{ID: buildResourceBaseInfoServiceID(42, "trainer")}},
		Allocations:       []ResourceBaseInfoAllocation{allocation},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical payload: %v", err)
	}
	if !resourceBaseInfoJSONContains(raw, `"resourceInstances"`) || !resourceBaseInfoJSONContains(raw, `"capacity"`) || !resourceBaseInfoJSONContains(raw, `"metrics"`) {
		t.Fatalf("gpu instance encoding missing expected sections: %s", string(raw))
	}
	if !resourceBaseInfoJSONContains(raw, `"claims"`) || !resourceBaseInfoJSONContains(raw, `"smCount"`) || !resourceBaseInfoJSONContains(raw, `"temperatureC"`) {
		t.Fatalf("allocation claim encoding missing dimensions: %s", string(raw))
	}
	if len(allocation.Claims) != 1 || len(allocation.Claims[0].Dimensions) != 3 {
		t.Fatalf("allocation claims=%+v, want one claim carrying three dimensions", allocation.Claims)
	}
	if allocation.Claims[0].ServiceID != buildResourceBaseInfoServiceID(42, "trainer") || allocation.Claims[0].ResourceInstanceID != gpuInstance.ID {
		t.Fatalf("allocation claim identifiers not stable: %+v", allocation.Claims[0])
	}
}

func TestResourceBaseInfoV3MeasureShapeRules(t *testing.T) {
	if err := validateResourceBaseInfoMeasureShape(ResourceBaseInfoMeasure{Value: ptrFloat64(1), Used: ptrFloat64(2)}); err == nil {
		t.Fatal("expected mixed value/used measure shape to be rejected")
	}
	if err := validateResourceBaseInfoMeasureShape(ResourceBaseInfoMeasure{Available: ptrFloat64(3)}); err == nil {
		t.Fatal("expected available without sourceReported to be rejected")
	}
	if err := validateResourceBaseInfoMeasureShape(ResourceBaseInfoMeasure{Available: ptrFloat64(3), Value: ptrFloat64(1), SourceReported: true}); err == nil {
		t.Fatal("expected available mixed with another numeric shape to be rejected")
	}
	measure := ResourceBaseInfoMeasure{Available: ptrFloat64(3), SourceReported: true}
	if err := validateResourceBaseInfoMeasureShape(measure); err != nil {
		t.Fatalf("expected source-reported available measure to pass, got %v", err)
	}
	raw, err := json.Marshal(measure)
	if err != nil {
		t.Fatalf("marshal measure: %v", err)
	}
	if !resourceBaseInfoJSONContains(raw, `"available"`) || !resourceBaseInfoJSONContains(raw, `"sourceReported"`) {
		t.Fatalf("expected source-reported available to be encoded: %s", string(raw))
	}
}

func TestResourceBaseInfoV3BannedShapesOmitted(t *testing.T) {
	assertJSONTagsAbsent(t, ResourceBaseInfoV3{}, "vm", "k8s", "nodes", "gpus", "carriers", "matrix", "axes", "cells")
	assertJSONTagsAbsent(t, ResourceBaseInfoService{}, "allocations")
	assertJSONTagsAbsent(t, ResourceBaseInfoResourceInstance{}, "usedBy")
}

func TestBuildResourceBaseInfoResponseDropsInvalidV3Measures(t *testing.T) {
	resource := models.Resource{
		BaseInfo: `{"schemaVersion":3,"resourceId":"resource:42","status":"success","source":"remote_task","collectedAt":"2026-05-06T00:00:00Z","resourceInstances":[{"id":"ri-1","resourceTypeId":"gpu","entityId":"entity-1","metrics":[{"name":"bad-1","available":3,"value":1,"sourceReported":true},{"name":"bad-2","available":3},{"name":"good","available":4,"sourceReported":true}]}]}`,
	}

	resp := buildResourceBaseInfoResponse(resource, nil)
	typed, ok := resp["base_info"].(ResourceBaseInfoV3)
	if !ok {
		t.Fatalf("expected typed canonical base_info, got %#v", resp["base_info"])
	}
	if len(typed.ResourceInstances) != 1 {
		t.Fatalf("resourceInstances len=%d, want 1", len(typed.ResourceInstances))
	}
	metrics := typed.ResourceInstances[0].Metrics
	if len(metrics) != 1 {
		t.Fatalf("metrics len=%d, want 1 after sanitization", len(metrics))
	}
	if metrics[0].Name != "good" || metrics[0].Available == nil || !metrics[0].SourceReported {
		t.Fatalf("unexpected surviving metric after sanitization: %+v", metrics[0])
	}
}

func TestBuildResourceBaseInfoResponseKeepsSchemaVersion1Compatibility(t *testing.T) {
	resource := models.Resource{
		BaseInfo: `{"schemaVersion":1,"status":"success","source":"remote_task","collectedAt":1710000000,"machine":{"cpu":{"logicalCores":8}}}`,
		Labels:   `{"env":"prod"}`,
	}

	resp := buildResourceBaseInfoResponse(resource, nil)
	baseInfo, ok := resp["base_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected schemaVersion 1 payload to stay untyped, got %#v", resp["base_info"])
	}
	if baseInfo["schemaVersion"].(float64) != 1 {
		t.Fatalf("schemaVersion=%v, want 1", baseInfo["schemaVersion"])
	}
	labels, ok := resp["labels"].(map[string]interface{})
	if !ok || labels["env"] != "prod" {
		t.Fatalf("expected labels to stay serialized, got %#v", resp["labels"])
	}
}

func assertJSONTagsAbsent(t *testing.T, v interface{}, banned ...string) {
	t.Helper()
	typ := reflect.TypeOf(v)
	seen := map[string]struct{}{}
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" {
			continue
		}
		name := tag
		if idx := indexComma(tag); idx >= 0 {
			name = tag[:idx]
		}
		seen[name] = struct{}{}
	}
	for _, name := range banned {
		if _, ok := seen[name]; ok {
			t.Fatalf("found banned json field %q in %T", name, v)
		}
	}
}

func indexComma(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

func ptrFloat64(v float64) *float64 { return &v }

func resourceBaseInfoJSONContains(raw []byte, needle string) bool {
	return strings.Contains(string(raw), needle)
}

