package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"easydo-server/internal/services"

	"github.com/gin-gonic/gin"
)

const streamableHTTPProtocol = "streamable_http"

type inFlightLimiter struct {
	limit  int
	mu     sync.Mutex
	counts map[string]int
}

func newInFlightLimiter(limit int) *inFlightLimiter {
	return &inFlightLimiter{limit: limit, counts: make(map[string]int)}
}

func (l *inFlightLimiter) acquire(key string) bool {
	if l == nil || l.limit <= 0 || key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts[key] >= l.limit {
		return false
	}
	l.counts[key]++
	return true
}

func (l *inFlightLimiter) release(key string) {
	if l == nil || l.limit <= 0 || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts[key] <= 1 {
		delete(l.counts, key)
		return
	}
	l.counts[key]--
}

func (s *Server) RegisterRoutes(router gin.IRouter) {
	router.POST("/mcp", s.handleStreamableHTTP)
	router.GET("/mcp/sse", s.handleSSEConnect)
	router.POST("/mcp/sse/:session_id/message", s.handleSSEMessage)
}

func (s *Server) handleStreamableHTTP(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), s.requestTimeout)
	defer cancel()

	if err := s.checkOrigin(c.GetHeader("Origin")); err != nil {
		s.recordFailure(ctx, AuditRecordInput{
			OperationType: OperationRead,
			Protocol:      streamableHTTPProtocol,
			InputSummary:  map[string]any{"path": c.Request.URL.Path},
		}, err)
		c.Status(httpStatusForError(err))
		return
	}

	req, parseErr := s.parseJSONRPCRequest(c.Writer, c.Request)
	auditInput := auditInputForRequest(req)
	if parseErr != nil {
		s.recordFailure(ctx, auditInput, parseErr)
		s.writeJSONRPCParseError(c, req.ID, parseErr)
		return
	}

	actor, authErr := AuthenticateRequest(ctx, s.db, c.GetHeader("Authorization"))
	if authErr != nil {
		s.recordFailure(ctx, auditInput, authErr)
		s.writeJSONRPCError(c, req.ID, authErr)
		return
	}
	auditInput.UserID = actor.UserID

	userKey := strconv.FormatUint(actor.UserID, 10)
	if !s.userLimiter.acquire(userKey) {
		err := services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many in flight requests"}
		s.recordOutcome(ctx, auditInput, AuditStatusConcurrencyLimited, err, time.Now())
		s.writeJSONRPCError(c, req.ID, err)
		return
	}
	defer s.userLimiter.release(userKey)

	ipKey := c.ClientIP()
	if !s.ipLimiter.acquire(ipKey) {
		err := services.ServiceError{Code: services.ErrorCodeConcurrencyLimited, Message: "too many in flight requests"}
		s.recordOutcome(ctx, auditInput, AuditStatusConcurrencyLimited, err, time.Now())
		s.writeJSONRPCError(c, req.ID, err)
		return
	}
	defer s.ipLimiter.release(ipKey)

	resp, respond := s.handleJSONRPC(ctx, req, actor, auditInput)
	if !respond {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Server) parseJSONRPCRequest(w http.ResponseWriter, r *http.Request) (JSONRPCRequest, error) {
	var req JSONRPCRequest
	body := http.MaxBytesReader(w, r.Body, s.maxBodySize)
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		return req, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "invalid JSON-RPC request"}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "invalid JSON-RPC request"}
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return req, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "invalid JSON-RPC request"}
	}
	return req, nil
}

func (s *Server) checkOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	if len(s.allowedOrigins) == 0 {
		return services.ServiceError{Code: services.ErrorCodeForbidden, Message: "origin is not allowed"}
	}
	if _, ok := s.allowedOrigins[origin]; ok {
		return nil
	}
	return services.ServiceError{Code: services.ErrorCodeForbidden, Message: "origin is not allowed"}
}

func (s *Server) handleJSONRPC(ctx context.Context, req JSONRPCRequest, actor services.ActorContext, auditInput AuditRecordInput) (*JSONRPCResponse, bool) {
	if req.ID == nil {
		_, _ = s.executeMethod(ctx, req, actor, auditInput)
		return nil, false
	}
	resp := &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
	result, err := s.executeMethod(ctx, req, actor, auditInput)
	if err != nil {
		resp.Error = MapError(err)
		return resp, true
	}
	resp.Result = result
	return resp, true
}

func (s *Server) executeMethod(ctx context.Context, req JSONRPCRequest, actor services.ActorContext, auditInput AuditRecordInput) (any, error) {
	switch req.Method {
	case "initialize":
		result, err := WrapAudit(ctx, s.auditRecorder, auditInput, func(context.Context) (AuditResult, error) {
			out := map[string]any{
				"protocolVersion": mcpProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "easydo-mcp-server", "version": "0.1.0"},
			}
			return AuditResult{OutputSummary: out}, nil
		})
		return result.OutputSummary, err
	case "ping":
		result, err := WrapAudit(ctx, s.auditRecorder, auditInput, func(context.Context) (AuditResult, error) {
			out := map[string]any{}
			return AuditResult{OutputSummary: out}, nil
		})
		return result.OutputSummary, err
	case "tools/list":
		out := map[string]any{"tools": s.toolMetadata()}
		_, err := WrapAudit(ctx, s.auditRecorder, auditInput, func(context.Context) (AuditResult, error) {
			return AuditResult{OutputSummary: map[string]any{"tool_count": len(s.registry.List())}}, nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	case "tools/call":
		call, err := parseToolCallParams(req.Params)
		if err != nil {
			s.recordFailure(ctx, auditInput, err)
			return nil, err
		}
		tool, ok := s.registry.Get(call.Name)
		if ok {
			auditInput.ToolName = tool.Name
			auditInput.OperationType = tool.OperationType
			auditInput.TargetType = tool.TargetType
		}
		if !ok {
			err := services.ServiceError{Code: services.ErrorCodeNotFound, Message: "tool not found"}
			s.recordFailure(ctx, auditInput, err)
			return nil, err
		}
		result, err := WrapAudit(ctx, s.auditRecorder, auditInput, func(ctx context.Context) (AuditResult, error) {
			toolResult, err := s.registry.Invoke(ctx, call.Name, Invocation{Actor: actor, Arguments: call.Arguments, RequestID: requestIDString(req.ID), Protocol: streamableHTTPProtocol})
			if err != nil {
				return AuditResult{}, err
			}
			out := toolCallResult(toolResult)
			return AuditResult{OutputSummary: out}, nil
		})
		if err != nil {
			return nil, err
		}
		return result.OutputSummary, nil
	default:
		err := services.ServiceError{Code: services.ErrorCodeNotFound, Message: "method not found"}
		s.recordFailure(ctx, auditInput, err)
		return nil, err
	}
}

func (s *Server) toolMetadata() []any {
	registered := s.registry.List()
	tools := make([]any, 0, len(registered))
	for _, tool := range registered {
		tools = append(tools, map[string]any{
			"name":          tool.Name,
			"description":   tool.Description,
			"inputSchema":   tool.InputSchema,
			"operationType": string(tool.OperationType),
			"targetType":    tool.TargetType,
		})
	}
	return tools
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func parseToolCallParams(raw json.RawMessage) (toolCallParams, error) {
	var params toolCallParams
	if len(raw) == 0 {
		return params, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "tool name is required"}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return params, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "invalid tool call params"}
	}
	if params.Name == "" {
		return params, services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "tool name is required"}
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	return params, nil
}

func toolCallResult(result ToolResult) map[string]any {
	out := map[string]any{}
	if result.Content != nil {
		out["content"] = result.Content
	} else if result.StructuredContent != nil {
		data, _ := json.Marshal(result.StructuredContent)
		out["content"] = []any{map[string]any{"type": "text", "text": string(data)}}
	} else {
		out["content"] = []any{}
	}
	if result.StructuredContent != nil {
		out["structuredContent"] = result.StructuredContent
	}
	if result.Metadata != nil {
		out["metadata"] = result.Metadata
	}
	return out
}

func auditInputForRequest(req JSONRPCRequest) AuditRecordInput {
	return AuditRecordInput{
		RequestID:     requestIDString(req.ID),
		ToolName:      req.Method,
		OperationType: OperationRead,
		Protocol:      streamableHTTPProtocol,
		InputSummary:  map[string]any{"method": req.Method},
	}
}

func requestIDString(id any) string {
	switch v := id.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
}

func (s *Server) recordFailure(ctx context.Context, input AuditRecordInput, err error) {
	_ = RecordAuditOutcome(ctx, s.auditRecorder, AuditOutcomeInput{AuditRecordInput: input, Status: classifyAuditStatus(ctx, err), Err: safeError(err)})
}

func (s *Server) recordOutcome(ctx context.Context, input AuditRecordInput, status AuditStatus, err error, started time.Time) {
	_ = RecordAuditOutcome(ctx, s.auditRecorder, AuditOutcomeInput{AuditRecordInput: input, Status: status, Err: safeError(err), StartedAt: started, FinishedAt: time.Now()})
}

func (s *Server) writeJSONRPCParseError(c *gin.Context, id any, err error) {
	c.JSON(http.StatusOK, JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: MapError(safeError(err))})
}

func (s *Server) writeJSONRPCError(c *gin.Context, id any, err error) {
	if id == nil {
		c.Status(http.StatusNoContent)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = services.ServiceError{Code: services.ErrorCodeTimeout, Message: "request timed out"}
	}
	c.JSON(http.StatusOK, JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: MapError(safeError(err))})
}

func safeError(err error) error {
	if err == nil {
		return nil
	}
	var svcErr services.ServiceError
	if errors.As(err, &svcErr) {
		return svcErr
	}
	return services.ServiceError{Code: services.ErrorCodeInternalError, Message: "internal error"}
}
