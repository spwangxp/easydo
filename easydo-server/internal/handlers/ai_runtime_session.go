package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIRuntimeSessionHandler struct {
	DB            *gorm.DB
	RuntimeClient *services.AIRuntimeClient
}

func NewAIRuntimeSessionHandler() *AIRuntimeSessionHandler {
	return &AIRuntimeSessionHandler{
		DB:            models.DB,
		RuntimeClient: services.NewAIRuntimeClientFromConfig(),
	}
}

func (h *AIRuntimeSessionHandler) runtimeClient() *services.AIRuntimeClient {
	if h.RuntimeClient != nil {
		return h.RuntimeClient
	}
	return services.NewAIRuntimeClientFromConfig()
}

func (h *AIRuntimeSessionHandler) accessAllowed(c *gin.Context) (GovernanceContext, bool) {
	ctx := aiGovernanceContext(c, h.DB)
	if ctx.WorkspaceID == 0 || !userCanAccessWorkspace(h.DB, ctx.WorkspaceID, ctx.UserID, ctx.SystemRole) {
		c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "无权访问 AI Runtime"})
		return ctx, false
	}
	return ctx, true
}

func canUseAIRuntimeContextTags(ctx GovernanceContext, contextTags []string) bool {
	if ctx.WorkspaceID == 0 || !RequireNormalWorkspaceKind(ctx) {
		return false
	}
	if isAdminRole(ctx.SystemRole) || ctx.WorkspaceRole == models.WorkspaceRoleOwner {
		return true
	}
	if containsReservedPageAssistantTag(contextTags) {
		return false
	}
	return middleware.WorkspaceRoleAtLeast(ctx.WorkspaceRole, models.WorkspaceRoleDeveloper)
}

func aiRuntimeContextTags(payload map[string]any) []string {
	return aiAgentContextTagsFromValue(payload["context_tags"])
}

func rejectAIRuntimeContextTagUse(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "message": "当前工作空间角色无权使用该 AI 上下文标签"})
}

func hasLegacyAIRuntimeSceneFields(payload map[string]any) bool {
	for _, key := range []string{"scene", "scene_type", "scene_code"} {
		if _, exists := payload[key]; exists {
			return true
		}
	}
	return false
}

func rejectLegacyAIRuntimeSceneFields(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "scene_type/scene_code 已删除，请使用 context_tags 和 profile_id"})
}

func aiRuntimeWorkspaceIDFromRunID(runtimeRunID string) uint64 {
	runtimeRunID = strings.TrimSpace(strings.ToLower(runtimeRunID))
	if !strings.HasPrefix(runtimeRunID, "r_w") {
		return 0
	}
	rest := strings.TrimPrefix(runtimeRunID, "r_w")
	parts := strings.Split(rest, "_")
	if len(parts) < 3 || parts[0] == "" {
		return 0
	}
	value, err := strconv.ParseUint(parts[0], 36, 64)
	if err != nil {
		return 0
	}
	return value
}

func aiRuntimeWorkspaceIDFromArtifactID(artifactID string) uint64 {
	artifactID = strings.TrimSpace(strings.ToLower(artifactID))
	if !strings.HasPrefix(artifactID, "art_w") {
		return 0
	}
	rest := strings.TrimPrefix(artifactID, "art_w")
	parts := strings.Split(rest, "_")
	if len(parts) < 4 || parts[0] == "" {
		return 0
	}
	value, err := strconv.ParseUint(parts[0], 36, 64)
	if err != nil {
		return 0
	}
	return value
}

func aiRuntimeEnvelopeKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "none"
	}
	var builder strings.Builder
	lastDash := false
	for _, ch := range value {
		allowed := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '.' || ch == ':' || ch == '-'
		if allowed {
			builder.WriteRune(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "none"
	}
	return result
}

func aiRuntimePayloadClientEntryID(payload map[string]any) string {
	value, _ := payload["client_entry_id"].(string)
	return strings.TrimSpace(value)
}

const aiRuntimeRequestIDContextKey = "ai_runtime_request_id"

func isValidAIRuntimeRequestID(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		switch ch {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func aiRuntimeEnvelopeRequestID(c *gin.Context) string {
	if existing := c.GetString(aiRuntimeRequestIDContextKey); isValidAIRuntimeRequestID(existing) {
		return existing
	}
	requestID := c.GetHeader("X-Request-ID")
	if !isValidAIRuntimeRequestID(requestID) {
		requestID = "req-" + uuid.NewString()
	}
	c.Set(aiRuntimeRequestIDContextKey, requestID)
	return requestID
}

func aiRuntimeEnvelopeIdempotencyKey(c *gin.Context, governance GovernanceContext, runtimePath string, payload map[string]any) string {
	if key := strings.TrimSpace(c.GetHeader("X-Idempotency-Key")); key != "" {
		return key
	}
	if clientEntryID := aiRuntimePayloadClientEntryID(payload); clientEntryID != "" {
		return "entry:client:" + aiRuntimeEnvelopeKeyPart(clientEntryID)
	}
	return fmt.Sprintf("runtime:w%d:%s", governance.WorkspaceID, aiRuntimeEnvelopeKeyPart(runtimePath))
}

func normalizeAIRuntimeSessionPayload(c *gin.Context, payload map[string]any, requireProfile bool) bool {
	if hasLegacyAIRuntimeSceneFields(payload) {
		rejectLegacyAIRuntimeSceneFields(c)
		return false
	}
	if requireProfile && numericID(payload["profile_id"]) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "profile_id is required"})
		return false
	}
	tags := aiRuntimeContextTags(payload)
	if msg := validateAIAgentContextTags(tags); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": msg})
		return false
	}
	payload["context_tags"] = tags
	return true
}

func (h *AIRuntimeSessionHandler) forward(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) {
	client := h.runtimeClient()
	requestID := aiRuntimeEnvelopeRequestID(c)
	idempotencyKey := aiRuntimeEnvelopeIdempotencyKey(c, governance, runtimePath, payload)
	envelope := services.AIRuntimeEnvelope{
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
	forwardAIRuntimeBufferedResponse(c, client, method, runtimePath, envelope)
}

func (h *AIRuntimeSessionHandler) forwardStream(c *gin.Context, governance GovernanceContext, method, runtimePath string, payload map[string]any) {
	client := h.runtimeClient()
	requestID := aiRuntimeEnvelopeRequestID(c)
	idempotencyKey := aiRuntimeEnvelopeIdempotencyKey(c, governance, runtimePath, payload)
	envelope := services.AIRuntimeEnvelope{
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
	forwardAIRuntimeStreamResponse(c, client, method, runtimePath, envelope)
}

func aiRuntimeRunEventsQuery(c *gin.Context, governance GovernanceContext) (string, map[string]any, bool) {
	runtimeRunID := strings.TrimSpace(c.Param("runtime_run_id"))
	if runtimeRunID == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "runtime_run_id is required")
		return "", nil, false
	}
	if runWorkspaceID := aiRuntimeWorkspaceIDFromRunID(runtimeRunID); runWorkspaceID == 0 || runWorkspaceID != governance.WorkspaceID {
		writeAIRuntimeLocalError(c, http.StatusNotFound, "Runtime run not found")
		return "", nil, false
	}
	payload := map[string]any{}
	for _, key := range []string{"child_run_link_id", "parent_runtime_run_id", "parent_action_id", "parent_entry_id", "after_event_id"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			payload[key] = value
		}
	}
	return "/v1/runs/" + url.PathEscape(runtimeRunID) + "/events/query", payload, true
}

func writeSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
}

func writeAIRuntimeLocalError(c *gin.Context, status int, message string, extras ...gin.H) {
	requestID := aiRuntimeEnvelopeRequestID(c)
	body := gin.H{}
	for _, extra := range extras {
		for key, value := range extra {
			body[key] = value
		}
	}
	body["code"] = status
	body["message"] = message
	body["request_id"] = requestID
	c.Header("X-Request-ID", requestID)
	c.JSON(status, body)
}

func writeAIRuntimeProxyError(c *gin.Context, requestID string, err error) {
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}
	if services.IsAIRuntimeTransportError(err) {
		c.JSON(http.StatusBadGateway, gin.H{
			"code":       "ai_runtime_transport_error",
			"message":    "AI Runtime is unavailable",
			"request_id": requestID,
		})
		return
	}
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":       "ai_runtime_response_error",
		"message":    "AI Runtime response could not be proxied",
		"request_id": requestID,
	})
}

// Keep operational retry, quota, and trace metadata without forwarding cookies,
// credentials, or hop-by-hop transport headers from the Runtime response.
var aiRuntimeResponseHeaderAllowlist = []string{
	"Retry-After",
	"RateLimit",
	"RateLimit-Policy",
	"RateLimit-Limit",
	"RateLimit-Remaining",
	"RateLimit-Reset",
	"X-RateLimit-Limit",
	"X-RateLimit-Remaining",
	"X-RateLimit-Reset",
	"Traceparent",
	"Tracestate",
	"B3",
	"X-B3-TraceId",
	"X-B3-SpanId",
	"X-B3-Sampled",
	"X-B3-Flags",
	"X-Trace-ID",
}

func copyAIRuntimeAllowedResponseHeaders(c *gin.Context, upstream http.Header) {
	for _, name := range aiRuntimeResponseHeaderAllowlist {
		values := upstream.Values(name)
		if len(values) == 0 {
			continue
		}
		canonicalName := http.CanonicalHeaderKey(name)
		c.Writer.Header()[canonicalName] = append([]string(nil), values...)
	}
}

func writeAIRuntimeBufferedResponse(c *gin.Context, response *services.AIRuntimeResponse, fallbackRequestID string) {
	copyAIRuntimeAllowedResponseHeaders(c, response.Header)
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	requestID := response.Header.Get("X-Request-ID")
	if !isValidAIRuntimeRequestID(requestID) {
		requestID = fallbackRequestID
	}
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}
	c.Data(response.StatusCode, contentType, response.Body)
}

func forwardAIRuntimeBufferedResponse(c *gin.Context, client *services.AIRuntimeClient, method, runtimePath string, envelope services.AIRuntimeEnvelope) {
	response, err := client.ForwardResponse(c.Request.Context(), method, runtimePath, envelope)
	if err != nil {
		writeAIRuntimeProxyError(c, envelope.RequestID, err)
		return
	}
	writeAIRuntimeBufferedResponse(c, response, envelope.RequestID)
}

type aiRuntimeFlushWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

func (writer aiRuntimeFlushWriter) Write(data []byte) (int, error) {
	written, err := writer.writer.Write(data)
	if written > 0 && writer.flusher != nil {
		writer.flusher.Flush()
	}
	return written, err
}

func forwardAIRuntimeStreamResponse(c *gin.Context, client *services.AIRuntimeClient, method, runtimePath string, envelope services.AIRuntimeEnvelope) {
	response, err := client.ForwardStream(c.Request.Context(), method, runtimePath, envelope)
	if err != nil {
		writeAIRuntimeProxyError(c, envelope.RequestID, err)
		return
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			_ = c.Error(fmt.Errorf("close ai runtime response body failed: %w", closeErr))
		}
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			writeAIRuntimeProxyError(c, envelope.RequestID, fmt.Errorf("read ai runtime error response failed: %w", readErr))
			return
		}
		writeAIRuntimeBufferedResponse(c, &services.AIRuntimeResponse{
			StatusCode: response.StatusCode,
			Header:     response.Header.Clone(),
			Body:       body,
		}, envelope.RequestID)
		return
	}

	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/event-stream"
	}
	writeSSEHeaders(c)
	copyAIRuntimeAllowedResponseHeaders(c, response.Header)
	c.Header("Content-Type", contentType)
	requestID := response.Header.Get("X-Request-ID")
	if !isValidAIRuntimeRequestID(requestID) {
		requestID = envelope.RequestID
	}
	if requestID != "" {
		c.Header("X-Request-ID", requestID)
	}
	c.Status(response.StatusCode)
	flusher, _ := c.Writer.(http.Flusher)
	if _, copyErr := io.Copy(aiRuntimeFlushWriter{writer: c.Writer, flusher: flusher}, response.Body); copyErr != nil {
		_ = c.Error(fmt.Errorf("copy ai runtime stream failed: %w", copyErr))
	}
}

func aiRuntimeArtifactQuery(c *gin.Context, governance GovernanceContext) (string, map[string]any, bool) {
	artifactID := strings.TrimSpace(c.Param("artifact_id"))
	if artifactID == "" {
		writeAIRuntimeLocalError(c, http.StatusBadRequest, "artifact_id is required")
		return "", nil, false
	}
	if artifactWorkspaceID := aiRuntimeWorkspaceIDFromArtifactID(artifactID); artifactWorkspaceID == 0 || artifactWorkspaceID != governance.WorkspaceID {
		writeAIRuntimeLocalError(c, http.StatusNotFound, "Runtime artifact not found")
		return "", nil, false
	}
	payload := map[string]any{}
	for _, key := range []string{"child_run_link_id", "parent_runtime_run_id", "parent_action_id", "parent_entry_id"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			payload[key] = value
		}
	}
	return "/v1/artifacts/" + url.PathEscape(artifactID) + "/query", payload, true
}

func aiRuntimeSessionQueryPayload(c *gin.Context) map[string]any {
	payload := map[string]any{}
	for _, key := range []string{"profile_id", "profile_version_id", "business_type", "business_id", "status", "source", "session_kind"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			payload[key] = value
		}
	}
	if value := strings.TrimSpace(c.Query("context_tags")); value != "" {
		payload["context_tags"] = aiAgentContextTagsFromValue(value)
	}
	return payload
}

func (h *AIRuntimeSessionHandler) CurrentSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	if !normalizeAIRuntimeSessionPayload(c, payload, true) {
		return
	}
	if !canUseAIRuntimeContextTags(ctx, aiRuntimeContextTags(payload)) {
		rejectAIRuntimeContextTagUse(c)
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/current", payload)
}

func (h *AIRuntimeSessionHandler) ListSessions(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload := aiRuntimeSessionQueryPayload(c)
	if strings.TrimSpace(c.Query("scene_type")) != "" || strings.TrimSpace(c.Query("scene_code")) != "" {
		rejectLegacyAIRuntimeSceneFields(c)
		return
	}
	if !normalizeAIRuntimeSessionPayload(c, payload, false) {
		return
	}
	if !canUseAIRuntimeContextTags(ctx, aiRuntimeContextTags(payload)) {
		rejectAIRuntimeContextTagUse(c)
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/query", payload)
}

func (h *AIRuntimeSessionHandler) GetSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/query", map[string]any{})
}

func (h *AIRuntimeSessionHandler) ListEntries(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/entries/query", map[string]any{})
}

func (h *AIRuntimeSessionHandler) CreateEntry(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/entries", payload)
}

func (h *AIRuntimeSessionHandler) CreateEntryStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forwardStream(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/entries/stream", payload)
}

func (h *AIRuntimeSessionHandler) CancelSession(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/sessions/"+c.Param("id")+"/cancel", payload)
}

func (h *AIRuntimeSessionHandler) UpdateSessionModel(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPut, "/v1/sessions/"+c.Param("id")+"/model", payload)
}

func (h *AIRuntimeSessionHandler) DecideAction(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, "/v1/actions/"+c.Param("id")+"/decision", payload)
}

func (h *AIRuntimeSessionHandler) DecideActionStream(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	payload, ok := readOptionalJSONMap(c)
	if !ok {
		return
	}
	h.forwardStream(c, ctx, http.MethodPost, "/v1/actions/"+c.Param("id")+"/decision/stream", payload)
}

func (h *AIRuntimeSessionHandler) DecidePiApproval(c *gin.Context) {
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
	h.forward(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/pi-approval/decision", payload)
}

func (h *AIRuntimeSessionHandler) DecidePiApprovalStream(c *gin.Context) {
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
	h.forwardStream(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/pi-approval/decision/stream", payload)
}

func (h *AIRuntimeSessionHandler) ListRunEvents(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	runtimePath, payload, ok := aiRuntimeRunEventsQuery(c, ctx)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, runtimePath, payload)
}

func (h *AIRuntimeSessionHandler) StreamRunEvents(c *gin.Context) {
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
	h.forwardStream(c, ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runtimeRunID)+"/events/stream", payload)
}

func (h *AIRuntimeSessionHandler) GetArtifact(c *gin.Context) {
	ctx, ok := h.accessAllowed(c)
	if !ok {
		return
	}
	runtimePath, payload, ok := aiRuntimeArtifactQuery(c, ctx)
	if !ok {
		return
	}
	h.forward(c, ctx, http.MethodPost, runtimePath, payload)
}
