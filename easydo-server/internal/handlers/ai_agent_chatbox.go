package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AIAgentChatboxHandler struct {
	DB            *gorm.DB
	RuntimeClient *services.AIRuntimeClient
}

func NewAIAgentChatboxHandler() *AIAgentChatboxHandler {
	return &AIAgentChatboxHandler{
		DB:            models.DB,
		RuntimeClient: services.NewAIRuntimeClientFromConfig(),
	}
}

func (h *AIAgentChatboxHandler) runtimeClient() *services.AIRuntimeClient {
	if h.RuntimeClient != nil {
		return h.RuntimeClient
	}
	return services.NewAIRuntimeClientFromConfig()
}

func (h *AIAgentChatboxHandler) accessAllowed(c *gin.Context) (GovernanceContext, bool) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !canUseAIRuntimeContextTags(ctx, nil) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "当前工作空间角色无权使用 Agent Chatbox"})
		return ctx, false
	}
	return ctx, true
}

func (h *AIAgentChatboxHandler) runtimeEnvelope(c *gin.Context, governance GovernanceContext, runtimePath string, payload map[string]any) services.AIRuntimeEnvelope {
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

func (h *AIAgentChatboxHandler) callRuntime(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) (*services.AIRuntimeResponse, error) {
	client := h.runtimeClient()
	return client.ForwardResponse(c.Request.Context(), method, runtimePath, h.runtimeEnvelope(c, governance, runtimePath, payload))
}

func (h *AIAgentChatboxHandler) forwardRuntime(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) {
	client := h.runtimeClient()
	forwardAIRuntimeBufferedResponse(c, client, method, runtimePath, h.runtimeEnvelope(c, governance, runtimePath, payload))
}

func (h *AIAgentChatboxHandler) forwardRuntimeStream(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) {
	client := h.runtimeClient()
	forwardAIRuntimeStreamResponse(c, client, method, runtimePath, h.runtimeEnvelope(c, governance, runtimePath, payload))
}

func withPiRuntimeEngine(payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["runtime_engine"] = "pi"
	return payload
}

func runtimeResponseData(body []byte) (any, bool) {
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, false
	}
	data, ok := decoded["data"]
	return data, ok
}

func runtimeResponseMap(body []byte) (map[string]any, bool) {
	data, ok := runtimeResponseData(body)
	if !ok {
		return nil, false
	}
	record, ok := data.(map[string]any)
	return record, ok
}

func runtimeResponseArray(body []byte) ([]any, bool) {
	data, ok := runtimeResponseData(body)
	if !ok {
		return nil, false
	}
	rows, ok := data.([]any)
	return rows, ok
}

func isCallableCommonProfile(profile map[string]any) bool {
	status := strings.TrimSpace(toString(profile["status"]))
	if status == "disabled" || status == "archived" {
		return false
	}
	return len(aiAgentContextTagsFromValue(profile["context_tags"])) == 0
}

func numericID(value any) uint64 {
	switch typed := value.(type) {
	case json.Number:
		id, _ := strconv.ParseUint(typed.String(), 10, 64)
		return id
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case int:
		if typed > 0 {
			return uint64(typed)
		}
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case uint64:
		return typed
	case string:
		id, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return id
	}
	return 0
}

func chatboxSessionURL(sessionID uint64) string {
	return "/store/ai-agents/chat/" + strconv.FormatUint(sessionID, 10)
}

func chatboxBusinessID(profileID uint64, versionKey string) string {
	return strconv.FormatUint(profileID, 10) + ":" + versionKey
}

func chatboxSessionProfileID(session map[string]any) uint64 {
	if profileID := numericID(session["profile_id"]); profileID > 0 {
		return profileID
	}
	return numericID(session["agent_profile_id"])
}

func chatboxSessionProfileVersionID(session map[string]any) uint64 {
	if versionID := numericID(session["profile_version_id"]); versionID > 0 {
		return versionID
	}
	return numericID(session["agent_profile_version_id"])
}

func chatboxSessionProfileVersionKey(session map[string]any) string {
	if versionKey := strings.TrimSpace(toString(session["profile_version_key"])); versionKey != "" {
		return versionKey
	}
	return strings.TrimSpace(toString(session["agent_profile_version_key"]))
}

func chatboxSessionPayload(profileID uint64, versionKey, firstSessionTimestamp, title string) map[string]any {
	return map[string]any{
		"profile_id":              profileID,
		"profile_version_id":      versionKey,
		"context_tags":            []string{},
		"session_kind":            "chat",
		"business_type":           "agent_profile",
		"business_id":             chatboxBusinessID(profileID, versionKey),
		"title":                   strings.TrimSpace(title),
		"source":                  "agent_chatbox",
		"first_session_timestamp": firstSessionTimestamp,
	}
}

func (h *AIAgentChatboxHandler) appendAudit(c *gin.Context, governance GovernanceContext, action string, targetID uint64, metadata map[string]any) {
	workspaceID := governance.WorkspaceID
	actorWorkspaceID := governance.WorkspaceID
	_ = services.AppendAuditLog(c.Request.Context(), h.DB, services.AuditInput{
		WorkspaceID:      &workspaceID,
		ActorUserID:      governance.UserID,
		ActorRole:        governance.SystemRole,
		ActorWorkspaceID: &actorWorkspaceID,
		Action:           action,
		TargetType:       "agent_chatbox",
		TargetID:         targetID,
		Metadata:         metadata,
		IP:               c.ClientIP(),
		UserAgent:        c.Request.UserAgent(),
	})
}

func (h *AIAgentChatboxHandler) queryExistingSession(c *gin.Context, governance GovernanceContext, profileID uint64, versionKey string) (map[string]any, bool) {
	response, err := h.callRuntime(c, governance, http.MethodPost, "/v1/sessions/query", map[string]any{
		"profile_id":    profileID,
		"context_tags":  []string{},
		"source":        "agent_chatbox",
		"business_type": "agent_profile",
		"business_id":   chatboxBusinessID(profileID, versionKey),
		"status":        "active",
	})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return nil, false
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return nil, false
	}
	rows, ok := runtimeResponseArray(response.Body)
	if !ok || len(rows) == 0 {
		return nil, true
	}
	session, ok := rows[0].(map[string]any)
	if !ok {
		return nil, true
	}
	if numericID(session["id"]) == 0 {
		return nil, true
	}
	return session, true
}

func (h *AIAgentChatboxHandler) createRuntimeSession(c *gin.Context, governance GovernanceContext, profileID uint64, versionKey string, payload map[string]any) {
	firstSessionTimestamp := strings.TrimSpace(toString(payload["first_session_timestamp"]))
	if firstSessionTimestamp == "" {
		firstSessionTimestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	runtimePayload := chatboxSessionPayload(profileID, versionKey, firstSessionTimestamp, toString(payload["title"]))
	response, err := h.callRuntime(c, governance, http.MethodPost, "/v1/sessions/current", runtimePayload)
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return
	}
	session, ok := runtimeResponseMap(response.Body)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": "AI Runtime session response invalid"})
		return
	}
	sessionID := numericID(session["id"])
	session["url"] = chatboxSessionURL(sessionID)
	h.appendAudit(c, governance, "agent_chatbox.session.opened", profileID, map[string]any{
		"session_id":              sessionID,
		"profile_id":              profileID,
		"version_key":             versionKey,
		"first_session_timestamp": firstSessionTimestamp,
		"restored":                false,
	})
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": session})
}

func estimateChatboxInputTokens(content string) int {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return 0
	}
	words := len(strings.Fields(trimmed))
	chars := int(math.Ceil(float64(len([]rune(trimmed))) / 4.0))
	if words > chars {
		return words
	}
	return chars
}

func chatboxInputHardLimit(profile map[string]any) int {
	inference, _ := profile["inference"].(map[string]any)
	maxTokens := numericID(inference["max_tokens"])
	if maxTokens == 0 {
		return 0
	}
	return int(maxTokens / 2)
}

func (h *AIAgentChatboxHandler) loadChatboxSession(c *gin.Context, governance GovernanceContext, sessionID string) (map[string]any, bool) {
	response, err := h.callRuntime(c, governance, http.MethodPost, "/v1/sessions/"+sessionID+"/query", map[string]any{})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return nil, false
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return nil, false
	}
	session, ok := runtimeResponseMap(response.Body)
	if !ok {
		writeAIRuntimeLocalError(c, http.StatusBadGateway, "AI Runtime session response invalid")
		return nil, false
	}
	if strings.TrimSpace(toString(session["source"])) != "agent_chatbox" {
		writeAIRuntimeLocalError(c, http.StatusForbidden, "该会话不是 Agent Chatbox 会话")
		return nil, false
	}
	return session, true
}

func (h *AIAgentChatboxHandler) loadProfileForSession(c *gin.Context, governance GovernanceContext, session map[string]any) (map[string]any, bool) {
	profileID := chatboxSessionProfileID(session)
	if profileID == 0 {
		writeAIRuntimeLocalError(c, http.StatusBadGateway, "AI Runtime session profile invalid")
		return nil, false
	}
	versionKey := chatboxSessionProfileVersionKey(session)
	versionID := chatboxSessionProfileVersionID(session)
	if versionKey == "" || versionKey == "latest" || versionID == 0 {
		response, err := h.callRuntime(c, governance, http.MethodPost, "/v1/agent-profiles/"+strconv.FormatUint(profileID, 10)+"/query", map[string]any{})
		if err != nil {
			writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
			return nil, false
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
			return nil, false
		}
		profile, ok := runtimeResponseMap(response.Body)
		if !ok {
			writeAIRuntimeLocalError(c, http.StatusBadGateway, "AI Runtime profile response invalid")
			return nil, false
		}
		return profile, true
	}
	response, err := h.callRuntime(c, governance, http.MethodPost, "/v1/agent-profiles/"+strconv.FormatUint(profileID, 10)+"/versions/query", map[string]any{})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return nil, false
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return nil, false
	}
	versions, ok := runtimeResponseArray(response.Body)
	if !ok {
		writeAIRuntimeLocalError(c, http.StatusBadGateway, "AI Runtime profile versions response invalid")
		return nil, false
	}
	for _, item := range versions {
		version, ok := item.(map[string]any)
		if !ok || numericID(version["profile_version_id"]) != versionID {
			continue
		}
		snapshot, ok := version["snapshot"].(map[string]any)
		if !ok {
			writeAIRuntimeLocalError(c, http.StatusBadGateway, "AI Runtime profile version snapshot invalid")
			return nil, false
		}
		return snapshot, true
	}
	writeAIRuntimeLocalError(c, http.StatusNotFound, "Agent Profile version not found")
	return nil, false
}

func (h *AIAgentChatboxHandler) enforceInputLimit(c *gin.Context, governance GovernanceContext, session map[string]any, content string) bool {
	profile, ok := h.loadProfileForSession(c, governance, session)
	if !ok {
		return false
	}
	if !isCallableCommonProfile(profile) {
		writeAIRuntimeLocalError(c, http.StatusForbidden, "Agent Profile 不可从 Chatbox 调用")
		return false
	}
	limit := chatboxInputHardLimit(profile)
	if limit > 0 && estimateChatboxInputTokens(content) > limit {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "输入超过 Agent Profile Max Tokens / 2 限制", gin.H{
			"data": gin.H{
				"limit_tokens":     limit,
				"estimated_tokens": estimateChatboxInputTokens(content),
			},
		})
		return false
	}
	return true
}

func (h *AIAgentChatboxHandler) ListProfiles(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	response, err := h.callRuntime(c, ctx, http.MethodPost, "/v1/agent-profiles/query", map[string]any{})
	if err != nil {
		writeAIRuntimeProxyError(c, aiRuntimeEnvelopeRequestID(c), err)
		return
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return
	}
	data, ok := runtimeResponseData(response.Body)
	if !ok {
		writeAIRuntimeBufferedResponse(c, response, aiRuntimeEnvelopeRequestID(c))
		return
	}
	rows, ok := data.([]any)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": []any{}})
		return
	}
	filtered := make([]any, 0, len(rows))
	for _, row := range rows {
		profile, ok := row.(map[string]any)
		if ok && isCallableCommonProfile(profile) {
			filtered = append(filtered, profile)
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": filtered})
}

func (h *AIAgentChatboxHandler) OpenSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	profileID := numericID(payload["agent_profile_id"])
	if profileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "agent_profile_id is required"})
		return
	}
	versionKey := strings.TrimSpace(toString(payload["agent_profile_version_id"]))
	if versionKey == "" {
		versionKey = "latest"
	}
	existing, ok := h.queryExistingSession(c, ctx, profileID, versionKey)
	if !ok {
		return
	}
	if existing != nil {
		sessionID := numericID(existing["id"])
		existing["url"] = chatboxSessionURL(sessionID)
		h.appendAudit(c, ctx, "agent_chatbox.session.opened", profileID, map[string]any{
			"session_id":  sessionID,
			"profile_id":  profileID,
			"version_key": versionKey,
			"restored":    true,
		})
		c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": existing})
		return
	}
	h.createRuntimeSession(c, ctx, profileID, versionKey, payload)
}

func (h *AIAgentChatboxHandler) CreateSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	profileID := numericID(payload["agent_profile_id"])
	if profileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "agent_profile_id is required"})
		return
	}
	versionKey := strings.TrimSpace(toString(payload["agent_profile_version_id"]))
	if versionKey == "" {
		versionKey = "latest"
	}
	h.createRuntimeSession(c, ctx, profileID, versionKey, payload)
}

func (h *AIAgentChatboxHandler) ListSessions(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload := map[string]any{
		"context_tags":  []string{},
		"source":        "agent_chatbox",
		"business_type": "agent_profile",
	}
	if profileID := strings.TrimSpace(c.Query("agent_profile_id")); profileID != "" {
		versionKey := strings.TrimSpace(c.Query("agent_profile_version_id"))
		if versionKey == "" {
			versionKey = "latest"
		}
		payload["profile_id"] = profileID
		payload["profile_version_id"] = versionKey
		payload["business_id"] = profileID + ":" + versionKey
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		payload["status"] = status
	}
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/query", payload)
}

func (h *AIAgentChatboxHandler) GetSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/query", map[string]any{})
}

func (h *AIAgentChatboxHandler) ListEntries(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	if _, ok := h.loadChatboxSession(c, ctx, c.Param("id")); !ok {
		return
	}
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/entries/query", map[string]any{})
}

func (h *AIAgentChatboxHandler) CreateEntryStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	content := strings.TrimSpace(toString(payload["content"]))
	if content == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "content is required")
		return
	}
	session, ok := h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	if !h.enforceInputLimit(c, ctx, session, content) {
		return
	}
	h.appendAudit(c, ctx, "agent_chatbox.message.sent", chatboxSessionProfileID(session), map[string]any{
		"session_id":       numericID(session["id"]),
		"profile_id":       chatboxSessionProfileID(session),
		"version_key":      chatboxSessionProfileVersionKey(session),
		"content_length":   len([]rune(content)),
		"estimated_tokens": estimateChatboxInputTokens(content),
	})
	h.forwardRuntimeStream(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/entries/stream", withPiRuntimeEngine(payload))
}

func (h *AIAgentChatboxHandler) CancelSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	sessionID := numericID(c.Param("id"))
	h.appendAudit(c, ctx, "agent_chatbox.generation.cancelled", sessionID, map[string]any{
		"session_id":     sessionID,
		"runtime_run_id": strings.TrimSpace(toString(payload["runtime_run_id"])),
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/cancel", payload)
}

func (h *AIAgentChatboxHandler) ArchiveSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	session, ok := h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	sessionID := numericID(session["id"])
	h.appendAudit(c, ctx, "agent_chatbox.session.archived", sessionID, map[string]any{
		"session_id": sessionID,
		"profile_id": chatboxSessionProfileID(session),
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/archive", map[string]any{})
}

func (h *AIAgentChatboxHandler) EnqueueQueueItem(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	_, ok = h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	sessionID := numericID(c.Param("id"))
	h.appendAudit(c, ctx, "agent_chatbox.queue.enqueued", sessionID, map[string]any{
		"session_id":     sessionID,
		"mode":           strings.TrimSpace(toString(payload["mode"])),
		"client_item_id": strings.TrimSpace(toString(payload["client_item_id"])),
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/queue/items", payload)
}

func (h *AIAgentChatboxHandler) ListQueueItems(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	_, ok = h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/queue/query", map[string]any{})
}

func (h *AIAgentChatboxHandler) CancelQueueItem(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	_, ok = h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	sessionID := numericID(c.Param("id"))
	queueItemID := strings.TrimSpace(c.Param("queue_item_id"))
	h.appendAudit(c, ctx, "agent_chatbox.queue.cancelled", sessionID, map[string]any{
		"session_id":    sessionID,
		"queue_item_id": queueItemID,
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/queue/items/"+url.PathEscape(queueItemID)+"/cancel", map[string]any{})
}

func (h *AIAgentChatboxHandler) ReorderQueueItems(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	_, ok = h.loadChatboxSession(c, ctx, c.Param("id"))
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	sessionID := numericID(c.Param("id"))
	h.appendAudit(c, ctx, "agent_chatbox.queue.reordered", sessionID, map[string]any{
		"session_id": sessionID,
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/queue/reorder", payload)
}

func (h *AIAgentChatboxHandler) UpdateSessionModel(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	sessionID := numericID(c.Param("id"))
	h.appendAudit(c, ctx, "agent_chatbox.model.switched", sessionID, map[string]any{
		"session_id": sessionID,
		"provider":   payload["provider"],
		"model":      payload["model"],
		"inference":  payload["inference"],
	})
	h.forwardRuntime(c, ctx, http.MethodPut, "/v1/sessions/"+c.Param("id")+"/model", payload)
}

func (h *AIAgentChatboxHandler) ContinueSessionStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	if strings.TrimSpace(toString(payload["content"])) == "" {
		payload["content"] = "继续"
	}
	sessionID := numericID(c.Param("id"))
	h.appendAudit(c, ctx, "agent_chatbox.generation.continued", sessionID, map[string]any{
		"session_id": sessionID,
	})
	h.forwardRuntimeStream(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/continue/stream", withPiRuntimeEngine(payload))
}

func (h *AIAgentChatboxHandler) DecideActionStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	actionID := strings.TrimSpace(c.Param("id"))
	auditTargetID := numericID(actionID)
	if auditTargetID == 0 {
		auditTargetID = ctx.WorkspaceID
	}
	h.appendAudit(c, ctx, "agent_chatbox.action.decided", auditTargetID, map[string]any{
		"action_id": actionID,
		"decision":  payload["decision"],
	})
	h.forwardRuntimeStream(c, ctx, http.MethodPost, "/v1/actions/"+c.Param("id")+"/decision/stream", payload)
}

func (h *AIAgentChatboxHandler) DecidePiApproval(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	runtimeRunID := strings.TrimSpace(c.Param("runtime_run_id"))
	if runtimeRunID == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "runtime_run_id is required")
		return
	}
	if runWorkspaceID := aiRuntimeWorkspaceIDFromRunID(runtimeRunID); runWorkspaceID == 0 || runWorkspaceID != ctx.WorkspaceID {
		writeAIRuntimeLocalError(c, http.StatusNotFound, "Runtime run not found")
		return
	}
	h.appendAudit(c, ctx, "agent_chatbox.pi_approval.decided", ctx.WorkspaceID, map[string]any{
		"runtime_run_id": runtimeRunID,
		"decision":       payload["decision"],
	})
	h.forwardRuntime(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/pi-approval/decision", payload)
}

func (h *AIAgentChatboxHandler) DecidePiApprovalStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	runtimeRunID := strings.TrimSpace(c.Param("runtime_run_id"))
	if runtimeRunID == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "runtime_run_id is required")
		return
	}
	if runWorkspaceID := aiRuntimeWorkspaceIDFromRunID(runtimeRunID); runWorkspaceID == 0 || runWorkspaceID != ctx.WorkspaceID {
		writeAIRuntimeLocalError(c, http.StatusNotFound, "Runtime run not found")
		return
	}
	h.appendAudit(c, ctx, "agent_chatbox.pi_approval.decided", ctx.WorkspaceID, map[string]any{
		"runtime_run_id": runtimeRunID,
		"decision":       payload["decision"],
	})
	h.forwardRuntimeStream(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/pi-approval/decision/stream", payload)
}

func (h *AIAgentChatboxHandler) ListRunEvents(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	runtimePath, payload, ok := aiRuntimeRunEventsQuery(c, ctx)
	if !ok {
		return
	}
	h.forwardRuntime(c, ctx, http.MethodPost, runtimePath, payload)
}

func (h *AIAgentChatboxHandler) StreamRunEvents(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	runtimeRunID := strings.TrimSpace(c.Param("runtime_run_id"))
	if runtimeRunID == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "runtime_run_id is required")
		return
	}
	if runWorkspaceID := aiRuntimeWorkspaceIDFromRunID(runtimeRunID); runWorkspaceID == 0 || runWorkspaceID != ctx.WorkspaceID {
		writeAIRuntimeLocalError(c, http.StatusNotFound, "Runtime run not found")
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forwardRuntimeStream(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/events/stream", payload)
}

func (h *AIAgentChatboxHandler) GetArtifact(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	runtimePath, payload, ok := aiRuntimeArtifactQuery(c, ctx)
	if !ok {
		return
	}
	h.forwardRuntime(c, ctx, http.MethodPost, runtimePath, payload)
}
