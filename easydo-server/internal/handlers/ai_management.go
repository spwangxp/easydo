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
	Name         string                 `json:"name" binding:"required"`
	Description  string                 `json:"description"`
	ProviderType string                 `json:"provider_type" binding:"required"`
	BaseURL      string                 `json:"base_url"`
	CredentialID uint64                 `json:"credential_id"`
	HeadersJSON  map[string]interface{} `json:"headers_json"`
	SettingsJSON map[string]interface{} `json:"settings_json"`
	MetadataJSON map[string]interface{} `json:"metadata_json"`
	Status       string                 `json:"status"`
}

type aiModelBindingRequest struct {
	ModelID          uint64                 `json:"model_id" binding:"required"`
	ProviderModelKey string                 `json:"provider_model_key"`
	CapabilitiesJSON map[string]interface{} `json:"capabilities_json"`
	SettingsJSON     map[string]interface{} `json:"settings_json"`
	MetadataJSON     map[string]interface{} `json:"metadata_json"`
	Status           string                 `json:"status"`
}

type aiAgentRequest struct {
	Name               string                 `json:"name" binding:"required"`
	Description        string                 `json:"description"`
	Scenario           string                 `json:"scenario" binding:"required"`
	ScopeType          string                 `json:"scope_type"`
	SystemPrompt       string                 `json:"system_prompt"`
	UserPromptTemplate string                 `json:"user_prompt_template"`
	InputSchemaJSON    map[string]interface{} `json:"input_schema_json"`
	OutputSchemaJSON   map[string]interface{} `json:"output_schema_json"`
	ToolPolicyJSON     map[string]interface{} `json:"tool_policy_json"`
	Status             string                 `json:"status"`
}

type aiAgentRuntimeProfileRequest struct {
	Name                string                   `json:"name" binding:"required"`
	ModelID             uint64                   `json:"model_id" binding:"required"`
	BindingPriorityJSON []map[string]interface{} `json:"binding_priority_json"`
	RuntimeSettingsJSON map[string]interface{}   `json:"runtime_settings_json"`
	FallbackEnabled     *bool                    `json:"fallback_enabled"`
	Status              string                   `json:"status"`
}

func ensureProviderExists(db *gorm.DB, workspaceID, providerID uint64) error {
	var provider models.AIProvider
	return db.Where("workspace_id = ? AND id = ?", workspaceID, providerID).First(&provider).Error
}

func ensureModelExists(db *gorm.DB, modelID uint64) error {
	var model models.AIModelCatalog
	return db.First(&model, modelID).Error
}

func validateRuntimeProfileBindings(db *gorm.DB, workspaceID, modelID uint64, items []map[string]interface{}) error {
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

func marshalJSONOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func requireWorkspaceOwnerOrAdmin(c *gin.Context, db *gorm.DB) (uint64, uint64, string, bool) {
	userID, role := getRequestUser(c)
	workspaceID, _ := getRequestWorkspace(c)
	if workspaceID == 0 || !isWorkspaceOwner(db, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "仅系统管理员或工作空间 Owner 可执行该操作"})
		return 0, 0, "", false
	}
	return workspaceID, userID, role, true
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
	workspaceID, userID, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	updates := map[string]interface{}{
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
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	workspaceID, userID, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	var req aiModelBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
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
		CapabilitiesJSON: marshalJSONOrEmpty(req.CapabilitiesJSON),
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
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	var req aiModelBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	if err := ensureModelExists(h.DB, req.ModelID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "模型不存在"})
		return
	}
	updates := map[string]interface{}{
		"model_id":           req.ModelID,
		"provider_model_key": strings.TrimSpace(req.ProviderModelKey),
		"capabilities_json":  marshalJSONOrEmpty(req.CapabilitiesJSON),
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
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI Agent 定义"})
		return
	}
	var items []models.AIAgent
	if err := h.DB.Preload("RuntimeProfiles").Where("workspace_id = ?", workspaceID).Order("updated_at DESC, id DESC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 AI Agent 定义失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": items})
}

func (h *AIAgentHandler) CreateAgent(c *gin.Context) {
	workspaceID, userID, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
	if !ok {
		return
	}
	var req aiAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "请求参数无效"})
		return
	}
	item := models.AIAgent{
		WorkspaceID:        workspaceID,
		Name:               strings.TrimSpace(req.Name),
		Description:        req.Description,
		Scenario:           strings.TrimSpace(req.Scenario),
		ScopeType:          defaultIfEmpty(strings.TrimSpace(req.ScopeType), models.AgentScopeWorkspace),
		SystemPrompt:       req.SystemPrompt,
		UserPromptTemplate: req.UserPromptTemplate,
		InputSchemaJSON:    marshalJSONOrEmpty(req.InputSchemaJSON),
		OutputSchemaJSON:   marshalJSONOrEmpty(req.OutputSchemaJSON),
		ToolPolicyJSON:     marshalJSONOrEmpty(req.ToolPolicyJSON),
		Status:             models.AIAgentStatus(defaultIfEmpty(strings.TrimSpace(req.Status), string(models.AIAgentStatusDraft))),
		CreatedBy:          userID,
	}
	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "创建 AI Agent 定义失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) UpdateAgent(c *gin.Context) {
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	updates := map[string]interface{}{
		"name":                 strings.TrimSpace(req.Name),
		"description":          req.Description,
		"scenario":             strings.TrimSpace(req.Scenario),
		"scope_type":           defaultIfEmpty(strings.TrimSpace(req.ScopeType), item.ScopeType),
		"system_prompt":        req.SystemPrompt,
		"user_prompt_template": req.UserPromptTemplate,
		"input_schema_json":    marshalJSONOrEmpty(req.InputSchemaJSON),
		"output_schema_json":   marshalJSONOrEmpty(req.OutputSchemaJSON),
		"tool_policy_json":     marshalJSONOrEmpty(req.ToolPolicyJSON),
		"status":               defaultIfEmpty(strings.TrimSpace(req.Status), string(item.Status)),
	}
	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "更新 AI Agent 定义失败"})
		return
	}
	_ = h.DB.First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) DeleteAgent(c *gin.Context) {
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
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
	workspaceID, _ := getRequestWorkspace(c)
	userID, role := getRequestUser(c)
	if workspaceID == 0 || !userCanAccessWorkspace(h.DB, workspaceID, userID, role) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 Runtime Profile"})
		return
	}
	agentID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var profiles []models.AIRuntimeProfile
	if err := h.DB.Where("workspace_id = ? AND agent_id = ?", workspaceID, agentID).Order("updated_at DESC, id DESC").Find(&profiles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "加载 Runtime Profile 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": profiles})
}

func (h *AIAgentHandler) CreateRuntimeProfile(c *gin.Context) {
	workspaceID, userID, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
	if !ok {
		return
	}
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || agentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "agent_id 无效"})
		return
	}
	var agent models.AIAgent
	if err := h.DB.Where("workspace_id = ?", workspaceID).First(&agent, agentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "AI Agent 不存在"})
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
		AgentID:             agentID,
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
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) UpdateRuntimeProfile(c *gin.Context) {
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
	if !ok {
		return
	}
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || agentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "agent_id 无效"})
		return
	}
	var item models.AIRuntimeProfile
	if err := h.DB.Where("workspace_id = ? AND agent_id = ?", workspaceID, agentID).First(&item, c.Param("profile_id")).Error; err != nil {
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
	updates := map[string]interface{}{
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
	_ = h.DB.First(&item, item.ID).Error
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": item})
}

func (h *AIAgentHandler) DeleteRuntimeProfile(c *gin.Context) {
	workspaceID, _, _, ok := requireWorkspaceOwnerOrAdmin(c, h.DB)
	if !ok {
		return
	}
	agentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || agentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "agent_id 无效"})
		return
	}
	if err := h.DB.Where("workspace_id = ? AND agent_id = ?", workspaceID, agentID).Delete(&models.AIRuntimeProfile{}, c.Param("profile_id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "删除 Runtime Profile 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "删除成功"})
}
