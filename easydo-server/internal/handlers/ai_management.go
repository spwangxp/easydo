package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AIProviderHandler struct {
	DB *gorm.DB
}

var aiProviderDiscoveryHTTPClient = &http.Client{Timeout: 20 * time.Second}

func NewAIProviderHandler() *AIProviderHandler {
	return &AIProviderHandler{DB: models.DB}
}

type aiProviderRequest struct {
	Name         string         `json:"name" binding:"required"`
	Description  string         `json:"description"`
	ProviderType string         `json:"provider_type" binding:"required"`
	BaseURL      string         `json:"base_url"`
	CredentialID uint64         `json:"credential_id"`
	HeadersJSON  map[string]any `json:"headers_json"`
	SettingsJSON map[string]any `json:"settings_json"`
	MetadataJSON map[string]any `json:"metadata_json"`
	Status       string         `json:"status"`
}

type aiModelBindingRequest struct {
	ModelID                uint64         `json:"model_id" binding:"required"`
	ProviderModelKey       string         `json:"provider_model_key"`
	SettingsJSON           map[string]any `json:"settings_json"`
	MetadataJSON           map[string]any `json:"metadata_json"`
	ContextWindowTokens    *int64         `json:"context_window_tokens"`
	MaxOutputTokens        *int64         `json:"max_output_tokens"`
	SupportsToolUse        *bool          `json:"supports_tool_use"`
	SupportsStreaming      *bool          `json:"supports_streaming"`
	CapabilitySource       string         `json:"capability_source"`
	Status                 string         `json:"status"`
}

type aiProviderModelCandidate struct {
	ProviderModelKey    string         `json:"provider_model_key"`
	ProviderDisplayName string         `json:"provider_display_name"`
	ModelName           string         `json:"model_name"`
	ModelKind           string         `json:"model_kind"`
	ModelFamily         string         `json:"model_family"`
	SourceModelID       string         `json:"source_model_id"`
	Modalities          []string       `json:"modalities"`
	Capabilities        []string       `json:"capabilities"`
	ContextWindow       int64          `json:"context_window,omitempty"`
	MaxOutputTokens     int64          `json:"max_output_tokens,omitempty"`
	Pricing             map[string]any `json:"pricing,omitempty"`
	Recommended         bool           `json:"recommended"`
	Raw                 map[string]any `json:"raw,omitempty"`
}

type aiProviderDiscoveryResult struct {
	Endpoint   string
	StatusCode int
	LatencyMS  int64
	Models     []aiProviderModelCandidate
}

func decodeAIModelBindingRequest(c *gin.Context) (aiModelBindingRequest, bool) {
	var req aiModelBindingRequest
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return req, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return req, false
	}
	if _, exists := raw["capabilities_json"]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "capabilities_json 已废弃，请改用 Agent Profile 定义"})
		return req, false
	}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return req, false
	}
	if req.ModelID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "model_id 无效"})
		return req, false
	}
	return req, true
}


type modelBindingCapabilitySnapshot struct {
	ContextWindowTokens    uint64
	MaxOutputTokens        *uint64
	SupportsToolUse        bool
	SupportsStreaming      bool
	CapabilitySource       string
	CapabilityCheckedAt    time.Time
	CapabilitySnapshotHash string
}

func freezeModelBindingCapability(req *aiModelBindingRequest, model models.AIModelCatalog) (modelBindingCapabilitySnapshot, error) {
	if req == nil {
		return modelBindingCapabilitySnapshot{}, fmt.Errorf("model binding request is required")
	}
	metadata := map[string]any{}
	if req.MetadataJSON != nil {
		for key, value := range req.MetadataJSON {
			metadata[key] = value
		}
	}
	contextWindow := firstPositiveInt64(
		valueOrZero(req.ContextWindowTokens),
		firstObjectInt64(metadata, "context_window_tokens", "context_window", "contextWindow", "context_length"),
		model.ContextWindow,
	)
	if contextWindow <= 0 {
		return modelBindingCapabilitySnapshot{}, fmt.Errorf("context_window_tokens is required for model binding capability snapshot")
	}
	maxOutput := firstPositiveInt64(
		valueOrZero(req.MaxOutputTokens),
		firstObjectInt64(metadata, "max_output_tokens", "maxOutputTokens", "max_tokens"),
	)
	supportsToolUse := false
	if req.SupportsToolUse != nil {
		supportsToolUse = *req.SupportsToolUse
	} else {
		supportsToolUse = firstObjectBool(metadata, "supports_tool_use", "supportsToolUse") ||
			stringListContains(stringListFromAny(metadata["capabilities"]), "tool", "tools", "function_calling")
	}
	supportsStreaming := true
	if req.SupportsStreaming != nil {
		supportsStreaming = *req.SupportsStreaming
	} else if value, ok := metadataBool(metadata, "supports_streaming", "supportsStreaming"); ok {
		supportsStreaming = value
	}
	source := strings.TrimSpace(req.CapabilitySource)
	if source == "" {
		source = firstObjectString(metadata, "capability_source", "capabilitySource", "source")
	}
	if source == "" {
		if firstObjectInt64(metadata, "context_window_tokens", "context_window", "contextWindow", "context_length") > 0 {
			source = "provider_api"
		} else if model.ContextWindow > 0 {
			source = "catalog"
		} else {
			source = "manual_override"
		}
	}
	checkedAt := time.Now().UTC()
	var maxOutputPtr *uint64
	if maxOutput > 0 {
		v := uint64(maxOutput)
		maxOutputPtr = &v
	}
	contextTokens := uint64(contextWindow)
	hash := hashModelBindingCapability(contextTokens, maxOutputPtr, supportsToolUse, supportsStreaming, source)
	// keep metadata aligned with frozen columns for older consumers
	metadata["context_window"] = contextTokens
	metadata["context_window_tokens"] = contextTokens
	if maxOutputPtr != nil {
		metadata["max_output_tokens"] = *maxOutputPtr
	}
	metadata["supports_tool_use"] = supportsToolUse
	metadata["supports_streaming"] = supportsStreaming
	metadata["capability_source"] = source
	metadata["capability_checked_at"] = checkedAt.Format(time.RFC3339Nano)
	metadata["capability_snapshot_hash"] = hash
	req.MetadataJSON = metadata
	return modelBindingCapabilitySnapshot{
		ContextWindowTokens:    contextTokens,
		MaxOutputTokens:        maxOutputPtr,
		SupportsToolUse:        supportsToolUse,
		SupportsStreaming:      supportsStreaming,
		CapabilitySource:       source,
		CapabilityCheckedAt:    checkedAt,
		CapabilitySnapshotHash: hash,
	}, nil
}

func hashModelBindingCapability(contextWindow uint64, maxOutput *uint64, supportsToolUse, supportsStreaming bool, source string) string {
	maxOutputValue := uint64(0)
	if maxOutput != nil {
		maxOutputValue = *maxOutput
	}
	payload := fmt.Sprintf("cw=%d;mo=%d;tool=%t;stream=%t;source=%s", contextWindow, maxOutputValue, supportsToolUse, supportsStreaming, source)
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func metadataBool(metadata map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if raw, ok := metadata[key]; ok {
			switch value := raw.(type) {
			case bool:
				return value, true
			case string:
				trimmed := strings.TrimSpace(strings.ToLower(value))
				if trimmed == "true" || trimmed == "1" || trimmed == "yes" {
					return true, true
				}
				if trimmed == "false" || trimmed == "0" || trimmed == "no" {
					return false, true
				}
			case float64:
				return value != 0, true
			case int:
				return value != 0, true
			case int64:
				return value != 0, true
			}
		}
	}
	return false, false
}

func firstObjectBool(metadata map[string]any, keys ...string) bool {
	value, ok := metadataBool(metadata, keys...)
	return ok && value
}

func stringListContains(values []string, candidates ...string) bool {
	set := map[string]struct{}{}
	for _, value := range values {
		set[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := set[strings.ToLower(candidate)]; ok {
			return true
		}
	}
	return false
}

func ensureProviderExists(db *gorm.DB, workspaceID, providerID uint64) error {
	var provider models.AIProvider
	return db.Where("workspace_id = ? AND id = ?", workspaceID, providerID).First(&provider).Error
}

func ensureModelExists(db *gorm.DB, modelID uint64) error {
	var model models.AIModelCatalog
	return db.First(&model, modelID).Error
}

func marshalJSONOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func optionalAIProviderCredentialID(id uint64) *uint64 {
	if id == 0 {
		return nil
	}
	return &id
}

func aiGovernanceContext(c *gin.Context, db *gorm.DB) GovernanceContext {
	ctx := BuildGovernanceContext(c)
	if db == nil || ctx.WorkspaceID == 0 {
		return ctx
	}
	return governanceContextForWorkspace(db, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole)
}

func requireAIProviderWrite(c *gin.Context, db *gorm.DB) (uint64, uint64, bool) {
	ctx := aiGovernanceContext(c, db)
	if ctx.WorkspaceID == 0 || ctx.UserID == 0 || !canWriteAIProvider(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "当前工作空间角色无权管理 AI Provider"})
		return 0, 0, false
	}
	return ctx.WorkspaceID, ctx.UserID, true
}

func canWriteAIProvider(ctx GovernanceContext) bool {
	if isAdminRole(ctx.SystemRole) {
		return true
	}
	return middleware.WorkspaceRoleAtLeast(ctx.WorkspaceRole, models.WorkspaceRoleDeveloper)
}

func (h *AIProviderHandler) TestConnection(c *gin.Context) {
	workspaceID, userID, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	_, role := getRequestUser(c)
	var req aiProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	result, err := h.fetchProviderModels(c, workspaceID, userID, role, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	sampleSize := len(result.Models)
	if sampleSize > 5 {
		sampleSize = 5
	}
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{
			"ok":            true,
			"endpoint":      result.Endpoint,
			"status_code":   result.StatusCode,
			"latency_ms":    result.LatencyMS,
			"models_count":  len(result.Models),
			"sample_models": result.Models[:sampleSize],
		},
	})
}

func (h *AIProviderHandler) DiscoverModels(c *gin.Context) {
	workspaceID, userID, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	_, role := getRequestUser(c)
	var req aiProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	result, err := h.fetchProviderModels(c, workspaceID, userID, role, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{
			"endpoint":    result.Endpoint,
			"status_code": result.StatusCode,
			"latency_ms":  result.LatencyMS,
			"count":       len(result.Models),
			"models":      result.Models,
		},
	})
}

func (h *AIProviderHandler) fetchProviderModels(c *gin.Context, workspaceID, userID uint64, role string, req aiProviderRequest) (aiProviderDiscoveryResult, error) {
	if strings.TrimSpace(req.ProviderType) == "" {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Provider 类型不能为空")
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Base URL 不能为空")
	}
	if req.CredentialID == 0 {
		return aiProviderDiscoveryResult{}, fmt.Errorf("必须选择 Provider Credential")
	}
	var credential models.Credential
	if err := h.DB.First(&credential, req.CredentialID).Error; err != nil {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Credential 不存在")
	}
	if credential.WorkspaceID != workspaceID || !canReadCredentialValue(h.DB, &credential, userID, role) {
		return aiProviderDiscoveryResult{}, fmt.Errorf("无权使用该 Credential")
	}
	if !credential.IsUsable() {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Credential 当前不可用")
	}
	payload, err := services.NewCredentialEncryptionService().DecryptCredentialData(credential.EncryptedPayload)
	if err != nil {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Credential 解密失败")
	}
	modelsEndpoint := stringSetting(req.SettingsJSON, "models_endpoint", "/models")
	modelsURL, err := buildAIProviderModelsURL(req.BaseURL, modelsEndpoint)
	if err != nil {
		return aiProviderDiscoveryResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, modelsURL, nil)
	if err != nil {
		return aiProviderDiscoveryResult{}, fmt.Errorf("构造 Provider 请求失败")
	}
	applyAIProviderHeaders(httpReq, req.HeadersJSON, payload)
	startedAt := time.Now()
	resp, err := aiProviderDiscoveryHTTPClient.Do(httpReq)
	if err != nil {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Provider 连接失败：%v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return aiProviderDiscoveryResult{}, fmt.Errorf("读取 Provider 响应失败")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return aiProviderDiscoveryResult{}, fmt.Errorf("Provider 返回状态码 %d", resp.StatusCode)
	}
	candidates, err := normalizeAIProviderModelCandidates(body)
	if err != nil {
		return aiProviderDiscoveryResult{}, err
	}
	return aiProviderDiscoveryResult{
		Endpoint:   modelsURL,
		StatusCode: resp.StatusCode,
		LatencyMS:  time.Since(startedAt).Milliseconds(),
		Models:     candidates,
	}, nil
}

func buildAIProviderModelsURL(baseURL string, modelsEndpoint string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("Base URL 不能为空")
	}
	endpoint := strings.TrimSpace(modelsEndpoint)
	if endpoint == "" {
		endpoint = "/models"
	}
	if parsedEndpoint, err := url.Parse(endpoint); err == nil && parsedEndpoint.IsAbs() {
		return endpoint, nil
	}
	joined := base + "/" + strings.TrimLeft(endpoint, "/")
	parsed, err := url.Parse(joined)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("Models Endpoint 无效")
	}
	return parsed.String(), nil
}

func applyAIProviderHeaders(req *http.Request, headers map[string]any, credentialPayload map[string]any) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "EasyDo-AI-Provider-Discovery/1.0")
	for key, value := range headers {
		headerName := strings.TrimSpace(key)
		headerValue := strings.TrimSpace(fmt.Sprintf("%v", value))
		if headerName == "" || headerValue == "" || strings.EqualFold(headerName, "Authorization") {
			continue
		}
		req.Header.Set(headerName, headerValue)
	}
	secret := firstPayloadValue(credentialPayload, "token", "access_token", "api_key", "key", "password")
	if secret == "" {
		return
	}
	headerName := firstPayloadValue(credentialPayload, "auth_header", "header_name", "api_key_header")
	if headerName != "" {
		req.Header.Set(headerName, secret)
		return
	}
	tokenType := firstPayloadValue(credentialPayload, "token_type", "auth_scheme")
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if strings.EqualFold(tokenType, "raw") || strings.EqualFold(tokenType, "none") || strings.EqualFold(tokenType, "no_prefix") {
		req.Header.Set("Authorization", secret)
		return
	}
	req.Header.Set("Authorization", strings.TrimSpace(tokenType+" "+secret))
}

func normalizeAIProviderModelCandidates(body []byte) ([]aiProviderModelCandidate, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("Provider 模型响应不是有效 JSON")
	}
	items := extractAIProviderModelItems(payload)
	candidates := make([]aiProviderModelCandidate, 0, len(items))
	for index, item := range items {
		candidate := buildAIProviderModelCandidate(item, index == 0)
		if candidate.ProviderModelKey == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func extractAIProviderModelItems(payload any) []map[string]any {
	switch value := payload.(type) {
	case []any:
		return normalizeAIProviderModelItemSlice(value)
	case map[string]any:
		for _, key := range []string{"data", "models"} {
			if items, ok := value[key].([]any); ok {
				return normalizeAIProviderModelItemSlice(items)
			}
		}
		if nested, ok := value["response"].(map[string]any); ok {
			return extractAIProviderModelItems(nested)
		}
	}
	return nil
}

func normalizeAIProviderModelItemSlice(items []any) []map[string]any {
	models := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			models = append(models, object)
		}
	}
	return models
}

func buildAIProviderModelCandidate(item map[string]any, recommended bool) aiProviderModelCandidate {
	key := firstObjectString(item, "id", "model", "name")
	displayName := firstObjectString(item, "display_name", "displayName", "name", "id", "model")
	modelName := firstObjectString(item, "model_name", "modelName", "name", "id", "model")
	architecture := objectMap(item["architecture"])
	topProvider := objectMap(item["top_provider"])
	contextWindow := firstObjectInt64(
		item,
		"context_length",
		"contextLength",
		"context_window",
		"contextWindow",
		"max_context_length",
		"maxContextLength",
		"max_model_len",
		"maxModelLen",
		"max_model_length",
		"maxModelLength",
		"max_position_embeddings",
		"max_sequence_length",
	)
	if contextWindow == 0 {
		contextWindow = firstObjectInt64(topProvider, "context_length", "context_window", "max_context_length", "max_model_len")
	}
	maxOutputTokens := firstObjectInt64(item, "max_output_tokens", "max_completion_tokens", "max_tokens")
	if maxOutputTokens == 0 {
		maxOutputTokens = firstObjectInt64(topProvider, "max_completion_tokens", "max_output_tokens", "max_tokens")
	}
	modalities := normalizeProviderModelModalities(item, architecture)
	capabilities := normalizeProviderModelCapabilities(item)
	pricing := objectMap(item["pricing"])
	return aiProviderModelCandidate{
		ProviderModelKey:    key,
		ProviderDisplayName: displayName,
		ModelName:           defaultIfEmpty(modelName, displayName),
		ModelKind:           defaultIfEmpty(firstObjectString(item, "model_kind", "kind", "type"), "chat"),
		ModelFamily:         inferAIModelFamily(key, displayName),
		SourceModelID:       key,
		Modalities:          modalities,
		Capabilities:        capabilities,
		ContextWindow:       contextWindow,
		MaxOutputTokens:     maxOutputTokens,
		Pricing:             pricing,
		Recommended:         recommended,
		Raw:                 item,
	}
}

func normalizeProviderModelModalities(item map[string]any, architecture map[string]any) []string {
	values := append(stringListFromAny(item["modalities"]), stringListFromAny(item["input_modalities"])...)
	values = append(values, stringListFromAny(item["output_modalities"])...)
	modalityText := strings.ToLower(strings.Join(append(values, firstObjectString(architecture, "modality", "input_modalities")), " "))
	modalities := make([]string, 0, 3)
	if modalityText == "" || strings.Contains(modalityText, "text") {
		modalities = appendUniqueString(modalities, "text")
	}
	if strings.Contains(modalityText, "image") || strings.Contains(modalityText, "vision") {
		modalities = appendUniqueString(modalities, "vision")
	}
	if strings.Contains(modalityText, "audio") {
		modalities = appendUniqueString(modalities, "audio")
	}
	if strings.Contains(modalityText, "video") {
		modalities = appendUniqueString(modalities, "video")
	}
	return modalities
}

func normalizeProviderModelCapabilities(item map[string]any) []string {
	parameters := strings.ToLower(strings.Join(stringListFromAny(item["supported_parameters"]), " "))
	capabilities := make([]string, 0, 4)
	if strings.Contains(parameters, "tool") || strings.Contains(parameters, "function") {
		capabilities = appendUniqueString(capabilities, "tool")
	}
	if strings.Contains(parameters, "response_format") || strings.Contains(parameters, "json") {
		capabilities = appendUniqueString(capabilities, "json")
	}
	if strings.Contains(parameters, "reasoning") {
		capabilities = appendUniqueString(capabilities, "reasoning")
	}
	if strings.Contains(parameters, "stream") {
		capabilities = appendUniqueString(capabilities, "stream")
	}
	return capabilities
}

func inferAIModelFamily(values ...string) string {
	joined := strings.ToLower(strings.Join(values, " "))
	switch {
	case strings.Contains(joined, "gpt") || strings.Contains(joined, "openai"):
		return "gpt"
	case strings.Contains(joined, "claude") || strings.Contains(joined, "anthropic"):
		return "claude"
	case strings.Contains(joined, "gemini") || strings.Contains(joined, "google"):
		return "gemini"
	case strings.Contains(joined, "qwen"):
		return "qwen"
	case strings.Contains(joined, "llama"):
		return "llama"
	default:
		return ""
	}
}

func stringSetting(settings map[string]any, key, fallback string) string {
	if settings == nil {
		return fallback
	}
	value := strings.TrimSpace(fmt.Sprintf("%v", settings[key]))
	if value == "" || value == "<nil>" {
		return fallback
	}
	return value
}

func firstObjectString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func firstObjectInt64(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed > 0 {
				return int64(typed)
			}
		case float32:
			if typed > 0 {
				return int64(typed)
			}
		case int:
			if typed > 0 {
				return int64(typed)
			}
		case int64:
			if typed > 0 {
				return typed
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil && parsed > 0 {
				return parsed
			}
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			if err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return 0
}

func objectMap(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func stringListFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" && text != "<nil>" {
				values = append(values, text)
			}
		}
		return values
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ';' || r == '|' || r == '/' || r == '>' || r == '+'
		})
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			text := strings.TrimSpace(part)
			if text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func (h *AIProviderHandler) ListProviders(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI Provider"})
		return
	}
	var providers []models.AIProvider
	if err := h.DB.Preload("Bindings").Where("workspace_id = ?", workspaceID).Order("updated_at DESC, id DESC").Find(&providers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 AI Provider 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": providers})
}

func (h *AIProviderHandler) CreateProvider(c *gin.Context) {
	workspaceID, userID, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	var req aiProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	provider := models.AIProvider{
		WorkspaceID:  workspaceID,
		Name:         strings.TrimSpace(req.Name),
		Description:  req.Description,
		ProviderType: strings.TrimSpace(req.ProviderType),
		BaseURL:      strings.TrimSpace(req.BaseURL),
		CredentialID: optionalAIProviderCredentialID(req.CredentialID),
		HeadersJSON:  marshalJSONOrEmpty(req.HeadersJSON),
		SettingsJSON: marshalJSONOrEmpty(req.SettingsJSON),
		MetadataJSON: marshalJSONOrEmpty(req.MetadataJSON),
		Status:       models.AIProviderStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIProviderStatusActive))),
		CreatedBy:    userID,
	}
	if err := h.DB.Create(&provider).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 AI Provider 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": provider})
}

func (h *AIProviderHandler) UpdateProvider(c *gin.Context) {
	workspaceID, _, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	var provider models.AIProvider
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&provider, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "AI Provider 不存在"})
		return
	}
	var req aiProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	updates := map[string]any{
		"name":          strings.TrimSpace(req.Name),
		"description":   req.Description,
		"provider_type": strings.TrimSpace(req.ProviderType),
		"base_url":      strings.TrimSpace(req.BaseURL),
		"credential_id": optionalAIProviderCredentialID(req.CredentialID),
		"headers_json":  marshalJSONOrEmpty(req.HeadersJSON),
		"settings_json": marshalJSONOrEmpty(req.SettingsJSON),
		"metadata_json": marshalJSONOrEmpty(req.MetadataJSON),
		"status":        defaultIfEmpty(strings.TrimSpace(req.Status), string(provider.Status)),
	}
	if err := h.DB.Model(&provider).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 AI Provider 失败"})
		return
	}
	_ = h.DB.Preload("Bindings").First(&provider, provider.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": provider})
}

func (h *AIProviderHandler) DeleteProvider(c *gin.Context) {
	workspaceID, _, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	if err := h.DB.Where("workspace_id = ?", workspaceID).Delete(&models.AIProvider{}, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除 AI Provider 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}

func (h *AIProviderHandler) ListBindings(c *gin.Context) {
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI 模型绑定"})
		return
	}
	providerID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var bindings []models.AIModelBinding
	query := h.DB.Preload("Model").Where("workspace_id = ?", workspaceID).Order("updated_at DESC, id DESC")
	if providerID > 0 {
		query = query.Where("provider_id = ?", providerID)
	}
	if err := query.Find(&bindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 AI 模型绑定失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": bindings})
}

func (h *AIProviderHandler) CreateBinding(c *gin.Context) {
	workspaceID, userID, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	providerID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || providerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "provider_id 无效"})
		return
	}
	if err := ensureProviderExists(h.DB, workspaceID, providerID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "provider 不存在"})
		return
	}
	req, ok := decodeAIModelBindingRequest(c)
	if !ok {
		return
	}
	var model models.AIModelCatalog
	if err := h.DB.First(&model, req.ModelID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	capability, err := freezeModelBindingCapability(&req, model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	checkedAt := capability.CapabilityCheckedAt
	binding := models.AIModelBinding{
		WorkspaceID:            workspaceID,
		ModelID:                req.ModelID,
		ProviderID:             providerID,
		ProviderModelKey:       strings.TrimSpace(req.ProviderModelKey),
		SettingsJSON:           marshalJSONOrEmpty(req.SettingsJSON),
		MetadataJSON:           marshalJSONOrEmpty(req.MetadataJSON),
		ContextWindowTokens:    &capability.ContextWindowTokens,
		MaxOutputTokens:        capability.MaxOutputTokens,
		SupportsToolUse:        capability.SupportsToolUse,
		SupportsStreaming:      capability.SupportsStreaming,
		CapabilitySource:       capability.CapabilitySource,
		CapabilityCheckedAt:    &checkedAt,
		CapabilitySnapshotHash: capability.CapabilitySnapshotHash,
		Status:                 models.AIModelBindingStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIModelBindingStatusActive))),
		CreatedBy:              userID,
	}
	if err := h.DB.Create(&binding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 AI 模型绑定失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": binding})
}

func (h *AIProviderHandler) UpdateBinding(c *gin.Context) {
	workspaceID, _, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	providerID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || providerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "provider_id 无效"})
		return
	}
	var binding models.AIModelBinding
	if err := h.DB.Where("workspace_id = ? AND provider_id = ?", workspaceID, providerID).First(&binding, c.Param("binding_id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "AI 模型绑定不存在"})
		return
	}
	req, ok := decodeAIModelBindingRequest(c)
	if !ok {
		return
	}
	var model models.AIModelCatalog
	if err := h.DB.First(&model, req.ModelID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	capability, err := freezeModelBindingCapability(&req, model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	checkedAt := capability.CapabilityCheckedAt
	updates := map[string]any{
		"model_id":                 req.ModelID,
		"provider_model_key":       strings.TrimSpace(req.ProviderModelKey),
		"settings_json":            marshalJSONOrEmpty(req.SettingsJSON),
		"metadata_json":            marshalJSONOrEmpty(req.MetadataJSON),
		"context_window_tokens":    capability.ContextWindowTokens,
		"max_output_tokens":        capability.MaxOutputTokens,
		"supports_tool_use":        capability.SupportsToolUse,
		"supports_streaming":       capability.SupportsStreaming,
		"capability_source":        capability.CapabilitySource,
		"capability_checked_at":    checkedAt,
		"capability_snapshot_hash": capability.CapabilitySnapshotHash,
		"status":                   defaultIfEmpty(strings.TrimSpace(req.Status), string(binding.Status)),
	}
	if err := h.DB.Model(&binding).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 AI 模型绑定失败"})
		return
	}
	_ = h.DB.Preload("Model").First(&binding, binding.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": binding})
}

func (h *AIProviderHandler) DeleteBinding(c *gin.Context) {
	workspaceID, _, ok := requireAIProviderWrite(c, h.DB)
	if !ok {
		return
	}
	providerID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || providerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "provider_id 无效"})
		return
	}
	if err := h.DB.Where("workspace_id = ? AND provider_id = ?", workspaceID, providerID).Delete(&models.AIModelBinding{}, c.Param("binding_id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除 AI 模型绑定失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}
