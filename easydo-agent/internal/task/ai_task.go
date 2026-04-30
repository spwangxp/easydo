package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

type aiTaskPayload struct {
	AISessionID      uint64                 `json:"ai_session_id"`
	Scenario         string                 `json:"scenario"`
	RuntimeProfileID uint64                 `json:"runtime_profile_id"`
	ProviderID       uint64                 `json:"provider_id"`
	ModelID          uint64                 `json:"model_id"`
	BindingID        uint64                 `json:"binding_id"`
	AgentID          uint64                 `json:"agent_id"`
	Request          map[string]interface{} `json:"request"`
}

type aiTaskStructuredResult struct {
	Summary      string                   `json:"summary"`
	QualityScore float64                  `json:"quality_score,omitempty"`
	Issues       []map[string]interface{} `json:"issues,omitempty"`
	IssuesCount  int                      `json:"issues_count,omitempty"`
	Defects      []map[string]interface{} `json:"defects,omitempty"`
	DefectCount  int                      `json:"defect_count,omitempty"`
	Suggestions  []string                 `json:"suggestions,omitempty"`
}

func IsAITaskPayload(taskType string, params map[string]interface{}) bool {
	if params != nil {
		if strings.TrimSpace(fmt.Sprint(params["mode"])) == "ai-task" {
			return true
		}
		scenario := strings.TrimSpace(fmt.Sprint(params["scenario"]))
		if scenario == "mr_quality_check" || scenario == "requirement_defect_assistant" {
			return true
		}
	}
	switch strings.TrimSpace(taskType) {
	case "mr_quality_check", "requirement_defect_assistant":
		return true
	default:
		return false
	}
}

func isAITaskParams(params TaskParams) bool {
	return IsAITaskPayload(params.TaskType, params.Params)
}

func buildAITaskPrompt(payload aiTaskPayload) string {
	requestJSON, _ := sonic.MarshalString(payload.Request)
	languageInstruction := buildAIOutputLanguageInstruction(payload.Request)
	switch strings.TrimSpace(payload.Scenario) {
	case "mr_quality_check":
		return "You are an MR quality review assistant. Analyze the provided merge request context and return JSON only with fields: summary(string), quality_score(number 0-100), issues(array of {severity,title,description,suggestion}), issues_count(number)." + languageInstruction + " Input: " + requestJSON
	case "requirement_defect_assistant":
		return "You are a requirement defect analysis assistant. Analyze the provided requirement context and return JSON only with fields: summary(string), defects(array of {severity,title,description,suggestion}), defect_count(number), suggestions(array of string)." + languageInstruction + " Input: " + requestJSON
	default:
		return "Return JSON only for this AI task request." + languageInstruction + " Input: " + requestJSON
	}
}

func buildAIOutputLanguageInstruction(request map[string]interface{}) string {
	if request == nil {
		return ""
	}
	language := strings.TrimSpace(fmt.Sprint(request["output_language"]))
	if language == "" {
		return ""
	}
	return " Use " + language + " for all human-readable text fields in the JSON response."
}

func fallbackAITaskStructuredResult(payload aiTaskPayload) aiTaskStructuredResult {
	inputText := strings.TrimSpace(fmt.Sprint(payload.Request["input_text"]))
	shortSummary := inputText
	if len(shortSummary) > 160 {
		shortSummary = shortSummary[:160]
	}
	if shortSummary == "" {
		shortSummary = "No input text provided"
	}
	switch payload.Scenario {
	case "mr_quality_check":
		issues := []map[string]interface{}{}
		if strings.Contains(strings.ToLower(inputText), "todo") {
			issues = append(issues, map[string]interface{}{
				"severity":    "medium",
				"title":       "Contains TODO markers",
				"description": "The MR content still contains TODO markers that may indicate incomplete work.",
				"suggestion":  "Resolve or explicitly track remaining TODO items before merge.",
			})
		}
		return aiTaskStructuredResult{
			Summary:      shortSummary,
			QualityScore: maxFloat(0, 100-float64(len(issues))*15),
			Issues:       issues,
			IssuesCount:  len(issues),
		}
	case "requirement_defect_assistant":
		defects := []map[string]interface{}{}
		if !strings.ContainsAny(inputText, "0123456789") {
			defects = append(defects, map[string]interface{}{
				"severity":    "medium",
				"title":       "No quantifiable acceptance criteria",
				"description": "The requirement text does not appear to include measurable constraints or acceptance criteria.",
				"suggestion":  "Add explicit acceptance criteria, constraints, or completion conditions.",
			})
		}
		return aiTaskStructuredResult{
			Summary:     shortSummary,
			Defects:     defects,
			DefectCount: len(defects),
			Suggestions: []string{"Clarify boundaries, acceptance criteria, and error scenarios."},
		}
	default:
		return aiTaskStructuredResult{Summary: shortSummary}
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (e *Executor) executeAITask(ctx context.Context, params TaskParams, callback LogCallback) *Result {
	startTime := time.Now()
	payload := aiTaskPayload{}
	if params.Params != nil {
		if raw, err := sonic.Marshal(params.Params); err == nil {
			_ = sonic.Unmarshal(raw, &payload)
		}
	}
	if strings.TrimSpace(payload.Scenario) == "" {
		payload.Scenario = strings.TrimSpace(params.TaskType)
	}
	if payload.Request == nil {
		payload.Request = make(map[string]interface{})
	}
	if len(payload.Request) == 0 && params.Params != nil {
		payload.Request = params.Params
	}
	prompt := buildAITaskPrompt(payload)
	if callback != nil {
		callback(params.TaskID, "info", fmt.Sprintf("starting ai-task scenario=%s ai_session_id=%d", payload.Scenario, payload.AISessionID), "system", 1)
	}

	structured := fallbackAITaskStructuredResult(payload)
	apiKey := strings.TrimSpace(params.EnvVars["OPENAI_API_KEY"])
	baseURL := strings.TrimSpace(params.EnvVars["OPENAI_BASE_URL"])
	modelName := strings.TrimSpace(params.EnvVars["OPENAI_MODEL"])
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}
	if apiKey != "" {
		model, err := openaiModel.NewChatModel(ctx, &openaiModel.ChatModelConfig{
			Model:   modelName,
			APIKey:  apiKey,
			BaseURL: baseURL,
		})
		if err == nil {
			resp, genErr := model.Generate(ctx, []*schema.Message{{Role: schema.System, Content: "Return JSON only."}, {Role: schema.User, Content: prompt}})
			if genErr == nil {
				if unmarshalErr := sonic.UnmarshalString(resp.Content, &structured); unmarshalErr == nil {
					if callback != nil {
						callback(params.TaskID, "info", "ai-task model response parsed successfully", "system", 2)
					}
				} else if callback != nil {
					callback(params.TaskID, "warn", "ai-task response was not valid JSON, falling back to deterministic parser", "system", 2)
				}
			}
		}
	}

	structuredMap := map[string]interface{}{}
	if raw, err := sonic.Marshal(structured); err == nil {
		_ = sonic.Unmarshal(raw, &structuredMap)
	}
	stdout, _ := sonic.MarshalString(structured)
	return &Result{
		ExitCode:         0,
		Stdout:           stdout,
		Stderr:           "",
		Error:            "",
		Duration:         time.Since(startTime),
		StructuredOutput: structuredMap,
	}
}
