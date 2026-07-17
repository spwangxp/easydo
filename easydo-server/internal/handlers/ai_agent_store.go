package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AIAgentStoreHandler struct {
	DB            *gorm.DB
	RuntimeClient *services.AIRuntimeClient
}

func NewAIAgentStoreHandler() *AIAgentStoreHandler {
	return &AIAgentStoreHandler{
		DB:            models.DB,
		RuntimeClient: services.NewAIRuntimeClientFromConfig(),
	}
}

func (h *AIAgentStoreHandler) runtimeClient() *services.AIRuntimeClient {
	if h.RuntimeClient != nil {
		return h.RuntimeClient
	}
	return services.NewAIRuntimeClientFromConfig()
}

func (h *AIAgentStoreHandler) listAllowed(c *gin.Context) (GovernanceContext, bool) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !userCanAccessWorkspace(h.DB, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI Agent Store"})
		return ctx, false
	}
	return ctx, true
}

func (h *AIAgentStoreHandler) commonWriteAllowed(c *gin.Context) (GovernanceContext, bool) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !canWriteAIAgentCommon(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "当前工作空间角色无权管理 common AI Agent 资源"})
		return ctx, false
	}
	return ctx, true
}

const (
	aiAgentReservedPageAssistantProfileName = "page-ai-assistant"
	aiAgentContextTagPageAssistant          = "page-assistant"
	builtinEasyDoMCPResourceKey             = "easydo"
)

func canWriteAIAgentCommon(ctx GovernanceContext) bool {
	if !RequireNormalWorkspaceKind(ctx) {
		return false
	}
	if isAdminRole(ctx.SystemRole) {
		return true
	}
	return middleware.WorkspaceRoleAtLeast(ctx.WorkspaceRole, models.WorkspaceRoleDeveloper)
}

func aiAgentContextTagsFromValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return uniqueNonEmptyStrings(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, strings.TrimSpace(fmt.Sprint(item)))
		}
		return uniqueNonEmptyStrings(items)
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ';' || r == '|' || r == '/'
		})
		return uniqueNonEmptyStrings(parts)
	default:
		return nil
	}
}

func isKnownAIAgentContextTag(tag string) bool {
	switch strings.TrimSpace(tag) {
	case aiAgentContextTagPageAssistant:
		return true
	default:
		return false
	}
}

func validateAIAgentContextTags(tags []string) string {
	for _, tag := range tags {
		if !isKnownAIAgentContextTag(tag) {
			return "未知上下文标签: " + tag
		}
	}
	return ""
}

func hasContextTag(tags []string, expected string) bool {
	for _, tag := range tags {
		if tag == expected {
			return true
		}
	}
	return false
}

func containsReservedPageAssistantTag(tags []string) bool {
	return hasContextTag(tags, aiAgentContextTagPageAssistant)
}

func uniqueNonEmptyStrings(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func hasAIAgentContextTags(payload map[string]any) bool {
	_, exists := payload["context_tags"]
	return exists
}

func hasLegacyAIAgentSceneProfileFields(payload map[string]any) bool {
	_, exists := payload["supported_scene_types"]
	return exists
}

func rejectLegacyAIAgentSceneProfileFields(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "supported_scene_types 已删除，请使用 context_tags"})
}

func readOptionalJSONMap(c *gin.Context) (map[string]any, bool) {
	if c.Request.Body == nil {
		return map[string]any{}, true
	}
	var payload map[string]any
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "请求参数无效")
		return nil, false
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, true
}

func (h *AIAgentStoreHandler) runtimeEnvelope(c *gin.Context, governance GovernanceContext, runtimePath string, payload map[string]any) services.AIRuntimeEnvelope {
	client := h.runtimeClient()
	requestID := aiRuntimeEnvelopeRequestID(c)
	idempotencyKey := aiRuntimeEnvelopeIdempotencyKey(c, governance, runtimePath, payload)
	return services.AIRuntimeEnvelope{
		RequestID:      requestID,
		IdempotencyKey: idempotencyKey,
		Actor: services.AIRuntimeActor{
			UserID:        governance.UserID,
			Username:      c.GetString("username"),
			SystemRole:    governance.SystemRole,
			WorkspaceID:   governance.WorkspaceID,
			WorkspaceRole: governance.WorkspaceRole,
			AuthSessionID: c.GetString("session_id"),
		},
		Auth: services.AIRuntimeAuth{
			DelegatedUserToken:  c.GetHeader("Authorization"),
			ServerInternalToken: client.InternalToken(),
		},
		Payload: payload,
	}
}

func (h *AIAgentStoreHandler) callRuntime(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) (*services.AIRuntimeResponse, error) {
	client := h.runtimeClient()
	return client.ForwardResponse(c.Request.Context(), method, runtimePath, h.runtimeEnvelope(c, governance, runtimePath, payload))
}

func (h *AIAgentStoreHandler) forward(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) {
	client := h.runtimeClient()
	forwardAIRuntimeBufferedResponse(c, client, method, runtimePath, h.runtimeEnvelope(c, governance, runtimePath, payload))
}

func isBuiltinEasyDoResourceIdentifier(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value))
	return normalized == builtinEasyDoMCPResourceKey || normalized == "builtin:easydo"
}

func payloadTargetsBuiltinEasyDoMCP(payload map[string]any) bool {
	if strings.TrimSpace(toString(payload["resource_kind"])) != "mcp_server" {
		return false
	}
	for _, key := range []string{"resource_key", "resource_id", "name"} {
		if isBuiltinEasyDoResourceIdentifier(toString(payload[key])) {
			return true
		}
	}
	return false
}

func rejectBuiltinEasyDoMCPMutation(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "内置 easydo MCP Server 不允许编辑或删除"})
}

func originFromRequest(c *gin.Context) string {
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(c.GetHeader(header))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
	}

	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request != nil && c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.GetHeader("Host"))
	}
	if host == "" && c.Request != nil {
		host = strings.TrimSpace(c.Request.Host)
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func builtinEasyDoMCPResource(c *gin.Context, governance GovernanceContext) map[string]any {
	origin := strings.TrimRight(originFromRequest(c), "/")
	if origin == "" {
		origin = "http://localhost"
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	config := map[string]any{
		"type":     "http",
		"url":      origin + "/mcp",
		"timeout":  30,
		"disabled": false,
	}
	return map[string]any{
		"id":            builtinEasyDoMCPResourceKey,
		"workspace_id":  governance.WorkspaceID,
		"resource_kind": "mcp_server",
		"resource_key":  builtinEasyDoMCPResourceKey,
		"resource_id":   builtinEasyDoMCPResourceKey,
		"name":          builtinEasyDoMCPResourceKey,
		"description":   "每个用户在当前工作空间的内置 EasyDo MCP Server",
		"version":       "latest",
		"status":        "active",
		"builtin":       true,
		"readonly":      true,
		"spec": map[string]any{
			"resource_subtype": "mcp_server",
			"builtin":          true,
			"readonly":         true,
			"workspace_id":     governance.WorkspaceID,
			"user_id":          governance.UserID,
			"mcpServers": map[string]any{
				builtinEasyDoMCPResourceKey: config,
			},
		},
		"endpoint": map[string]any{
			"type": "http",
			"url":  origin + "/mcp",
		},
		"secret_ref": map[string]any{
			"configured": true,
			"scope":      "current_user_workspace",
			"auth_mode":  "delegated_user_session",
		},
		"tags":       []string{"mcp-server", "builtin", "easydo"},
		"created_by": governance.UserID,
		"created_at": timestamp,
		"updated_at": timestamp,
	}
}

func withBuiltinEasyDoMCPResource(c *gin.Context, governance GovernanceContext, body []byte) ([]byte, error) {
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		data = map[string]any{}
		response["data"] = data
	}
	rawServers, _ := data["mcp_servers"].([]any)
	servers := []any{builtinEasyDoMCPResource(c, governance)}
	for _, item := range rawServers {
		record, ok := item.(map[string]any)
		if ok && isBuiltinEasyDoResourceIdentifier(toString(record["resource_key"])) {
			continue
		}
		servers = append(servers, item)
	}
	data["mcp_servers"] = servers
	return json.Marshal(response)
}

type aiAgentProfileIdentity struct {
	Name        string
	ContextTags []string
}

func (identity aiAgentProfileIdentity) isReservedPageAssistant() bool {
	return identity.Name == aiAgentReservedPageAssistantProfileName
}

func (h *AIAgentStoreHandler) existingProfileIdentity(c *gin.Context, governance GovernanceContext, profileID string) (aiAgentProfileIdentity, bool) {
	runtimeResponse, err := h.callRuntime(c, governance, http.MethodPost, "/v1/agent-profiles/"+profileID+"/query", map[string]any{})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return aiAgentProfileIdentity{}, false
	}
	if runtimeResponse.StatusCode < http.StatusOK || runtimeResponse.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, runtimeResponse, aiRuntimeEnvelopeRequestID(c))
		return aiAgentProfileIdentity{}, false
	}
	var response map[string]any
	if err := json.Unmarshal(runtimeResponse.Body, &response); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": "AI runtime profile 响应无效"})
		return aiAgentProfileIdentity{}, false
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": "AI runtime profile 数据无效"})
		return aiAgentProfileIdentity{}, false
	}
	return aiAgentProfileIdentity{
		Name:        strings.TrimSpace(toString(data["name"])),
		ContextTags: aiAgentContextTagsFromValue(data["context_tags"]),
	}, true
}

func normalizeAIAgentProfilePayload(c *gin.Context, payload map[string]any, profileID string) bool {
	if hasLegacyAIAgentSceneProfileFields(payload) {
		rejectLegacyAIAgentSceneProfileFields(c)
		return false
	}
	if len(payload) > 0 && strings.TrimSpace(toString(payload["profile_kind"])) == "" {
		payload["profile_kind"] = "generic"
	}
	_, hasTags := payload["context_tags"]
	tags := aiAgentContextTagsFromValue(payload["context_tags"])
	if msg := validateAIAgentContextTags(tags); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": msg})
		return false
	}
	if hasTags || profileID == "" {
		payload["context_tags"] = tags
	}
	return true
}

func validateReservedPageAssistantPayload(c *gin.Context, payload map[string]any, existing aiAgentProfileIdentity) bool {
	name := strings.TrimSpace(toString(payload["name"]))
	if existing.isReservedPageAssistant() {
		if name != "" && name != aiAgentReservedPageAssistantProfileName {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "page-ai-assistant 是系统保留 Profile 名称，不允许修改"})
			return false
		}
		if hasAIAgentContextTags(payload) && !containsReservedPageAssistantTag(aiAgentContextTagsFromValue(payload["context_tags"])) {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "page-ai-assistant 必须保留 page-assistant 上下文标签"})
			return false
		}
		return true
	}

	tags := aiAgentContextTagsFromValue(payload["context_tags"])
	if name == aiAgentReservedPageAssistantProfileName && !containsReservedPageAssistantTag(tags) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "page-ai-assistant 必须包含 page-assistant 上下文标签"})
		return false
	}
	if name != aiAgentReservedPageAssistantProfileName && containsReservedPageAssistantTag(tags) {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "page-assistant 标签只能用于 page-ai-assistant Profile"})
		return false
	}
	return true
}

func (h *AIAgentStoreHandler) profileMutationAllowed(c *gin.Context, payload map[string]any, profileID, operation string) (GovernanceContext, bool) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !RequireNormalWorkspaceKind(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权管理 AI Agent"})
		return ctx, false
	}
	if !normalizeAIAgentProfilePayload(c, payload, profileID) {
		return ctx, false
	}
	existing := aiAgentProfileIdentity{}
	if profileID != "" {
		var ok bool
		existing, ok = h.existingProfileIdentity(c, ctx, profileID)
		if !ok {
			return ctx, false
		}
	}
	if !validateReservedPageAssistantPayload(c, payload, existing) {
		return ctx, false
	}
	if operation == "delete" && existing.isReservedPageAssistant() {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "page-ai-assistant 是系统保留 Profile，不允许删除"})
		return ctx, false
	}
	if RequireWorkspaceGovernance(ctx) {
		return ctx, true
	}
	if !canWriteAIAgentCommon(ctx) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "当前工作空间角色无权管理 AI Agent"})
		return ctx, false
	}

	if existing.isReservedPageAssistant() || containsReservedPageAssistantTag(existing.ContextTags) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "仅工作空间 Owner 可管理 page-ai-assistant Profile"})
		return ctx, false
	}
	if containsReservedPageAssistantTag(aiAgentContextTagsFromValue(payload["context_tags"])) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "非 Owner 不可管理 page-assistant 上下文标签"})
		return ctx, false
	}
	return ctx, true
}

func (h *AIAgentStoreHandler) ListProfiles(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/query", map[string]any{})
}

func (h *AIAgentStoreHandler) ListWorkspaces(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-workspaces/query", map[string]any{})
}

// GetOperationsSummary exposes Runtime-owned operational health inside the AI
// Agent Store while preserving the caller's workspace scope and correlation ID.
func (h *AIAgentStoreHandler) GetOperationsSummary(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/operations/summary", map[string]any{})
}

func (h *AIAgentStoreHandler) EnsureWorkspace(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-workspaces", payload)
}

func (h *AIAgentStoreHandler) GetWorkspace(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "query")
}

func (h *AIAgentStoreHandler) ConnectWorkspace(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "connect")
}

func (h *AIAgentStoreHandler) PauseWorkspace(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "pause")
}

func (h *AIAgentStoreHandler) ResumeWorkspace(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "resume")
}

func (h *AIAgentStoreHandler) RecycleWorkspace(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "recycle")
}

func (h *AIAgentStoreHandler) ListWorkspaceAudits(c *gin.Context) {
	h.forwardWorkspaceLifecycle(c, "audits/query")
}

func (h *AIAgentStoreHandler) forwardWorkspaceLifecycle(c *gin.Context, operation string) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	workspaceRuntimeID := strings.TrimSpace(c.Param("workspace_runtime_id"))
	if workspaceRuntimeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "workspace_runtime_id is required"})
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-workspaces/"+url.PathEscape(workspaceRuntimeID)+"/"+operation, map[string]any{})
}

func (h *AIAgentStoreHandler) CreateProfile(c *gin.Context) {
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	ctx, ok := h.profileMutationAllowed(c, payload, "", "create")
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles", payload)
}

func (h *AIAgentStoreHandler) GetProfile(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/"+c.Param("id")+"/query", map[string]any{})
}

func (h *AIAgentStoreHandler) UpdateProfile(c *gin.Context) {
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	ctx, ok := h.profileMutationAllowed(c, payload, c.Param("id"), "update")
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPut, "/v1/agent-profiles/"+c.Param("id"), payload)
}

func (h *AIAgentStoreHandler) DeleteProfile(c *gin.Context) {
	ctx, ok := h.profileMutationAllowed(c, map[string]any{}, c.Param("id"), "delete")
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodDelete, "/v1/agent-profiles/"+c.Param("id"), map[string]any{})
}

func (h *AIAgentStoreHandler) PublishProfile(c *gin.Context) {
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	ctx, ok := h.profileMutationAllowed(c, payload, c.Param("id"), "publish")
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/"+c.Param("id")+"/publish", payload)
}

func (h *AIAgentStoreHandler) ValidateProfile(c *gin.Context) {
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	ctx, ok := h.profileMutationAllowed(c, payload, c.Param("id"), "validate")
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/"+c.Param("id")+"/validate", payload)
}

func (h *AIAgentStoreHandler) ListProfileVersions(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/"+c.Param("id")+"/versions/query", map[string]any{})
}

func (h *AIAgentStoreHandler) GetProfileDependencies(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-profiles/"+c.Param("id")+"/dependencies/query", map[string]any{})
}

func (h *AIAgentStoreHandler) ListResources(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	runtimeResponse, err := h.callRuntime(c, ctx, http.MethodPost, "/v1/agent-resources/query", map[string]any{})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return
	}
	if runtimeResponse.StatusCode < http.StatusOK || runtimeResponse.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, runtimeResponse, aiRuntimeEnvelopeRequestID(c))
		return
	}
	augmented, err := withBuiltinEasyDoMCPResource(c, ctx, runtimeResponse.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": "AI runtime resource 响应无效"})
		return
	}
	augmentedResponse := *runtimeResponse
	augmentedResponse.Body = augmented
	writeAIRuntimeBufferedResponse(c, &augmentedResponse, aiRuntimeEnvelopeRequestID(c))
}

func (h *AIAgentStoreHandler) ListResourceVersions(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-resources/"+c.Param("id")+"/versions/query", map[string]any{})
}

func (h *AIAgentStoreHandler) GetResourceDependencies(c *gin.Context) {
	ctx, ok := h.listAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-resources/"+c.Param("id")+"/dependencies/query", map[string]any{})
}

func (h *AIAgentStoreHandler) CreateResource(c *gin.Context) {
	ctx, ok := h.commonWriteAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	if payloadTargetsBuiltinEasyDoMCP(payload) {
		rejectBuiltinEasyDoMCPMutation(c)
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-resources", payload)
}

func (h *AIAgentStoreHandler) UpdateResource(c *gin.Context) {
	ctx, ok := h.commonWriteAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	if isBuiltinEasyDoResourceIdentifier(c.Param("id")) || payloadTargetsBuiltinEasyDoMCP(payload) {
		rejectBuiltinEasyDoMCPMutation(c)
		return
	}
	h.forward(c, ctx, http.MethodPut, "/v1/agent-resources/"+c.Param("id"), payload)
}

func (h *AIAgentStoreHandler) ScanResource(c *gin.Context) {
	ctx, ok := h.commonWriteAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	if isBuiltinEasyDoResourceIdentifier(c.Param("id")) {
		rejectBuiltinEasyDoMCPMutation(c)
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-resources/"+c.Param("id")+"/scan", payload)
}

func (h *AIAgentStoreHandler) ProbeMcpResource(c *gin.Context) {
	ctx, ok := h.commonWriteAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/agent-resources/mcp/probe", payload)
}

func (h *AIAgentStoreHandler) DeleteResource(c *gin.Context) {
	ctx, ok := h.commonWriteAllowed(c)
	if !ok {
		return
	}
	if isBuiltinEasyDoResourceIdentifier(c.Param("id")) {
		rejectBuiltinEasyDoMCPMutation(c)
		return
	}
	h.forward(c, ctx, http.MethodDelete, "/v1/agent-resources/"+c.Param("id"), map[string]any{})
}
