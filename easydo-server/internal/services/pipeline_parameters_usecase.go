package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"easydo-server/internal/models"
)

type GetPipelineParameterSchemaRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
}

type PreviewPipelineTriggerParametersRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	Inputs      map[string]map[string]any
}

type GetPipelineRunParametersRequest struct {
	Actor       ActorContext
	WorkspaceID uint64
	PipelineID  uint64
	RunID       uint64
}

type PipelineParameterSchemaResult struct {
	WorkspaceID    uint64                            `json:"workspace_id"`
	PipelineID     uint64                            `json:"pipeline_id"`
	Prompt         string                            `json:"prompt,omitempty"`
	InputPrompts   []PipelineParameterPrompt         `json:"input_prompts,omitempty"`
	ParameterTable []PipelineParameterTableRow       `json:"parameter_table,omitempty"`
	Nodes          []PipelineParameterNodeSchema     `json:"nodes"`
	ExampleInputs  map[string]map[string]interface{} `json:"example_inputs"`
}

type PipelineParameterNodeSchema struct {
	NodeID   string                    `json:"node_id"`
	NodeName string                    `json:"node_name,omitempty"`
	TaskType string                    `json:"task_type,omitempty"`
	Params   []PipelineParameterSchema `json:"params"`
}

type PipelineParameterSchema struct {
	Key        string      `json:"key"`
	Label      string      `json:"label,omitempty"`
	Value      interface{} `json:"value,omitempty"`
	IsFlexible bool        `json:"is_flexible"`
	Required   bool        `json:"required"`
}

type PipelineTriggerPreviewResult struct {
	WorkspaceID      uint64                            `json:"workspace_id"`
	PipelineID       uint64                            `json:"pipeline_id"`
	CanTrigger       bool                              `json:"can_trigger"`
	Prompt           string                            `json:"prompt,omitempty"`
	InputPrompts     []PipelineParameterPrompt         `json:"input_prompts,omitempty"`
	ParameterTable   []PipelineParameterTableRow       `json:"parameter_table,omitempty"`
	Missing          []PipelineParameterIssue          `json:"missing"`
	Unknown          []PipelineParameterIssue          `json:"unknown"`
	ExampleInputs    map[string]map[string]interface{} `json:"example_inputs"`
	NormalizedInputs map[string]map[string]interface{} `json:"normalized_inputs,omitempty"`
}

type PipelineParameterPrompt struct {
	NodeID       string      `json:"node_id"`
	NodeName     string      `json:"node_name,omitempty"`
	TaskType     string      `json:"task_type,omitempty"`
	Key          string      `json:"key"`
	Label        string      `json:"label,omitempty"`
	CurrentValue interface{} `json:"current_value,omitempty"`
	InputPath    string      `json:"input_path"`
	Message      string      `json:"message"`
}

type PipelineParameterTableRow struct {
	TaskName           string      `json:"task_name,omitempty"`
	TaskType           string      `json:"task_type,omitempty"`
	NodeID             string      `json:"node_id"`
	ParamName          string      `json:"param_name"`
	ParamLabel         string      `json:"param_label,omitempty"`
	ValueType          string      `json:"value_type"`
	DefaultValue       interface{} `json:"default_value,omitempty"`
	InputPath          string      `json:"input_path"`
	Required           bool        `json:"required"`
	InteractionOptions []string    `json:"interaction_options"`
}

type PipelineParameterIssue struct {
	NodeID string `json:"node_id"`
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason"`
}

type PipelineRunParameterView struct {
	RunID       uint64                      `json:"run_id"`
	PipelineID  uint64                      `json:"pipeline_id"`
	BuildNumber int                         `json:"build_number"`
	Trigger     PipelineRunParameterTrigger `json:"trigger"`
	Nodes       []PipelineRunNodeParamView  `json:"nodes"`
}

type PipelineRunParameterTrigger struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Operator string `json:"operator"`
}

type PipelineRunNodeParamView struct {
	NodeID        string                     `json:"node_id"`
	NodeName      string                     `json:"node_name"`
	RuntimeParams []PipelineRuntimeParamView `json:"runtime_params"`
	DefaultParams []PipelineDefaultParamView `json:"default_params"`
}

type PipelineRuntimeParamView struct {
	Key    string      `json:"key"`
	Label  string      `json:"label"`
	Value  interface{} `json:"value"`
	Source string      `json:"source"`
}

type PipelineDefaultParamView struct {
	Key        string      `json:"key"`
	Label      string      `json:"label"`
	Value      interface{} `json:"value"`
	Overridden bool        `json:"overridden"`
}

type pipelineParameterConfig struct {
	Nodes []pipelineParameterNode `json:"nodes"`
}

type pipelineParameterNode struct {
	ID               string                           `json:"id"`
	NodeID           string                           `json:"node_id"`
	Name             string                           `json:"name"`
	NodeName         string                           `json:"node_name"`
	Type             string                           `json:"type"`
	TaskKey          string                           `json:"task_key"`
	DefinitionParams []models.PipelineDefinitionParam `json:"-"`
}

func (n *pipelineParameterNode) UnmarshalJSON(data []byte) error {
	type alias pipelineParameterNode
	aux := struct {
		alias
		Params json.RawMessage `json:"params"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*n = pipelineParameterNode(aux.alias)
	if len(aux.Params) > 0 && string(aux.Params) != "null" {
		_ = json.Unmarshal(aux.Params, &n.DefinitionParams)
	}
	return nil
}

func (u *PipelineQueryUseCase) GetPipelineParameterSchema(ctx context.Context, req GetPipelineParameterSchemaRequest) (PipelineParameterSchemaResult, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineParameterSchemaResult{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineParameterSchemaResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineParameterSchemaResult{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineParameterSchemaResult{}, err
	}
	pipeline, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID)
	if err != nil {
		return PipelineParameterSchemaResult{}, err
	}
	return buildPipelineParameterSchemaResult(*pipeline), nil
}

func (u *PipelineQueryUseCase) PreviewPipelineTriggerParameters(ctx context.Context, req PreviewPipelineTriggerParametersRequest) (PipelineTriggerPreviewResult, error) {
	schema, err := u.GetPipelineParameterSchema(ctx, GetPipelineParameterSchemaRequest{Actor: req.Actor, WorkspaceID: req.WorkspaceID, PipelineID: req.PipelineID})
	if err != nil {
		return PipelineTriggerPreviewResult{}, err
	}
	result := PipelineTriggerPreviewResult{
		WorkspaceID:      req.WorkspaceID,
		PipelineID:       req.PipelineID,
		CanTrigger:       true,
		InputPrompts:     schema.InputPrompts,
		ParameterTable:   schema.ParameterTable,
		Missing:          []PipelineParameterIssue{},
		Unknown:          []PipelineParameterIssue{},
		ExampleInputs:    schema.ExampleInputs,
		NormalizedInputs: normalizePipelinePreviewInputs(req.Inputs),
	}
	allowed := make(map[string]map[string]struct{}, len(schema.Nodes))
	for _, node := range schema.Nodes {
		paramKeys := make(map[string]struct{}, len(node.Params))
		for _, param := range node.Params {
			paramKeys[param.Key] = struct{}{}
			if _, ok := req.Inputs[node.NodeID][param.Key]; param.Required && !ok {
				result.Missing = append(result.Missing, PipelineParameterIssue{NodeID: node.NodeID, Key: param.Key, Reason: "required_runtime_input_missing"})
			}
		}
		allowed[node.NodeID] = paramKeys
	}
	for nodeID, params := range req.Inputs {
		allowedParams, ok := allowed[nodeID]
		if !ok {
			result.Unknown = append(result.Unknown, PipelineParameterIssue{NodeID: sanitizeText(nodeID), Reason: "unknown_node"})
			continue
		}
		for key := range params {
			if _, ok := allowedParams[key]; !ok {
				result.Unknown = append(result.Unknown, PipelineParameterIssue{NodeID: sanitizeText(nodeID), Key: sanitizeText(key), Reason: "unknown_param"})
			}
		}
	}
	sortPipelineParameterIssues(result.Missing)
	sortPipelineParameterIssues(result.Unknown)
	if len(result.Missing) > 0 || len(result.Unknown) > 0 {
		result.CanTrigger = false
	}
	result.Prompt = buildPipelineParameterPromptText(schema.InputPrompts, result.Unknown)
	return result, nil
}

func (u *PipelineQueryUseCase) GetPipelineRunParameters(ctx context.Context, req GetPipelineRunParametersRequest) (PipelineRunParameterView, error) {
	if err := validatePipelineQueryActor(req.Actor); err != nil {
		return PipelineRunParameterView{}, err
	}
	if u == nil || u.DB == nil {
		return PipelineRunParameterView{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "db is required"}
	}
	if req.PipelineID == 0 {
		return PipelineRunParameterView{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "pipeline_id is required"}
	}
	if req.RunID == 0 {
		return PipelineRunParameterView{}, ServiceError{Code: ErrorCodeInvalidArgument, Message: "run_id is required"}
	}
	if _, err := u.resolveWorkspace(ctx, req.Actor, req.WorkspaceID); err != nil {
		return PipelineRunParameterView{}, err
	}
	if _, err := u.loadPipeline(ctx, req.WorkspaceID, req.PipelineID); err != nil {
		return PipelineRunParameterView{}, err
	}
	run, err := u.loadPipelineRun(ctx, req.WorkspaceID, req.PipelineID, req.RunID)
	if err != nil {
		return PipelineRunParameterView{}, err
	}
	return buildPipelineRunParameterView(*run), nil
}

func buildPipelineParameterSchemaResult(pipeline models.Pipeline) PipelineParameterSchemaResult {
	result := PipelineParameterSchemaResult{
		WorkspaceID:    pipeline.WorkspaceID,
		PipelineID:     pipeline.ID,
		InputPrompts:   []PipelineParameterPrompt{},
		ParameterTable: []PipelineParameterTableRow{},
		Nodes:          []PipelineParameterNodeSchema{},
		ExampleInputs:  map[string]map[string]interface{}{},
	}
	config := parsePipelineParameterConfig(firstNonEmptyPipelineRaw(pipeline.Definition, pipeline.Config))
	for _, node := range config.Nodes {
		nodeID := sanitizeText(firstNonEmptyPipelineRaw(node.ID, node.NodeID))
		if nodeID == "" {
			continue
		}
		nodeSchema := PipelineParameterNodeSchema{
			NodeID:   nodeID,
			NodeName: sanitizeText(firstNonEmptyPipelineRaw(node.Name, node.NodeName)),
			TaskType: sanitizeText(firstNonEmptyPipelineRaw(node.TaskKey, node.Type)),
			Params:   []PipelineParameterSchema{},
		}
		for _, param := range node.DefinitionParams {
			if !param.IsFlexible || containsSensitiveKeyPart(param.Key) {
				continue
			}
			key := sanitizeText(strings.TrimSpace(param.Key))
			if key == "" {
				continue
			}
			paramSchema := PipelineParameterSchema{
				Key:        key,
				Label:      sanitizeText(param.Label),
				Value:      sanitizeAndDropSensitiveKeys(param.Value),
				IsFlexible: param.IsFlexible,
				Required:   param.IsFlexible,
			}
			nodeSchema.Params = append(nodeSchema.Params, paramSchema)
			result.InputPrompts = append(result.InputPrompts, buildPipelineParameterPrompt(nodeSchema, paramSchema))
			result.ParameterTable = append(result.ParameterTable, buildPipelineParameterTableRow(nodeSchema, paramSchema))
			if result.ExampleInputs[nodeID] == nil {
				result.ExampleInputs[nodeID] = map[string]interface{}{}
			}
			result.ExampleInputs[nodeID][key] = sanitizeAndDropSensitiveKeys(param.Value)
		}
		if len(nodeSchema.Params) > 0 {
			sort.Slice(nodeSchema.Params, func(i, j int) bool { return nodeSchema.Params[i].Key < nodeSchema.Params[j].Key })
			result.Nodes = append(result.Nodes, nodeSchema)
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].NodeID < result.Nodes[j].NodeID })
	sort.Slice(result.InputPrompts, func(i, j int) bool {
		if result.InputPrompts[i].NodeID != result.InputPrompts[j].NodeID {
			return result.InputPrompts[i].NodeID < result.InputPrompts[j].NodeID
		}
		return result.InputPrompts[i].Key < result.InputPrompts[j].Key
	})
	sort.Slice(result.ParameterTable, func(i, j int) bool {
		if result.ParameterTable[i].NodeID != result.ParameterTable[j].NodeID {
			return result.ParameterTable[i].NodeID < result.ParameterTable[j].NodeID
		}
		return result.ParameterTable[i].ParamName < result.ParameterTable[j].ParamName
	})
	result.Prompt = buildPipelineParameterPromptText(result.InputPrompts, nil)
	return result
}

func buildPipelineRunParameterView(run models.PipelineRun) PipelineRunParameterView {
	view := PipelineRunParameterView{
		RunID:       run.ID,
		PipelineID:  run.PipelineID,
		BuildNumber: run.BuildNumber,
		Trigger: PipelineRunParameterTrigger{
			Type:     sanitizeText(run.TriggerType),
			Source:   sanitizeText(run.TriggerSource),
			Operator: sanitizeText(run.TriggerUser),
		},
		Nodes: []PipelineRunNodeParamView{},
	}
	var snapshot models.PipelineRunConfigSnapshot
	runtimeInputs := map[string]map[string]interface{}{}
	if trimmed := strings.TrimSpace(run.RunConfig); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &snapshot); err == nil {
			runtimeInputs = snapshot.Inputs
			if strings.TrimSpace(snapshot.Trigger.Type) != "" {
				view.Trigger.Type = sanitizeText(snapshot.Trigger.Type)
			}
			if strings.TrimSpace(snapshot.Trigger.Source) != "" {
				view.Trigger.Source = sanitizeText(snapshot.Trigger.Source)
			}
			if strings.TrimSpace(snapshot.Trigger.Operator) != "" {
				view.Trigger.Operator = sanitizeText(snapshot.Trigger.Operator)
			}
		}
	}
	if len(runtimeInputs) == 0 {
		runtimeInputs = buildPipelineRuntimeInputsFromResolvedNodes(run.ResolvedNodes)
	}
	runtimeSource := firstNonEmptyPipelineRaw(view.Trigger.Source, view.Trigger.Type)

	nodeViews := map[string]*PipelineRunNodeParamView{}
	nodeOrder := []string{}
	ensureNode := func(nodeID, nodeName string) *PipelineRunNodeParamView {
		nodeID = sanitizeText(strings.TrimSpace(nodeID))
		if nodeID == "" {
			return nil
		}
		if existing, ok := nodeViews[nodeID]; ok {
			if existing.NodeName == "" {
				existing.NodeName = sanitizeText(nodeName)
			}
			return existing
		}
		nodeView := &PipelineRunNodeParamView{NodeID: nodeID, NodeName: sanitizeText(nodeName), RuntimeParams: []PipelineRuntimeParamView{}, DefaultParams: []PipelineDefaultParamView{}}
		nodeViews[nodeID] = nodeView
		nodeOrder = append(nodeOrder, nodeID)
		return nodeView
	}
	snapshotConfig := parsePipelineParameterConfig(run.PipelineSnapshot)
	for _, node := range snapshotConfig.Nodes {
		nodeView := ensureNode(firstNonEmptyPipelineRaw(node.ID, node.NodeID), firstNonEmptyPipelineRaw(node.Name, node.NodeName))
		if nodeView == nil {
			continue
		}
		for _, param := range node.DefinitionParams {
			if containsSensitiveKeyPart(param.Key) {
				continue
			}
			key := sanitizeText(strings.TrimSpace(param.Key))
			if key == "" {
				continue
			}
			nodeView.DefaultParams = append(nodeView.DefaultParams, PipelineDefaultParamView{Key: key, Label: sanitizeText(param.Label), Value: sanitizeAndDropSensitiveKeys(param.Value)})
		}
		sort.Slice(nodeView.DefaultParams, func(i, j int) bool { return nodeView.DefaultParams[i].Key < nodeView.DefaultParams[j].Key })
	}
	for nodeID := range runtimeInputs {
		if _, ok := nodeViews[nodeID]; !ok {
			ensureNode(nodeID, "")
		}
	}
	for _, nodeID := range nodeOrder {
		nodeView := nodeViews[nodeID]
		inputs := runtimeInputs[nodeID]
		for key, value := range inputs {
			if containsSensitiveKeyPart(key) {
				continue
			}
			runtimeParam := PipelineRuntimeParamView{Key: sanitizeText(key), Value: sanitizeAndDropSensitiveKeys(value), Source: sanitizeText(runtimeSource)}
			for i := range nodeView.DefaultParams {
				if nodeView.DefaultParams[i].Key == runtimeParam.Key {
					nodeView.DefaultParams[i].Overridden = true
					runtimeParam.Label = nodeView.DefaultParams[i].Label
				}
			}
			nodeView.RuntimeParams = append(nodeView.RuntimeParams, runtimeParam)
		}
		sort.Slice(nodeView.RuntimeParams, func(i, j int) bool { return nodeView.RuntimeParams[i].Key < nodeView.RuntimeParams[j].Key })
		view.Nodes = append(view.Nodes, *nodeView)
	}
	return view
}

func parsePipelineParameterConfig(raw string) pipelineParameterConfig {
	var config pipelineParameterConfig
	if strings.TrimSpace(raw) == "" {
		return config
	}
	_ = json.Unmarshal([]byte(raw), &config)
	return config
}

func buildPipelineRuntimeInputsFromResolvedNodes(raw string) map[string]map[string]interface{} {
	var nodes []map[string]interface{}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &nodes) != nil {
		return nil
	}
	result := map[string]map[string]interface{}{}
	for _, node := range nodes {
		nodeID := sanitizeText(toString(node["node_id"]))
		if nodeID == "" {
			continue
		}
		resolved, ok := node["resolved_inputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for key, value := range resolved {
			if containsSensitiveKeyPart(key) {
				continue
			}
			if result[nodeID] == nil {
				result[nodeID] = map[string]interface{}{}
			}
			result[nodeID][sanitizeText(key)] = sanitizeAndDropSensitiveKeys(value)
		}
	}
	return result
}

func normalizePipelinePreviewInputs(inputs map[string]map[string]any) map[string]map[string]interface{} {
	if len(inputs) == 0 {
		return nil
	}
	result := make(map[string]map[string]interface{}, len(inputs))
	for nodeID, params := range inputs {
		cleanNodeID := sanitizeText(nodeID)
		if cleanNodeID == "" {
			continue
		}
		for key, value := range params {
			if containsSensitiveKeyPart(key) {
				continue
			}
			if result[cleanNodeID] == nil {
				result[cleanNodeID] = map[string]interface{}{}
			}
			result[cleanNodeID][sanitizeText(key)] = sanitizeAndDropSensitiveKeys(value)
		}
	}
	return result
}

func sortPipelineParameterIssues(items []PipelineParameterIssue) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].NodeID != items[j].NodeID {
			return items[i].NodeID < items[j].NodeID
		}
		return items[i].Key < items[j].Key
	})
}

func firstNonEmptyPipelineRaw(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildPipelineParameterPrompt(node PipelineParameterNodeSchema, param PipelineParameterSchema) PipelineParameterPrompt {
	label := firstNonEmptyPipelineRaw(param.Label, param.Key)
	inputPath := fmt.Sprintf("inputs.%s.%s", node.NodeID, param.Key)
	return PipelineParameterPrompt{
		NodeID:       node.NodeID,
		NodeName:     node.NodeName,
		TaskType:     node.TaskType,
		Key:          param.Key,
		Label:        param.Label,
		CurrentValue: param.Value,
		InputPath:    inputPath,
		Message:      fmt.Sprintf("请在参数表中让用户选择：沿用当前值或提供新值。参数 %s 属于任务 %s，最终值应放入 %s。", label, firstNonEmptyPipelineRaw(node.NodeName, node.NodeID), inputPath),
	}
}

func buildPipelineParameterTableRow(node PipelineParameterNodeSchema, param PipelineParameterSchema) PipelineParameterTableRow {
	return PipelineParameterTableRow{
		TaskName:           node.NodeName,
		TaskType:           node.TaskType,
		NodeID:             node.NodeID,
		ParamName:          param.Key,
		ParamLabel:         param.Label,
		ValueType:          inferPipelineParameterValueType(param.Value),
		DefaultValue:       param.Value,
		InputPath:          fmt.Sprintf("inputs.%s.%s", node.NodeID, param.Key),
		Required:           param.Required,
		InteractionOptions: []string{"use_current_value", "override_value"},
	}
}

func buildPipelineParameterPromptText(prompts []PipelineParameterPrompt, unknown []PipelineParameterIssue) string {
	if len(prompts) == 0 && len(unknown) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if len(prompts) > 0 {
		parts = append(parts, fmt.Sprintf("这条流水线需要用户交互确认 %d 个参数；不得自行沿用当前值。必须先用 parameter_table 以表格展示参数名、值类型、当前/默认值、所属任务和 input_path，让用户逐项选择 use_current_value 或 override_value，再按 nested inputs 触发流水线。", len(prompts)))
	}
	if len(unknown) > 0 {
		parts = append(parts, fmt.Sprintf("检测到 %d 个未知节点或参数，请先修正 inputs。", len(unknown)))
	}
	return strings.Join(parts, " ")
}

func inferPipelineParameterValueType(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return "number"
	case []interface{}, []string:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}
