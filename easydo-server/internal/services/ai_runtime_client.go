package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"easydo-server/internal/config"
)

type AIRuntimeActor struct {
	UserID        uint64 `json:"user_id"`
	Username      string `json:"username"`
	SystemRole    string `json:"system_role"`
	WorkspaceID   uint64 `json:"workspace_id"`
	WorkspaceRole string `json:"workspace_role"`
	AuthSessionID string `json:"auth_session_id"`
}

type AIRuntimeAuth struct {
	DelegatedUserToken  string `json:"delegated_user_token,omitempty"`
	UserToken           string `json:"user_token,omitempty"`
	ServerInternalToken string `json:"server_internal_token"`
}

type AIRuntimeEnvelope struct {
	RequestID      string         `json:"request_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	Actor          AIRuntimeActor `json:"actor"`
	Auth           AIRuntimeAuth  `json:"auth"`
	Payload        map[string]any `json:"payload"`
}

type AIRuntimeClientOptions struct {
	BaseURL       string
	InternalToken string
	Timeout       time.Duration
	HTTPClient    *http.Client
	Logger        *slog.Logger
}

type AIRuntimeClient struct {
	baseURL       string
	internalToken string
	httpClient    *http.Client
	streamClient  *http.Client
	logger        *slog.Logger
}

type AIRuntimeResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type AIRuntimeTransportError struct {
	cause error
}

func (e *AIRuntimeTransportError) Error() string {
	return "ai runtime transport failed"
}

func (e *AIRuntimeTransportError) Unwrap() error {
	return e.cause
}

func IsAIRuntimeTransportError(err error) bool {
	var transportErr *AIRuntimeTransportError
	return errors.As(err, &transportErr)
}

type AIRuntimeResponseError struct {
	cause error
}

func (e *AIRuntimeResponseError) Error() string {
	return "ai runtime response failed"
}

func (e *AIRuntimeResponseError) Unwrap() error {
	return e.cause
}

func NewAIRuntimeClient(opts AIRuntimeClientOptions) *AIRuntimeClient {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	streamClient := *client
	streamClient.Timeout = 0
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &AIRuntimeClient{
		baseURL:       strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/"),
		internalToken: strings.TrimSpace(opts.InternalToken),
		httpClient:    client,
		streamClient:  &streamClient,
		logger:        logger,
	}
}

func NewAIRuntimeClientFromConfig() *AIRuntimeClient {
	baseURL := ""
	internalToken := ""
	if config.Config != nil {
		baseURL = config.Config.GetString("ai_runtime.base_url")
		internalToken = config.Config.GetString("ai_runtime.internal_token")
		if internalToken == "" {
			internalToken = config.Config.GetString("server.internal_token")
		}
	}
	return NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL:       baseURL,
		InternalToken: internalToken,
		Timeout:       aiRuntimeTimeoutFromConfig(),
	})
}

func aiRuntimeTimeoutFromConfig() time.Duration {
	if config.Config == nil {
		return 0
	}
	timeout := config.Config.GetDuration("ai_runtime.timeout")
	if timeout <= 0 {
		return 0
	}
	return timeout
}

func (c *AIRuntimeClient) InternalToken() string {
	if c == nil {
		return ""
	}
	return c.internalToken
}

func (c *AIRuntimeClient) Forward(ctx context.Context, method, path string, envelope AIRuntimeEnvelope) (int, []byte, error) {
	response, err := c.ForwardResponse(ctx, method, path, envelope)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, response.Body, nil
}

func (c *AIRuntimeClient) ForwardResponse(ctx context.Context, method, path string, envelope AIRuntimeEnvelope) (*AIRuntimeResponse, error) {
	c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "started", 0, "")
	req, err := c.buildRequest(ctx, method, path, envelope)
	if err != nil {
		c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "failed", 0, "ai_runtime_request_error")
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "failed", 0, "ai_runtime_transport_error")
		return nil, &AIRuntimeTransportError{cause: err}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "failed", resp.StatusCode, "ai_runtime_response_error")
		return nil, &AIRuntimeResponseError{cause: readErr}
	}
	outcome := "succeeded"
	code := ""
	if resp.StatusCode >= http.StatusBadRequest {
		outcome = "upstream_error"
		code = "ai_runtime_upstream_error"
	}
	c.logRuntimeRequest(ctx, method, path, envelope.RequestID, outcome, resp.StatusCode, code)
	return &AIRuntimeResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       responseBody,
	}, nil
}

func (c *AIRuntimeClient) ForwardStream(ctx context.Context, method, path string, envelope AIRuntimeEnvelope) (*http.Response, error) {
	c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "started", 0, "")
	req, err := c.buildRequest(ctx, method, path, envelope)
	if err != nil {
		c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "failed", 0, "ai_runtime_request_error")
		return nil, err
	}
	resp, err := c.streamClient.Do(req)
	if err != nil {
		c.logRuntimeRequest(ctx, method, path, envelope.RequestID, "failed", 0, "ai_runtime_transport_error")
		return nil, &AIRuntimeTransportError{cause: err}
	}
	outcome := "connected"
	code := ""
	if resp.StatusCode >= http.StatusBadRequest {
		outcome = "upstream_error"
		code = "ai_runtime_upstream_error"
	}
	c.logRuntimeRequest(ctx, method, path, envelope.RequestID, outcome, resp.StatusCode, code)
	return resp, nil
}

func (c *AIRuntimeClient) logRuntimeRequest(ctx context.Context, method, path, requestID, outcome string, status int, code string) {
	if c == nil || c.logger == nil {
		return
	}
	attributes := []any{
		"component", "ai_runtime_client",
		"request_id", strings.TrimSpace(requestID),
		"runtime_path", normalizedAIRuntimeLogPath(path),
		"operation", strings.ToUpper(strings.TrimSpace(method)),
		"outcome", outcome,
	}
	if status > 0 {
		attributes = append(attributes, "http_status", status)
	}
	if code != "" {
		attributes = append(attributes, "code", code)
		c.logger.ErrorContext(ctx, "ai runtime request", attributes...)
		return
	}
	c.logger.InfoContext(ctx, "ai runtime request", attributes...)
}

func normalizedAIRuntimeLogPath(rawPath string) string {
	path := strings.SplitN(strings.TrimSpace(rawPath), "?", 2)[0]
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		return "/"
	}
	dynamicParents := map[string]bool{
		"agent-profiles":   true,
		"agent-resources":  true,
		"agent-workspaces": true,
		"sessions":         true,
		"actions":          true,
		"runs":             true,
		"artifacts":        true,
	}
	staticOperations := map[string]bool{
		"query": true, "current": true, "entries": true, "stream": true,
		"cancel": true, "model": true, "continue": true, "decision": true,
		"pi-approval": true, "events": true, "publish": true, "validate": true,
		"versions": true, "scan": true, "connect": true, "pause": true,
		"resume": true, "recycle": true, "audits": true,
	}
	for index := 1; index < len(segments); index++ {
		if dynamicParents[segments[index-1]] && !staticOperations[segments[index]] {
			segments[index] = ":id"
		}
	}
	return "/" + strings.Join(segments, "/")
}

func (c *AIRuntimeClient) ForwardRaw(ctx context.Context, method, path string) (int, []byte, error) {
	req, err := c.buildRawRequest(ctx, method, path)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, &AIRuntimeTransportError{cause: err}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return resp.StatusCode, nil, &AIRuntimeResponseError{cause: readErr}
	}
	return resp.StatusCode, responseBody, nil
}

func (c *AIRuntimeClient) buildRequest(ctx context.Context, method, path string, envelope AIRuntimeEnvelope) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("ai runtime client is not configured")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("ai runtime base url is not configured")
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime request failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build runtime request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID := strings.TrimSpace(envelope.RequestID); requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	return req, nil
}

func (c *AIRuntimeClient) buildRawRequest(ctx context.Context, method, path string) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("ai runtime client is not configured")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("ai runtime base url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build runtime request failed: %w", err)
	}
	if c.internalToken != "" {
		req.Header.Set("X-Internal-Token", c.internalToken)
	}
	return req, nil
}
