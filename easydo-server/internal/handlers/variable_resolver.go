package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
)

// VariableResolver handles variable substitution in pipeline configurations
type VariableResolver struct {
	// Task outputs from completed tasks
	taskOutputs map[string]map[string]interface{}
	// Global environment variables
	envVars map[string]string
	// Input parameters
	inputs map[string]interface{}
	// Secrets (loaded from database)
	secrets map[string]string
}

// NewVariableResolver creates a new variable resolver
func NewVariableResolver() *VariableResolver {
	return &VariableResolver{
		taskOutputs: make(map[string]map[string]interface{}),
		envVars:     make(map[string]string),
		inputs:      make(map[string]interface{}),
		secrets:     make(map[string]string),
	}
}

// SetTaskOutput sets the output of a completed task
func (r *VariableResolver) SetTaskOutput(nodeID string, outputs map[string]interface{}) {
	if r.taskOutputs == nil {
		r.taskOutputs = make(map[string]map[string]interface{})
	}
	r.taskOutputs[nodeID] = outputs
}

// SetEnvVars sets global environment variables
func (r *VariableResolver) SetEnvVars(envVars map[string]string) {
	r.envVars = envVars
}

// SetInputs sets input parameters
func (r *VariableResolver) SetInputs(inputs map[string]interface{}) {
	r.inputs = inputs
}

// SetSecrets sets secret values
func (r *VariableResolver) SetSecrets(secrets map[string]string) {
	r.secrets = secrets
}

// ResolveVariables replaces all variable references in a string with their values
func (r *VariableResolver) ResolveVariables(configStr string) (string, error) {
	if configStr == "" {
		return "", nil
	}

	// Pattern to match ${...} references
	pattern := `\$\{([^}]+)\}`
	re := regexp.MustCompile(pattern)

	result := configStr
	matches := re.FindAllStringSubmatch(configStr, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		fullMatch := match[0]
		varRef := match[1]

		// Determine the type of variable
		value, err := r.resolveVariable(varRef)
		if err != nil {
			// If variable cannot be resolved, keep the original reference
			continue
		}

		result = strings.Replace(result, fullMatch, value, 1)
	}

	return result, nil
}

// resolveVariable resolves a single variable reference
func (r *VariableResolver) resolveVariable(varRef string) (string, error) {
	varRef = strings.TrimSpace(varRef)

	// Check for different variable types
	switch {
	case strings.HasPrefix(varRef, "outputs."):
		return r.resolveOutputVariable(varRef)
	case strings.HasPrefix(varRef, "env."):
		return r.resolveEnvVariable(varRef)
	case strings.HasPrefix(varRef, "inputs."):
		return r.resolveInputVariable(varRef)
	case strings.HasPrefix(varRef, "secrets."):
		return r.resolveSecretVariable(varRef)
	default:
		// Try to resolve as a simple output variable (node_id.field format)
		return r.resolveOutputVariable("outputs." + varRef)
	}
}

// resolveOutputVariable resolves ${outputs.<node_id>.<field>} variables
func (r *VariableResolver) resolveOutputVariable(varRef string) (string, error) {
	// Extract node ID and field from outputs.<node_id>.<field>
	parts := strings.SplitN(varRef, ".", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid output variable format: %s", varRef)
	}

	nodeID := parts[1]
	field := parts[2]

	outputs, exists := r.taskOutputs[nodeID]
	if !exists {
		return "", fmt.Errorf("output not found for node: %s", nodeID)
	}

	value, exists := outputs[field]
	if !exists {
		return "", fmt.Errorf("output field not found: %s.%s", nodeID, field)
	}

	return convertToString(value), nil
}

// resolveEnvVariable resolves ${env.<variable>} variables
func (r *VariableResolver) resolveEnvVariable(varRef string) (string, error) {
	// Extract variable name from env.<variable>
	parts := strings.SplitN(varRef, ".", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid env variable format: %s", varRef)
	}

	varName := parts[1]
	value, exists := r.envVars[varName]
	if !exists {
		return "", fmt.Errorf("environment variable not found: %s", varName)
	}

	return value, nil
}

// resolveInputVariable resolves ${inputs.<key>} variables
func (r *VariableResolver) resolveInputVariable(varRef string) (string, error) {
	// Extract key from inputs.<key>
	parts := strings.SplitN(varRef, ".", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid input variable format: %s", varRef)
	}

	key := parts[1]
	value, exists := r.inputs[key]
	if !exists {
		return "", fmt.Errorf("input not found: %s", key)
	}

	return convertToString(value), nil
}

// resolveSecretVariable resolves ${secrets.<key>} variables
func (r *VariableResolver) resolveSecretVariable(varRef string) (string, error) {
	// Extract key from secrets.<key>
	parts := strings.SplitN(varRef, ".", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid secret variable format: %s", varRef)
	}

	key := parts[1]
	value, exists := r.secrets[key]
	if !exists {
		return "", fmt.Errorf("secret not found: %s", key)
	}

	return value, nil
}

// convertToString converts a value to string
func convertToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return fmt.Sprintf("%t", v)
	case []int:
		str := "["
		for i, val := range v {
			if i > 0 {
				str += " "
			}
			str += fmt.Sprintf("%d", val)
		}
		str += "]"
		return str
	default:
		if str, err := json.Marshal(v); err == nil {
			return string(str)
		}
		return fmt.Sprintf("%v", v)
	}
}

// OutputExtractor extracts custom outputs from task execution results
type OutputExtractor struct {
	Extractions []OutputExtractionConfig `json:"output_extraction"`
}

// OutputExtractionConfig defines how to extract a custom output field
type OutputExtractionConfig struct {
	Field    string `json:"field"`
	Regex    string `json:"regex"`
	Source   string `json:"source"`   // stdout, stderr
	Required bool   `json:"required"` // Whether extraction is required
}

// ExtractOutputs extracts custom outputs from stdout/stderr
func (e *OutputExtractor) ExtractOutputs(stdout, stderr string) (map[string]interface{}, error) {
	outputs := make(map[string]interface{})

	for _, extraction := range e.Extractions {
		var content string
		switch extraction.Source {
		case "stderr":
			content = stderr
		case "stdout":
			fallthrough
		default:
			content = stdout
		}

		value, err := extractValue(content, extraction.Regex)
		if err != nil {
			if extraction.Required {
				return nil, fmt.Errorf("failed to extract required output %s: %w", extraction.Field, err)
			}
			// Non-required extractions can be skipped
			continue
		}

		outputs[extraction.Field] = value
	}

	return outputs, nil
}

// extractValue extracts a value from content using regex
func extractValue(content, pattern string) (string, error) {
	if content == "" || pattern == "" {
		return "", fmt.Errorf("empty content or pattern")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	matches := re.FindStringSubmatch(content)
	if len(matches) < 2 {
		return "", fmt.Errorf("pattern not found in content")
	}

	// Return the first captured group
	return strings.TrimSpace(matches[1]), nil
}

// BuildGlobalEnvVars builds global environment variables for pipeline execution
func BuildGlobalEnvVars(pipeline *models.Pipeline, run *models.PipelineRun) map[string]string {
	envVars := map[string]string{
		"CI":                   "true",
		"EASYDO":               "true",
		"PIPELINE_ID":          fmt.Sprintf("%d", pipeline.ID),
		"PIPELINE_NAME":        pipeline.Name,
		"PIPELINE_DESCRIPTION": pipeline.Description,
		"RUN_ID":               fmt.Sprintf("%d", run.ID),
		"BUILD_NUMBER":         fmt.Sprintf("%d", run.BuildNumber),
		"BUILD_TAG":            fmt.Sprintf("build-%d", run.ID),
		"BUILD_URL":            fmt.Sprintf("http://localhost:8080/pipelines/%d/runs/%d", pipeline.ID, run.ID),
	}

	// Set current time for build date/time
	now := time.Now()

	envVars["BUILD_DATE"] = now.Format("2006-01-02")
	envVars["BUILD_TIME"] = now.Format("15:04:05")
	envVars["BUILD_TIMESTAMP"] = fmt.Sprintf("%d", now.Unix())

	return envVars
}

// ResolveNodeConfig resolves all variable references in a node configuration
func (r *VariableResolver) ResolveNodeConfig(config map[string]interface{}) (map[string]interface{}, error) {
	resolvedConfig := make(map[string]interface{})

	for key, value := range config {
		strValue, ok := value.(string)
		if ok {
			resolved, err := r.ResolveVariables(strValue)
			if err != nil {
				// If resolution fails, keep the original value
				resolvedConfig[key] = value
			} else {
				resolvedConfig[key] = resolved
			}
		} else if mapValue, ok := value.(map[string]interface{}); ok {
			// Recursively resolve nested maps
			resolved, err := r.ResolveNodeConfig(mapValue)
			if err != nil {
				resolvedConfig[key] = value
			} else {
				resolvedConfig[key] = resolved
			}
		} else if arrValue, ok := value.([]interface{}); ok {
			// Resolve arrays
			resolvedArray := make([]interface{}, len(arrValue))
			for i, item := range arrValue {
				if strItem, ok := item.(string); ok {
					resolvedItem, _ := r.ResolveVariables(strItem)
					resolvedArray[i] = resolvedItem
				} else {
					resolvedArray[i] = item
				}
			}
			resolvedConfig[key] = resolvedArray
		} else {
			resolvedConfig[key] = value
		}
	}

	return resolvedConfig, nil
}
