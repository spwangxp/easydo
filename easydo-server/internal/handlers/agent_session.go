package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"easydo-server/internal/agent"
	"easydo-server/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AgentSessionHandler — 新 Agent Runtime 的 HTTP 处理器
// 直接使用 Go 后端的 Event Store + Projection，不再代理到 easydo-ai-runtime
// Pi Agent Runtime 作为独立服务通过 HTTP 回调本 API 来发布事件
type AgentSessionHandler struct {
	DB      *gorm.DB
	Manager *agent.SessionManager
}

func NewAgentSessionHandler() *AgentSessionHandler {
	mgr, err := agent.NewSessionManager(models.DB)
	if err != nil {
		panic(fmt.Sprintf("init session manager: %v", err))
	}
	return &AgentSessionHandler{
		DB:      models.DB,
		Manager: mgr,
	}
}

// ListSessions — GET /api/v2/agent/sessions
func (h *AgentSessionHandler) ListSessions(c *gin.Context) {
	sessions := h.Manager.ListSessions()
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": sessions,
	})
}

// CreateSession — POST /api/v2/agent/sessions
func (h *AgentSessionHandler) CreateSession(c *gin.Context) {
	var req struct {
		Agent string      `json:"agent"`
		Model agent.ModelRef `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	id, err := h.Manager.CreateSession(ctx, req.Agent, req.Model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{"id": id},
	})
}

// GetProjection — GET /api/v2/agent/sessions/:id/projection
func (h *AgentSessionHandler) GetProjection(c *gin.Context) {
	sessionID := c.Param("id")
	proj := h.Manager.GetProjection(sessionID)
	if proj == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "session not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": proj,
	})
}

// EventStream — GET /api/v2/agent/sessions/:id/events (SSE)
// 前端通过 SSE 实时接收事件，自动重放历史事件 + 接收新事件
func (h *AgentSessionHandler) EventStream(c *gin.Context) {
	sessionID := c.Param("id")
	afterSeqStr := c.DefaultQuery("after_seq", "0")
	afterSeq, _ := strconv.ParseInt(afterSeqStr, 10, 64)

	ch, cleanup := h.Manager.SubscribeEvents(sessionID, afterSeq)
	defer cleanup()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, _ := c.Writer.(http.Flusher)
	enc := json.NewEncoder(c.Writer)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			// SSE format: event: <type>\ndata: <json>\n\n
			_, _ = io.WriteString(c.Writer, fmt.Sprintf("event: %s\n", evt.Type))
			_, _ = io.WriteString(c.Writer, "data: ")
			_ = enc.Encode(evt)
			_, _ = io.WriteString(c.Writer, "\n")
			flusher.Flush()
		}
	}
}

// Prompt — POST /api/v2/agent/sessions/:id/prompt
// 向 session 发送 prompt，Pi Runtime 会处理并产生事件
// 返回时事件已持久化，前端通过 SSE 接收
func (h *AgentSessionHandler) Prompt(c *gin.Context) {
	sessionID := c.Param("id")
	var req struct {
		Text  string         `json:"text"`
		Files []agent.FileRef `json:"files,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	msgID := agent.MessageID(fmt.Sprintf("msg_%d", time.Now().UnixNano()))

	// 发布 prompted 事件
	_, err := h.Manager.PublishEvent(ctx, &agent.PromptedEvent{
		Type:      "session.prompted",
		EventBase: agent.EventBase{SessionID: sessionID, Timestamp: time.Now().UTC()},
		MessageID: msgID,
		Prompt:    req.Text,
		Files:     req.Files,
		Delivery:  "prompt",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	// 发布 step started 事件（实际应由 Pi 发布，这里先占位）
	assistantMsgID := agent.MessageID(fmt.Sprintf("asst_%d", time.Now().UnixNano()))
	_, _ = h.Manager.PublishEvent(ctx, &agent.StepStartedEvent{
		Type:               "session.step.started",
		EventBase:          agent.EventBase{SessionID: sessionID, Timestamp: time.Now().UTC()},
		AssistantMessageID: assistantMsgID,
		Agent:              "",
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"message_id":         msgID,
			"assistant_message_id": assistantMsgID,
		},
	})
}

// Approve — POST /api/v2/agent/sessions/:id/approval/:request_id
func (h *AgentSessionHandler) Approve(c *gin.Context) {
	sessionID := c.Param("id")
	requestID := c.Param("request_id")
	var req struct {
		Result string `json:"result"` // "approved" | "rejected"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}

	ctx := c.Request.Context()
	_, err := h.Manager.PublishEvent(ctx, &agent.ApprovalResolvedEvent{
		Type:      "permission.resolved",
		EventBase: agent.EventBase{SessionID: sessionID, Timestamp: time.Now().UTC()},
		RequestID: requestID,
		Result:    req.Result,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200})
}

// ============================================================
// Internal API — Pi Runtime 回调本接口来发布事件
// ============================================================

// IngestEvent — POST /api/v2/agent/internal/events
// Pi Runtime 在处理完一个 agent step 后回调此接口发布事件
func (h *AgentSessionHandler) IngestEvent(c *gin.Context) {
	var raw struct {
		SessionID string          `json:"session_id"`
		Type      string          `json:"type"`
		Data      json.RawMessage `json:"data"`
	}
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid event"})
		return
	}

	ctx := c.Request.Context()
	evt, err := agent.UnmarshalEvent(raw.Type, raw.Data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	// 注入 SessionID
	if hasSession, ok := evt.(agent.HasSessionIDSetter); ok {
		hasSession.SetSessionID(raw.SessionID)
	}

	stored, err := h.Manager.PublishEvent(ctx, evt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"seq": stored.Seq}})
}
