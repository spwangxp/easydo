package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theory/jsonpath"
)

const (
	webhookRuntimeInputSourceTypeJSONPath = "jsonpath"

	webhookRuntimeInputMissingPolicyIgnore = "ignore"
	webhookRuntimeInputMissingPolicyFail   = "fail"

	webhookRuntimeScalarTypeString  = "string"
	webhookRuntimeScalarTypeNumber  = "number"
	webhookRuntimeScalarTypeBoolean = "boolean"

	webhookRuntimeMappingStatusMatched           = "matched"
	webhookRuntimeMappingStatusMissing           = "missing"
	webhookRuntimeMappingStatusInvalidExpression = "invalid_expression"
	webhookRuntimeMappingStatusInvalidTarget     = "invalid_target"
	webhookRuntimeMappingStatusMultipleValues    = "multiple_values"
	webhookRuntimeMappingStatusTypeUnsupported   = "type_unsupported"
	webhookRuntimeMappingStatusTypeMismatch      = "type_mismatch"
	webhookRuntimeMappingStatusInvalidPolicy     = "invalid_missing_policy"
	webhookRuntimeMappingStatusInvalidID         = "invalid_id"
	webhookRuntimeMappingStatusDuplicateID       = "duplicate_id"
	webhookRuntimeMappingStatusTooManyRules      = "too_many_rules"
	webhookRuntimeMappingStatusPayloadTooLarge   = "payload_too_large"

	webhookRuntimeMappingMaxRules          = 100
	webhookRuntimePreviewPayloadLimitBytes = 1024 * 1024
)

type webhookRuntimeInputTarget struct {
	NodeID   string `json:"node_id"`
	ParamKey string `json:"param_key"`
}

type webhookRuntimeInputMapping struct {
	ID            string                    `json:"id"`
	SourceType    string                    `json:"source_type"`
	SourceExpr    string                    `json:"source_expr"`
	Target        webhookRuntimeInputTarget `json:"target"`
	MissingPolicy string                    `json:"missing_policy"`
}

type runtimeSettableTarget struct {
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name,omitempty"`
	ParamKey   string `json:"param_key"`
	ParamLabel string `json:"param_label,omitempty"`
	FieldType  string `json:"field_type"`
}

type mappingSummary struct {
	Total   int `json:"total"`
	Matched int `json:"matched"`
	Missing int `json:"missing"`
	Failed  int `json:"failed"`
}

type mappingError struct {
	ID      string                    `json:"id,omitempty"`
	Code    string                    `json:"code"`
	Message string                    `json:"message,omitempty"`
	Target  webhookRuntimeInputTarget `json:"target,omitempty"`
}

type webhookRuntimePreviewRuleResult struct {
	Code  string      `json:"code"`
	Value interface{} `json:"value,omitempty"`
}

type webhookRuntimePreviewResult struct {
	Values      map[string]map[string]interface{}          `json:"values"`
	RuleResults map[string]webhookRuntimePreviewRuleResult `json:"rule_results"`
	Summary     mappingSummary                             `json:"summary"`
}

func runtimeSettableTargetKey(nodeID, paramKey string) string {
	return strings.TrimSpace(nodeID) + "\x00" + strings.TrimSpace(paramKey)
}

func buildRuntimeSettableTargets(config PipelineConfig) map[string]runtimeSettableTarget {
	targets := make(map[string]runtimeSettableTarget)
	for _, node := range config.Nodes {
		nodeID := firstNonEmptyTaskValue(node.ID, node.NodeID)
		if strings.TrimSpace(nodeID) == "" || len(node.DefinitionParams) == 0 {
			continue
		}
		_, def, ok := getPipelineTaskDefinition(firstNonEmptyTaskValue(node.TaskKey, node.Type))
		if !ok {
			continue
		}
		fieldTargets := make(map[string]runtimeSettableTarget, len(def.FieldsSchema))
		for _, field := range def.FieldsSchema {
			scalarType, ok := normalizeRuntimeFieldType(field.Type)
			if !ok {
				continue
			}
			fieldTargets[field.Key] = runtimeSettableTarget{
				NodeID:     nodeID,
				NodeName:   firstNonEmptyTaskValue(node.Name, node.NodeName),
				ParamKey:   field.Key,
				ParamLabel: field.Label,
				FieldType:  scalarType,
			}
		}
		for _, param := range node.DefinitionParams {
			if !param.IsFlexible {
				continue
			}
			target, ok := fieldTargets[param.Key]
			if !ok {
				continue
			}
			targets[runtimeSettableTargetKey(nodeID, param.Key)] = target
		}
	}
	return targets
}

func validateWebhookRuntimeMappings(mappings []webhookRuntimeInputMapping, targets map[string]runtimeSettableTarget) []mappingError {
	mappings = normalizeWebhookRuntimeMappings(mappings)
	errs := make([]mappingError, 0)
	if len(mappings) > webhookRuntimeMappingMaxRules {
		errs = append(errs, mappingError{Code: webhookRuntimeMappingStatusTooManyRules, Message: fmt.Sprintf("mapping rules exceed limit %d", webhookRuntimeMappingMaxRules)})
	}
	seen := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		id := strings.TrimSpace(mapping.ID)
		if id == "" {
			errs = append(errs, mappingError{Code: webhookRuntimeMappingStatusInvalidID, Message: "mapping id is required", Target: mapping.Target})
		} else if _, exists := seen[id]; exists {
			errs = append(errs, mappingError{ID: id, Code: webhookRuntimeMappingStatusDuplicateID, Message: "duplicate mapping id", Target: mapping.Target})
		} else {
			seen[id] = struct{}{}
		}
		if _, err := compileWebhookRuntimeJSONPath(mapping.SourceType, mapping.SourceExpr); err != nil {
			errs = append(errs, mappingError{ID: id, Code: webhookRuntimeMappingStatusInvalidExpression, Message: err.Error(), Target: mapping.Target})
		}
		switch strings.TrimSpace(mapping.MissingPolicy) {
		case webhookRuntimeInputMissingPolicyIgnore, webhookRuntimeInputMissingPolicyFail:
		default:
			errs = append(errs, mappingError{ID: id, Code: webhookRuntimeMappingStatusInvalidPolicy, Message: "unsupported missing policy", Target: mapping.Target})
		}
		if _, ok := targets[runtimeSettableTargetKey(mapping.Target.NodeID, mapping.Target.ParamKey)]; !ok {
			errs = append(errs, mappingError{ID: id, Code: webhookRuntimeMappingStatusInvalidTarget, Message: "target is not runtime settable", Target: mapping.Target})
		}
	}
	return errs
}

func evaluateWebhookRuntimeMappings(payload any, mappings []webhookRuntimeInputMapping, targets map[string]runtimeSettableTarget) (map[string]map[string]interface{}, mappingSummary, []mappingError) {
	mappings = normalizeWebhookRuntimeMappings(mappings)
	summary := mappingSummary{Total: len(mappings)}
	values := make(map[string]map[string]interface{})
	errs := make([]mappingError, 0)
	for _, mapping := range mappings {
		target, ok := targets[runtimeSettableTargetKey(mapping.Target.NodeID, mapping.Target.ParamKey)]
		if !ok {
			summary.Failed++
			errs = append(errs, mappingError{ID: mapping.ID, Code: webhookRuntimeMappingStatusInvalidTarget, Message: "target is not runtime settable", Target: mapping.Target})
			continue
		}
		code, value, shouldAssign := evaluateWebhookRuntimeMapping(payload, mapping, target)
		switch code {
		case webhookRuntimeMappingStatusMatched:
			summary.Matched++
			if shouldAssign {
				nodeValues := values[target.NodeID]
				if nodeValues == nil {
					nodeValues = make(map[string]interface{})
					values[target.NodeID] = nodeValues
				}
				nodeValues[target.ParamKey] = value
			}
		case webhookRuntimeMappingStatusMissing:
			summary.Missing++
			if strings.TrimSpace(mapping.MissingPolicy) == webhookRuntimeInputMissingPolicyFail {
				summary.Failed++
				errs = append(errs, mappingError{ID: mapping.ID, Code: code, Message: "source value is missing", Target: mapping.Target})
			}
		default:
			summary.Failed++
			errs = append(errs, mappingError{ID: mapping.ID, Code: code, Message: "mapping evaluation failed", Target: mapping.Target})
		}
	}
	return values, summary, errs
}

func previewWebhookRuntimeMappings(payload any, mappings []webhookRuntimeInputMapping, targets map[string]runtimeSettableTarget) (webhookRuntimePreviewResult, []mappingError) {
	result := webhookRuntimePreviewResult{
		Values:      make(map[string]map[string]interface{}),
		RuleResults: make(map[string]webhookRuntimePreviewRuleResult),
	}
	if err := validatePreviewPayloadSize(payload); err != nil {
		return result, []mappingError{{Code: webhookRuntimeMappingStatusPayloadTooLarge, Message: err.Error()}}
	}
	validationErrs := validateWebhookRuntimeMappings(mappings, targets)
	if len(validationErrs) > 0 {
		return result, validationErrs
	}
	values, summary, runtimeErrs := evaluateWebhookRuntimeMappings(payload, mappings, targets)
	result.Values = values
	result.Summary = summary
	for _, mapping := range normalizeWebhookRuntimeMappings(mappings) {
		target, ok := targets[runtimeSettableTargetKey(mapping.Target.NodeID, mapping.Target.ParamKey)]
		if !ok {
			result.RuleResults[mapping.ID] = webhookRuntimePreviewRuleResult{Code: webhookRuntimeMappingStatusInvalidTarget}
			continue
		}
		code, value, shouldAssign := evaluateWebhookRuntimeMapping(payload, mapping, target)
		ruleResult := webhookRuntimePreviewRuleResult{Code: code}
		if code == webhookRuntimeMappingStatusMatched && shouldAssign {
			ruleResult.Value = value
		}
		result.RuleResults[mapping.ID] = ruleResult
	}
	_ = runtimeErrs
	return result, nil
}

func normalizeWebhookRuntimeMappings(mappings []webhookRuntimeInputMapping) []webhookRuntimeInputMapping {
	if mappings == nil {
		return []webhookRuntimeInputMapping{}
	}
	return mappings
}

func validatePreviewPayloadSize(payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(encoded) > webhookRuntimePreviewPayloadLimitBytes {
		return fmt.Errorf("preview payload exceeds %d bytes", webhookRuntimePreviewPayloadLimitBytes)
	}
	return nil
}

func evaluateWebhookRuntimeMapping(payload any, mapping webhookRuntimeInputMapping, target runtimeSettableTarget) (string, interface{}, bool) {
	path, err := compileWebhookRuntimeJSONPath(mapping.SourceType, mapping.SourceExpr)
	if err != nil {
		return webhookRuntimeMappingStatusInvalidExpression, nil, false
	}
	matches := path.Select(payload)
	if len(matches) == 0 {
		return webhookRuntimeMappingStatusMissing, nil, false
	}
	if len(matches) > 1 {
		return webhookRuntimeMappingStatusMultipleValues, nil, false
	}
	value := matches[0]
	if value == nil {
		return webhookRuntimeMappingStatusMissing, nil, false
	}
	scalarType, ok := detectRuntimeScalarType(value)
	if !ok {
		return webhookRuntimeMappingStatusTypeUnsupported, nil, false
	}
	if scalarType != target.FieldType {
		return webhookRuntimeMappingStatusTypeMismatch, nil, false
	}
	return webhookRuntimeMappingStatusMatched, value, true
}

func compileWebhookRuntimeJSONPath(sourceType, expr string) (*jsonpath.Path, error) {
	if strings.TrimSpace(sourceType) != webhookRuntimeInputSourceTypeJSONPath {
		return nil, fmt.Errorf("unsupported source type %q", sourceType)
	}
	path, err := jsonpath.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil, err
	}
	return path, nil
}

func normalizeRuntimeFieldType(fieldType string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(fieldType)) {
	case webhookRuntimeScalarTypeString, "select", "text":
		return webhookRuntimeScalarTypeString, true
	case webhookRuntimeScalarTypeNumber:
		return webhookRuntimeScalarTypeNumber, true
	case webhookRuntimeScalarTypeBoolean:
		return webhookRuntimeScalarTypeBoolean, true
	default:
		return "", false
	}
}

func detectRuntimeScalarType(value interface{}) (string, bool) {
	switch value.(type) {
	case string:
		return webhookRuntimeScalarTypeString, true
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return webhookRuntimeScalarTypeNumber, true
	case bool:
		return webhookRuntimeScalarTypeBoolean, true
	default:
		return "", false
	}
}
