package handlers

import (
	"strings"
	"testing"

	"easydo-server/internal/models"
)

func TestEvaluateWebhookRuntimeMappings_MatchedScalarValue(t *testing.T) {
	payload := map[string]interface{}{"ref": "main"}
	mappings := []webhookRuntimeInputMapping{{
		ID:            "rule-1",
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.ref",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
		MissingPolicy: webhookRuntimeInputMissingPolicyFail,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {
			NodeID:    "node-1",
			ParamKey:  "git_ref",
			FieldType: webhookRuntimeScalarTypeString,
		},
	}

	values, summary, errs := evaluateWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
	if got := values["node-1"]["git_ref"]; got != "main" {
		t.Fatalf("expected mapped string value, got %#v", got)
	}
	if summary.Matched != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestEvaluateWebhookRuntimeMappings_MissingFailStopsExecution(t *testing.T) {
	mappings := []webhookRuntimeInputMapping{{
		ID:            "rule-1",
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.ref",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
		MissingPolicy: webhookRuntimeInputMissingPolicyFail,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {
			NodeID:    "node-1",
			ParamKey:  "git_ref",
			FieldType: webhookRuntimeScalarTypeString,
		},
	}

	values, summary, errs := evaluateWebhookRuntimeMappings(map[string]interface{}{}, mappings, targets)
	if len(values) != 0 {
		t.Fatalf("expected no mapped values, got %#v", values)
	}
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %#v", errs)
	}
	if errs[0].Code != webhookRuntimeMappingStatusMissing {
		t.Fatalf("expected missing code, got %#v", errs[0])
	}
	if summary.Missing != 1 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestEvaluateWebhookRuntimeMappings_MultipleValuesRejected(t *testing.T) {
	payload := map[string]interface{}{"refs": []interface{}{"main", "release"}}
	mappings := []webhookRuntimeInputMapping{{
		ID:            "rule-1",
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.refs[*]",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
		MissingPolicy: webhookRuntimeInputMissingPolicyFail,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {
			NodeID:    "node-1",
			ParamKey:  "git_ref",
			FieldType: webhookRuntimeScalarTypeString,
		},
	}

	_, _, errs := evaluateWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %#v", errs)
	}
	if errs[0].Code != webhookRuntimeMappingStatusMultipleValues {
		t.Fatalf("expected multiple_values code, got %#v", errs[0])
	}
}

func TestEvaluateWebhookRuntimeMappings_TypeMatrixRejectsStringIntoNumber(t *testing.T) {
	payload := map[string]interface{}{"port": "22"}
	mappings := []webhookRuntimeInputMapping{{
		ID:            "rule-1",
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.port",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "port"},
		MissingPolicy: webhookRuntimeInputMissingPolicyFail,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "port"): {
			NodeID:    "node-1",
			ParamKey:  "port",
			FieldType: webhookRuntimeScalarTypeNumber,
		},
	}

	_, _, errs := evaluateWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %#v", errs)
	}
	if errs[0].Code != webhookRuntimeMappingStatusTypeMismatch {
		t.Fatalf("expected type_mismatch code, got %#v", errs[0])
	}
}

func TestPreviewWebhookRuntimeMappings_PreservesNumberAndBooleanTypes(t *testing.T) {
	payload := map[string]interface{}{"port": 22.0, "push": true}
	mappings := []webhookRuntimeInputMapping{
		{
			ID:            "port",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.port",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "port"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
		{
			ID:            "push",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.push",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "push"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
	}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "port"): {NodeID: "node-1", ParamKey: "port", FieldType: webhookRuntimeScalarTypeNumber},
		runtimeSettableTargetKey("node-1", "push"): {NodeID: "node-1", ParamKey: "push", FieldType: webhookRuntimeScalarTypeBoolean},
	}

	result, errs := previewWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
	if got, ok := result.Values["node-1"]["port"].(float64); !ok || got != 22 {
		t.Fatalf("expected numeric preview value, got %#v", result.Values["node-1"]["port"])
	}
	if got, ok := result.Values["node-1"]["push"].(bool); !ok || !got {
		t.Fatalf("expected boolean preview value, got %#v", result.Values["node-1"]["push"])
	}
}

func TestPreviewWebhookRuntimeMappings_ReturnsExpectedStatusCodes(t *testing.T) {
	payload := map[string]interface{}{"refs": []interface{}{"main", "release"}, "nested": map[string]interface{}{"key": "value"}}
	mappings := []webhookRuntimeInputMapping{
		{
			ID:            "matched",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.nested.key",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "script"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
		{
			ID:            "missing",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.missing",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "shell"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
		{
			ID:            "multiple",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.refs[*]",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
	}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "script"):  {NodeID: "node-1", ParamKey: "script", FieldType: webhookRuntimeScalarTypeString},
		runtimeSettableTargetKey("node-1", "shell"):   {NodeID: "node-1", ParamKey: "shell", FieldType: webhookRuntimeScalarTypeString},
		runtimeSettableTargetKey("node-1", "git_ref"): {NodeID: "node-1", ParamKey: "git_ref", FieldType: webhookRuntimeScalarTypeString},
	}

	result, errs := previewWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 0 {
		t.Fatalf("expected no top-level preview errors, got %#v", errs)
	}
	if result.RuleResults["matched"].Code != webhookRuntimeMappingStatusMatched {
		t.Fatalf("expected matched status, got %#v", result.RuleResults["matched"])
	}
	if result.RuleResults["missing"].Code != webhookRuntimeMappingStatusMissing {
		t.Fatalf("expected missing status, got %#v", result.RuleResults["missing"])
	}
	if result.RuleResults["multiple"].Code != webhookRuntimeMappingStatusMultipleValues {
		t.Fatalf("expected multiple_values status, got %#v", result.RuleResults["multiple"])
	}
}

func TestValidateWebhookRuntimeMappings_RejectsMoreThan100RulesAndDuplicateIDs(t *testing.T) {
	mappings := make([]webhookRuntimeInputMapping, 0, 101)
	for i := 0; i < 101; i++ {
		mappings = append(mappings, webhookRuntimeInputMapping{
			ID:            "dup",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.ref",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		})
	}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {NodeID: "node-1", ParamKey: "git_ref", FieldType: webhookRuntimeScalarTypeString},
	}

	errs := validateWebhookRuntimeMappings(mappings, targets)
	if len(errs) < 2 {
		t.Fatalf("expected validation errors, got %#v", errs)
	}
	codes := make([]string, 0, len(errs))
	for _, err := range errs {
		codes = append(codes, err.Code)
	}
	joined := strings.Join(codes, ",")
	if !strings.Contains(joined, webhookRuntimeMappingStatusTooManyRules) {
		t.Fatalf("expected too_many_rules code, got %v", codes)
	}
	if !strings.Contains(joined, webhookRuntimeMappingStatusDuplicateID) {
		t.Fatalf("expected duplicate_id code, got %v", codes)
	}
}

func TestValidateWebhookRuntimeMappings_RejectsEmptyID(t *testing.T) {
	mappings := []webhookRuntimeInputMapping{{
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.ref",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
		MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {NodeID: "node-1", ParamKey: "git_ref", FieldType: webhookRuntimeScalarTypeString},
	}

	errs := validateWebhookRuntimeMappings(mappings, targets)
	if len(errs) != 1 {
		t.Fatalf("expected one validation error, got %#v", errs)
	}
	if errs[0].Code != webhookRuntimeMappingStatusInvalidID {
		t.Fatalf("expected invalid_id code, got %#v", errs[0])
	}
}

func TestBuildRuntimeSettableTargets_UsesTaskFieldSchema(t *testing.T) {
	config := PipelineConfig{Nodes: []PipelineNode{{
		ID:      "node-1",
		TaskKey: "docker-run",
		Type:    "docker-run",
		Name:    "Deploy",
		DefinitionParams: []models.PipelineDefinitionParam{
			{Key: "host", Value: "example.com", IsFlexible: true},
			{Key: "port", Value: "22", IsFlexible: true},
			{Key: "runtime", Value: "docker", IsFlexible: true},
			{Key: "ignored", Value: "x", IsFlexible: true},
			{Key: "fixed", Value: "y", IsFlexible: false},
		},
	}}}

	targets := buildRuntimeSettableTargets(config)
	portTarget, ok := targets[runtimeSettableTargetKey("node-1", "port")]
	if !ok {
		t.Fatalf("expected port target to exist")
	}
	if portTarget.FieldType != webhookRuntimeScalarTypeNumber {
		t.Fatalf("expected port type from task field schema, got %#v", portTarget)
	}
	if runtimeTarget, ok := targets[runtimeSettableTargetKey("node-1", "runtime")]; !ok || runtimeTarget.FieldType != webhookRuntimeScalarTypeString {
		t.Fatalf("expected select field to normalize to string, got %#v", runtimeTarget)
	}
	if _, ok := targets[runtimeSettableTargetKey("node-1", "ignored")]; ok {
		t.Fatalf("expected unknown schema field to be excluded")
	}
	if _, ok := targets[runtimeSettableTargetKey("node-1", "fixed")]; ok {
		t.Fatalf("expected non-flexible field to be excluded")
	}
}

func TestBuildRuntimeSettableTargets_TreatsTextFieldsAsString(t *testing.T) {
	config := PipelineConfig{Nodes: []PipelineNode{{
		ID:      "node-1",
		TaskKey: "shell",
		Type:    "shell",
		Name:    "Build",
		DefinitionParams: []models.PipelineDefinitionParam{{
			Key:        "script",
			Label:      "脚本",
			Value:      "echo default",
			IsFlexible: true,
		}},
	}}}

	target, ok := buildRuntimeSettableTargets(config)[runtimeSettableTargetKey("node-1", "script")]
	if !ok {
		t.Fatalf("expected text field target to exist")
	}
	if target.FieldType != webhookRuntimeScalarTypeString {
		t.Fatalf("expected text field to normalize to string, got %#v", target)
	}
}

func TestPreviewWebhookRuntimeMappings_RejectsPayloadOver1MB(t *testing.T) {
	payload := map[string]interface{}{"data": strings.Repeat("a", webhookRuntimePreviewPayloadLimitBytes)}
	_, errs := previewWebhookRuntimeMappings(payload, nil, nil)
	if len(errs) != 1 || errs[0].Code != webhookRuntimeMappingStatusPayloadTooLarge {
		t.Fatalf("expected payload_too_large error, got %#v", errs)
	}
}

func TestEvaluateWebhookRuntimeMappings_MultipleValuesRejectsMixedNullAndScalar(t *testing.T) {
	payload := map[string]interface{}{"refs": []interface{}{nil, "main"}}
	mappings := []webhookRuntimeInputMapping{{
		ID:            "rule-1",
		SourceType:    webhookRuntimeInputSourceTypeJSONPath,
		SourceExpr:    "$.refs[*]",
		Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
		MissingPolicy: webhookRuntimeInputMissingPolicyFail,
	}}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {NodeID: "node-1", ParamKey: "git_ref", FieldType: webhookRuntimeScalarTypeString},
	}

	_, _, errs := evaluateWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %#v", errs)
	}
	if errs[0].Code != webhookRuntimeMappingStatusMultipleValues {
		t.Fatalf("expected multiple_values code, got %#v", errs[0])
	}
}

func TestPreviewWebhookRuntimeMappings_PreservesPerRuleStatusForSharedTarget(t *testing.T) {
	payload := map[string]interface{}{"ref": "main"}
	mappings := []webhookRuntimeInputMapping{
		{
			ID:            "matched",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.ref",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
		{
			ID:            "missing",
			SourceType:    webhookRuntimeInputSourceTypeJSONPath,
			SourceExpr:    "$.missing",
			Target:        webhookRuntimeInputTarget{NodeID: "node-1", ParamKey: "git_ref"},
			MissingPolicy: webhookRuntimeInputMissingPolicyIgnore,
		},
	}
	targets := map[string]runtimeSettableTarget{
		runtimeSettableTargetKey("node-1", "git_ref"): {NodeID: "node-1", ParamKey: "git_ref", FieldType: webhookRuntimeScalarTypeString},
	}

	result, errs := previewWebhookRuntimeMappings(payload, mappings, targets)
	if len(errs) != 0 {
		t.Fatalf("expected no preview errors, got %#v", errs)
	}
	if result.RuleResults["matched"].Code != webhookRuntimeMappingStatusMatched {
		t.Fatalf("expected matched rule to stay matched, got %#v", result.RuleResults["matched"])
	}
	if result.RuleResults["missing"].Code != webhookRuntimeMappingStatusMissing {
		t.Fatalf("expected missing rule to stay missing, got %#v", result.RuleResults["missing"])
	}
}
