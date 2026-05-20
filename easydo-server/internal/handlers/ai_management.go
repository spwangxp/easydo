package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AIProviderHandler struct {
	DB *gorm.DB
}

type AIAgentHandler struct {
	DB *gorm.DB
}

func NewAIProviderHandler() *AIProviderHandler {
	return &AIProviderHandler{DB: models.DB}
}

func NewAIAgentHandler() *AIAgentHandler {
	return &AIAgentHandler{DB: models.DB}
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
	ModelID          uint64         `json:"model_id" binding:"required"`
	ProviderModelKey string         `json:"provider_model_key"`
	SettingsJSON     map[string]any `json:"settings_json"`
	MetadataJSON     map[string]any `json:"metadata_json"`
	Status           string         `json:"status"`
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
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "capabilities_json 已废弃，请改用 runtime profile / agent capability 定义"})
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

type aiAgentRequest struct {
	Name               string           `json:"name" binding:"required"`
	Description        string           `json:"description"`
	ScopeType          string           `json:"scope_type"`
	RuntimeProfileID   *uint64          `json:"runtime_profile_id"`
	SystemPrompt       *string          `json:"system_prompt"`
	UserPromptTemplate *string          `json:"user_prompt_template"`
	InputSchemaJSON    *json.RawMessage `json:"input_schema_json"`
	OutputSchemaJSON   *json.RawMessage `json:"output_schema_json"`
	ToolPolicyJSON     *json.RawMessage `json:"tool_policy_json"`
	ToolsJSON          *json.RawMessage `json:"tools_json"`
	SkillsJSON         *json.RawMessage `json:"skills_json"`
	MemoryJSON         *json.RawMessage `json:"memory_json"`
	MCPServersJSON     *json.RawMessage `json:"mcp_servers_json"`
	SubAgentsJSON      *json.RawMessage `json:"sub_agents_json"`
	MetadataJSON       *json.RawMessage `json:"metadata_json"`
	Status             string           `json:"status"`
}

type aiAgentRuntimeProfileRequest struct {
	Name                string           `json:"name" binding:"required"`
	ModelID             uint64           `json:"model_id" binding:"required"`
	BindingPriorityJSON []map[string]any `json:"binding_priority_json"`
	RuntimeSettingsJSON map[string]any   `json:"runtime_settings_json"`
	FallbackEnabled     *bool            `json:"fallback_enabled"`
	Status              string           `json:"status"`
}

type aiModelSummary struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type aiRuntimeProfileSummary struct {
	ID              uint64                        `json:"id"`
	WorkspaceID     uint64                        `json:"workspace_id"`
	Name            string                        `json:"name"`
	ModelID         uint64                        `json:"model_id"`
	FallbackEnabled bool                          `json:"fallback_enabled"`
	Status          models.AIRuntimeProfileStatus `json:"status"`
	Model           *aiModelSummary               `json:"model,omitempty"`
}

type aiAgentSummary struct {
	ID               uint64                   `json:"id"`
	WorkspaceID      uint64                   `json:"workspace_id"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	ScopeType        string                   `json:"scope_type"`
	RuntimeProfileID *uint64                  `json:"runtime_profile_id"`
	Status           models.AIAgentStatus     `json:"status"`
	RuntimeProfile   *aiRuntimeProfileSummary `json:"runtime_profile,omitempty"`
}

func ensureProviderExists(db *gorm.DB, workspaceID, providerID uint64) error {
	var provider models.AIProvider
	return db.Where("workspace_id = ? AND id = ?", workspaceID, providerID).First(&provider).Error
}

func ensureModelExists(db *gorm.DB, modelID uint64) error {
	var model models.AIModelCatalog
	return db.First(&model, modelID).Error
}

func ensureRuntimeProfileExists(db *gorm.DB, workspaceID, runtimeProfileID uint64) error {
	if runtimeProfileID == 0 {
		return nil
	}
	var profile models.AIRuntimeProfile
	return db.Where("workspace_id = ? AND id = ?", workspaceID, runtimeProfileID).First(&profile).Error
}

func optionalRuntimeProfileID(runtimeProfileID *uint64) *uint64 {
	if runtimeProfileID == nil || *runtimeProfileID == 0 {
		return nil
	}
	id := *runtimeProfileID
	return &id
}

func optionalRuntimeProfileIDOrExisting(runtimeProfileID *uint64, existing *uint64) *uint64 {
	if runtimeProfileID == nil {
		if existing == nil {
			return nil
		}
		id := *existing
		return &id
	}
	return optionalRuntimeProfileID(runtimeProfileID)
}

func validateRuntimeProfileBindings(db *gorm.DB, workspaceID, modelID uint64, items []map[string]any) error {
	for _, item := range items {
		bindingID := toUint64Value(item["binding_id"])
		if bindingID == 0 {
			return gorm.ErrInvalidData
		}
		var binding models.AIModelBinding
		if err := db.Where("workspace_id = ? AND id = ?", workspaceID, bindingID).First(&binding).Error; err != nil {
			return err
		}
		if binding.ModelID != modelID || binding.Status != models.AIModelBindingStatusActive {
			return gorm.ErrInvalidData
		}
	}
	return nil
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

func normalizeOptionalRawJSON(v *json.RawMessage) string {
	if v == nil {
		return ""
	}
	raw := strings.TrimSpace(string(*v))
	if raw == "" || raw == "null" {
		return ""
	}
	var payload any
	if err := json.Unmarshal(*v, &payload); err != nil {
		return ""
	}
	return marshalJSONOrEmpty(payload)
}

func normalizeOptionalRawJSONOrExisting(v *json.RawMessage, existing string) string {
	if v == nil {
		return existing
	}
	return normalizeOptionalRawJSON(v)
}

func optionalStringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func optionalStringValueOrExisting(v *string, existing string) string {
	if v == nil {
		return existing
	}
	return *v
}

func buildRuntimeProfileSummary(item *models.AIRuntimeProfile) *aiRuntimeProfileSummary {
	if item == nil {
		return nil
	}
	summary := &aiRuntimeProfileSummary{
		ID:              item.ID,
		WorkspaceID:     item.WorkspaceID,
		Name:            item.Name,
		ModelID:         item.ModelID,
		FallbackEnabled: item.FallbackEnabled,
		Status:          item.Status,
	}
	if item.Model != nil {
		summary.Model = &aiModelSummary{
			ID:          item.Model.ID,
			Name:        item.Model.Name,
			DisplayName: item.Model.DisplayName,
		}
	}
	return summary
}

func buildAgentSummary(item models.AIAgent) aiAgentSummary {
	return aiAgentSummary{
		ID:               item.ID,
		WorkspaceID:      item.WorkspaceID,
		Name:             item.Name,
		Description:      item.Description,
		ScopeType:        item.ScopeType,
		RuntimeProfileID: item.RuntimeProfileID,
		Status:           item.Status,
		RuntimeProfile:   buildRuntimeProfileSummary(item.RuntimeProfile),
	}
}

func aiGovernanceContext(c *gin.Context, db *gorm.DB) GovernanceContext {
	ctx := BuildGovernanceContext(c)
	if db == nil || ctx.WorkspaceID == 0 {
		return ctx
	}
	return governanceContextForWorkspace(db, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole)
}

func requirePlatformGovernance(c *gin.Context, db *gorm.DB) (uint64, uint64, bool) {
	ctx := aiGovernanceContext(c, db)
	if ctx.WorkspaceID == 0 || !RequirePlatformGovernance(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "仅平台治理上下文可执行该操作"})
		return 0, 0, false
	}
	return ctx.WorkspaceID, ctx.UserID, true
}

func requireWorkspaceGovernance(c *gin.Context, db *gorm.DB) (uint64, uint64, bool) {
	ctx := aiGovernanceContext(c, db)
	if ctx.WorkspaceID == 0 || !RequireWorkspaceGovernance(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "仅普通工作空间治理上下文可执行该操作"})
		return 0, 0, false
	}
	return ctx.WorkspaceID, ctx.UserID, true
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
	workspaceID, userID, ok := requirePlatformGovernance(c, h.DB)
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
		CredentialID: req.CredentialID,
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
	workspaceID, _, ok := requirePlatformGovernance(c, h.DB)
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
		"credential_id": req.CredentialID,
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
	workspaceID, _, ok := requirePlatformGovernance(c, h.DB)
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
	workspaceID, userID, ok := requirePlatformGovernance(c, h.DB)
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
	if err := ensureModelExists(h.DB, req.ModelID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	binding := models.AIModelBinding{
		WorkspaceID:      workspaceID,
		ModelID:          req.ModelID,
		ProviderID:       providerID,
		ProviderModelKey: strings.TrimSpace(req.ProviderModelKey),
		SettingsJSON:     marshalJSONOrEmpty(req.SettingsJSON),
		MetadataJSON:     marshalJSONOrEmpty(req.MetadataJSON),
		Status:           models.AIModelBindingStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIModelBindingStatusActive))),
		CreatedBy:        userID,
	}
	if err := h.DB.Create(&binding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 AI 模型绑定失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": binding})
}

func (h *AIProviderHandler) UpdateBinding(c *gin.Context) {
	workspaceID, _, ok := requirePlatformGovernance(c, h.DB)
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
	if err := ensureModelExists(h.DB, req.ModelID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	updates := map[string]any{
		"model_id":           req.ModelID,
		"provider_model_key": strings.TrimSpace(req.ProviderModelKey),
		"settings_json":      marshalJSONOrEmpty(req.SettingsJSON),
		"metadata_json":      marshalJSONOrEmpty(req.MetadataJSON),
		"status":             defaultIfEmpty(strings.TrimSpace(req.Status), string(binding.Status)),
	}
	if err := h.DB.Model(&binding).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 AI 模型绑定失败"})
		return
	}
	_ = h.DB.Preload("Model").First(&binding, binding.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": binding})
}

func (h *AIProviderHandler) DeleteBinding(c *gin.Context) {
	workspaceID, _, ok := requirePlatformGovernance(c, h.DB)
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

func (h *AIAgentHandler) ListAgents(c *gin.Context) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !userCanAccessWorkspace(h.DB, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI Agent 定义"})
		return
	}
	var items []models.AIAgent
	if err := h.DB.Preload("RuntimeProfile").Preload("RuntimeProfile.Model").Where("workspace_id = ?", ctx.WorkspaceID).Order("updated_at DESC, id DESC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 AI Agent 定义失败"})
		return
	}
	if RequireWorkspaceGovernance(ctx) {
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": items})
		return
	}
	summaries := make([]aiAgentSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, buildAgentSummary(item))
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": summaries})
}

func (h *AIAgentHandler) CreateAgent(c *gin.Context) {
	workspaceID, userID, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	var req aiAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if err := ensureRuntimeProfileExists(h.DB, workspaceID, toUint64Value(req.RuntimeProfileID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "runtime_profile_id 无效"})
		return
	}
	item := models.AIAgent{
		WorkspaceID:        workspaceID,
		Name:               strings.TrimSpace(req.Name),
		Description:        req.Description,
		ScopeType:          defaultIfEmpty(strings.TrimSpace(req.ScopeType), models.AgentScopeWorkspace),
		RuntimeProfileID:   optionalRuntimeProfileID(req.RuntimeProfileID),
		SystemPrompt:       optionalStringValue(req.SystemPrompt),
		UserPromptTemplate: optionalStringValue(req.UserPromptTemplate),
		InputSchemaJSON:    normalizeOptionalRawJSON(req.InputSchemaJSON),
		OutputSchemaJSON:   normalizeOptionalRawJSON(req.OutputSchemaJSON),
		ToolPolicyJSON:     normalizeOptionalRawJSON(req.ToolPolicyJSON),
		ToolsJSON:          normalizeOptionalRawJSON(req.ToolsJSON),
		SkillsJSON:         normalizeOptionalRawJSON(req.SkillsJSON),
		MemoryJSON:         normalizeOptionalRawJSON(req.MemoryJSON),
		MCPServersJSON:     normalizeOptionalRawJSON(req.MCPServersJSON),
		SubAgentsJSON:      normalizeOptionalRawJSON(req.SubAgentsJSON),
		MetadataJSON:       normalizeOptionalRawJSON(req.MetadataJSON),
		Status:             models.AIAgentStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIAgentStatusDraft))),
		CreatedBy:          userID,
	}
	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 AI Agent 定义失败"})
		return
	}
	_ = h.DB.Preload("RuntimeProfile").Preload("RuntimeProfile.Model").First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) UpdateAgent(c *gin.Context) {
	workspaceID, _, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	var item models.AIAgent
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&item, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "AI Agent 定义不存在"})
		return
	}
	var req aiAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if err := ensureRuntimeProfileExists(h.DB, workspaceID, toUint64Value(req.RuntimeProfileID)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "runtime_profile_id 无效"})
		return
	}
	updates := map[string]any{
		"name":                 strings.TrimSpace(req.Name),
		"description":          req.Description,
		"scope_type":           defaultIfEmpty(strings.TrimSpace(req.ScopeType), item.ScopeType),
		"runtime_profile_id":   optionalRuntimeProfileIDOrExisting(req.RuntimeProfileID, item.RuntimeProfileID),
		"system_prompt":        optionalStringValueOrExisting(req.SystemPrompt, item.SystemPrompt),
		"user_prompt_template": optionalStringValueOrExisting(req.UserPromptTemplate, item.UserPromptTemplate),
		"input_schema_json":    normalizeOptionalRawJSONOrExisting(req.InputSchemaJSON, item.InputSchemaJSON),
		"output_schema_json":   normalizeOptionalRawJSONOrExisting(req.OutputSchemaJSON, item.OutputSchemaJSON),
		"tool_policy_json":     normalizeOptionalRawJSONOrExisting(req.ToolPolicyJSON, item.ToolPolicyJSON),
		"tools_json":           normalizeOptionalRawJSONOrExisting(req.ToolsJSON, item.ToolsJSON),
		"skills_json":          normalizeOptionalRawJSONOrExisting(req.SkillsJSON, item.SkillsJSON),
		"memory_json":          normalizeOptionalRawJSONOrExisting(req.MemoryJSON, item.MemoryJSON),
		"mcp_servers_json":     normalizeOptionalRawJSONOrExisting(req.MCPServersJSON, item.MCPServersJSON),
		"sub_agents_json":      normalizeOptionalRawJSONOrExisting(req.SubAgentsJSON, item.SubAgentsJSON),
		"metadata_json":        normalizeOptionalRawJSONOrExisting(req.MetadataJSON, item.MetadataJSON),
		"status":               defaultIfEmpty(strings.TrimSpace(req.Status), string(item.Status)),
	}
	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 AI Agent 定义失败"})
		return
	}
	_ = h.DB.Preload("RuntimeProfile").Preload("RuntimeProfile.Model").First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) DeleteAgent(c *gin.Context) {
	workspaceID, _, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	if err := h.DB.Where("workspace_id = ?", workspaceID).Delete(&models.AIAgent{}, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除 AI Agent 定义失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}

func (h *AIAgentHandler) ListRuntimeProfiles(c *gin.Context) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !userCanAccessWorkspace(h.DB, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 Runtime Profile"})
		return
	}
	var profiles []models.AIRuntimeProfile
	if err := h.DB.Preload("Model").Where("workspace_id = ?", ctx.WorkspaceID).Order("updated_at DESC, id DESC").Find(&profiles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 Runtime Profile 失败"})
		return
	}
	if RequireWorkspaceGovernance(ctx) {
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": profiles})
		return
	}
	summaries := make([]*aiRuntimeProfileSummary, 0, len(profiles))
	for i := range profiles {
		summaries = append(summaries, buildRuntimeProfileSummary(&profiles[i]))
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": summaries})
}

func (h *AIAgentHandler) CreateRuntimeProfile(c *gin.Context) {
	workspaceID, userID, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	var req aiAgentRuntimeProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if err := ensureModelExists(h.DB, req.ModelID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	if err := validateRuntimeProfileBindings(h.DB, workspaceID, req.ModelID, req.BindingPriorityJSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "binding_priority_json 无效"})
		return
	}
	fallbackEnabled := true
	if req.FallbackEnabled != nil {
		fallbackEnabled = *req.FallbackEnabled
	}
	item := models.AIRuntimeProfile{
		WorkspaceID:         workspaceID,
		Name:                strings.TrimSpace(req.Name),
		ModelID:             req.ModelID,
		BindingPriorityJSON: marshalJSONOrEmpty(req.BindingPriorityJSON),
		RuntimeSettingsJSON: marshalJSONOrEmpty(req.RuntimeSettingsJSON),
		FallbackEnabled:     fallbackEnabled,
		Status:              models.AIRuntimeProfileStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIRuntimeProfileStatusDraft))),
		CreatedBy:           userID,
	}
	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 Runtime Profile 失败"})
		return
	}
	_ = h.DB.Preload("Model").First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) UpdateRuntimeProfile(c *gin.Context) {
	workspaceID, _, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	var item models.AIRuntimeProfile
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&item, c.Param("profile_id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "Runtime Profile 不存在"})
		return
	}
	var req aiAgentRuntimeProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if err := ensureModelExists(h.DB, req.ModelID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	if err := validateRuntimeProfileBindings(h.DB, workspaceID, req.ModelID, req.BindingPriorityJSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "binding_priority_json 无效"})
		return
	}
	fallbackEnabled := item.FallbackEnabled
	if req.FallbackEnabled != nil {
		fallbackEnabled = *req.FallbackEnabled
	}
	updates := map[string]any{
		"name":                  strings.TrimSpace(req.Name),
		"model_id":              req.ModelID,
		"binding_priority_json": marshalJSONOrEmpty(req.BindingPriorityJSON),
		"runtime_settings_json": marshalJSONOrEmpty(req.RuntimeSettingsJSON),
		"fallback_enabled":      fallbackEnabled,
		"status":                defaultIfEmpty(strings.TrimSpace(req.Status), string(item.Status)),
	}
	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 Runtime Profile 失败"})
		return
	}
	_ = h.DB.Preload("Model").First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func pipelineNodeReferencesRuntimeProfile(node PipelineNode, profileID uint64) bool {
	if profileID == 0 {
		return false
	}
	if toUint64Value(node.Config["runtime_profile_id"]) == profileID {
		return true
	}
	if toUint64Value(node.Params["runtime_profile_id"]) == profileID {
		return true
	}
	for _, param := range node.DefinitionParams {
		if param.Key == "runtime_profile_id" && toUint64Value(param.Value) == profileID {
			return true
		}
	}
	return false
}

func (h *AIAgentHandler) countPipelineDefinitionRuntimeProfileRefs(workspaceID, profileID uint64) (int64, error) {
	var pipelines []models.Pipeline
	if err := h.DB.Select("id", "name", "workspace_id", "config", "definition_json").Where("workspace_id = ?", workspaceID).Find(&pipelines).Error; err != nil {
		return 0, err
	}
	var count int64
	for _, pipeline := range pipelines {
		raw := strings.TrimSpace(pipelineDefinitionPayload(pipeline))
		if raw == "" {
			continue
		}
		config, err := parsePipelineConfigJSON(raw)
		if err != nil {
			return 0, err
		}
		for _, node := range config.Nodes {
			if pipelineNodeReferencesRuntimeProfile(node, profileID) {
				count++
				break
			}
		}
	}
	return count, nil
}

func (h *AIAgentHandler) DeleteRuntimeProfile(c *gin.Context) {
	workspaceID, _, ok := requireWorkspaceGovernance(c, h.DB)
	if !ok {
		return
	}
	profileID, err := strconv.ParseUint(c.Param("profile_id"), 10, 64)
	if err != nil || profileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "profile_id 无效"})
		return
	}
	var agentRefCount int64
	if err := h.DB.Model(&models.AIAgent{}).Where("workspace_id = ? AND runtime_profile_id = ?", workspaceID, profileID).Count(&agentRefCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "检查 Runtime Profile 引用失败"})
		return
	}
	if agentRefCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "该 Runtime Profile 已被 AI Agent 引用，无法删除"})
		return
	}
	var sessionRefCount int64
	if err := h.DB.Model(&models.AISession{}).Where("workspace_id = ? AND runtime_profile_id = ?", workspaceID, profileID).Count(&sessionRefCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "检查 Runtime Profile 引用失败"})
		return
	}
	if sessionRefCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "该 Runtime Profile 已被 AI Session 引用，无法删除"})
		return
	}
	pipelineRefCount, err := h.countPipelineDefinitionRuntimeProfileRefs(workspaceID, profileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "检查 Runtime Profile 引用失败"})
		return
	}
	if pipelineRefCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "该 Runtime Profile 已被流水线定义引用，无法删除"})
		return
	}
	if err := h.DB.Where("workspace_id = ?", workspaceID).Delete(&models.AIRuntimeProfile{}, profileID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除 Runtime Profile 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}
