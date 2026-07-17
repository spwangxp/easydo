package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/config"
)

type aiRuntimeClientRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn aiRuntimeClientRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type aiRuntimeClientErrorBody struct {
	err error
}

func (body *aiRuntimeClientErrorBody) Read([]byte) (int, error) { return 0, body.err }
func (*aiRuntimeClientErrorBody) Close() error                  { return nil }

func TestNewAIRuntimeClientFromConfigUsesConfiguredTimeout(t *testing.T) {
	config.Init()
	config.Config.Set("ai_runtime.base_url", "http://ai-runtime:8090")
	config.Config.Set("ai_runtime.internal_token", "test-token")
	config.Config.Set("ai_runtime.timeout", "3m")
	t.Cleanup(func() {
		config.Config.Set("ai_runtime.timeout", "")
	})

	client := NewAIRuntimeClientFromConfig()
	if client.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if client.httpClient.Timeout != 3*time.Minute {
		t.Fatalf("timeout=%s, want 3m", client.httpClient.Timeout)
	}
}

func TestAIRuntimeClientForwardStreamOutlivesRegularRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(60 * time.Millisecond)
		_, _ = w.Write([]byte("event: heartbeat\ndata: {}\n\n"))
	}))
	defer server.Close()

	client := NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL: server.URL,
		Timeout: 20 * time.Millisecond,
	})
	resp, err := client.ForwardStream(context.Background(), http.MethodPost, "/stream", AIRuntimeEnvelope{})
	if err != nil {
		t.Fatalf("ForwardStream() error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "event: heartbeat\ndata: {}\n\n" {
		t.Fatalf("body=%q", body)
	}
}

func TestAIRuntimeClientForwardResponsePreservesCorrelationAndMetadata(t *testing.T) {
	const requestID = "client.runtime.request"
	const responseRequestID = "runtime.response.request"
	const responseBody = `{"code":"invalid_request"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-Request-ID"); got != requestID {
			t.Errorf("X-Request-ID=%q, want %q", got, requestID)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Request-ID", responseRequestID)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()

	client := NewAIRuntimeClient(AIRuntimeClientOptions{BaseURL: server.URL})
	response, err := client.ForwardResponse(context.Background(), http.MethodPost, "/request", AIRuntimeEnvelope{RequestID: requestID})
	if err != nil {
		t.Fatalf("ForwardResponse() error = %v", err)
	}
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("Content-Type=%q", response.Header.Get("Content-Type"))
	}
	if response.Header.Get("X-Request-ID") != responseRequestID {
		t.Fatalf("X-Request-ID=%q", response.Header.Get("X-Request-ID"))
	}
	if string(response.Body) != responseBody {
		t.Fatalf("body=%q", response.Body)
	}
}

func TestAIRuntimeClientDistinguishesTransportAndResponseReadErrors(t *testing.T) {
	transportFailure := errors.New("forced transport failure")
	transportClient := NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL: "http://ai-runtime.test",
		HTTPClient: &http.Client{Transport: aiRuntimeClientRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		})},
	})
	_, err := transportClient.ForwardResponse(context.Background(), http.MethodPost, "/request", AIRuntimeEnvelope{})
	if !IsAIRuntimeTransportError(err) || !errors.Is(err, transportFailure) {
		t.Fatalf("transport error classification failed: %v", err)
	}

	readFailure := errors.New("forced read failure")
	readClient := NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL: "http://ai-runtime.test",
		HTTPClient: &http.Client{Transport: aiRuntimeClientRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       &aiRuntimeClientErrorBody{err: readFailure},
			}, nil
		})},
	})
	_, err = readClient.ForwardResponse(context.Background(), http.MethodPost, "/request", AIRuntimeEnvelope{})
	if err == nil || IsAIRuntimeTransportError(err) || !errors.Is(err, readFailure) {
		t.Fatalf("response read error classification failed: %v", err)
	}
}

func TestAIRuntimeClientLogsSanitizedRequestLifecycle(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":{"prompt":"must-not-be-logged"}}`)
	}))
	defer server.Close()

	client := NewAIRuntimeClient(AIRuntimeClientOptions{BaseURL: server.URL, Logger: logger})
	_, err := client.ForwardResponse(context.Background(), http.MethodPost, "/v1/runs/w42-secret-run/events/query", AIRuntimeEnvelope{
		RequestID: "client.runtime.lifecycle",
		Auth: AIRuntimeAuth{
			DelegatedUserToken:  "Bearer delegated-secret",
			ServerInternalToken: "internal-secret",
		},
		Payload: map[string]any{"prompt": "must-not-be-logged"},
	})
	if err != nil {
		t.Fatalf("ForwardResponse() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines=%d, want start and outcome: %s", len(lines), output.String())
	}
	var start, outcome map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil {
		t.Fatalf("decode start log: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &outcome); err != nil {
		t.Fatalf("decode outcome log: %v", err)
	}
	if start["request_id"] != "client.runtime.lifecycle" || start["runtime_path"] != "/v1/runs/:id/events/query" || start["operation"] != http.MethodPost || start["outcome"] != "started" {
		t.Fatalf("unexpected start log: %v", start)
	}
	if outcome["request_id"] != "client.runtime.lifecycle" || outcome["http_status"] != float64(http.StatusAccepted) || outcome["outcome"] != "succeeded" {
		t.Fatalf("unexpected outcome log: %v", outcome)
	}
	for _, secret := range []string{"w42-secret-run", "must-not-be-logged", "delegated-secret", "internal-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("logs leaked %q: %s", secret, output.String())
		}
	}
}

func TestAIRuntimeClientLogsStableErrorCodesWithoutRawErrors(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	transportFailure := errors.New("transport-secret-detail")
	client := NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL: "http://ai-runtime.test",
		Logger:  logger,
		HTTPClient: &http.Client{Transport: aiRuntimeClientRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportFailure
		})},
	})

	_, err := client.ForwardResponse(context.Background(), http.MethodPost, "/v1/operations/summary", AIRuntimeEnvelope{RequestID: "client.runtime.error"})
	if !IsAIRuntimeTransportError(err) {
		t.Fatalf("expected transport error, got %v", err)
	}
	if strings.Contains(output.String(), transportFailure.Error()) {
		t.Fatalf("logs leaked raw transport error: %s", output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var outcome map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &outcome); err != nil {
		t.Fatalf("decode outcome log: %v", err)
	}
	if outcome["code"] != "ai_runtime_transport_error" || outcome["outcome"] != "failed" {
		t.Fatalf("unexpected error log: %v", outcome)
	}

	output.Reset()
	readFailure := errors.New("response-secret-detail")
	responseClient := NewAIRuntimeClient(AIRuntimeClientOptions{
		BaseURL: "http://ai-runtime.test",
		Logger:  logger,
		HTTPClient: &http.Client{Transport: aiRuntimeClientRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       &aiRuntimeClientErrorBody{err: readFailure},
			}, nil
		})},
	})
	_, err = responseClient.ForwardResponse(context.Background(), http.MethodPost, "/v1/operations/summary", AIRuntimeEnvelope{RequestID: "client.runtime.response-error"})
	if err == nil || !errors.Is(err, readFailure) {
		t.Fatalf("expected response error, got %v", err)
	}
	if strings.Contains(output.String(), readFailure.Error()) {
		t.Fatalf("logs leaked raw response error: %s", output.String())
	}
	lines = strings.Split(strings.TrimSpace(output.String()), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &outcome); err != nil {
		t.Fatalf("decode response error log: %v", err)
	}
	if outcome["code"] != "ai_runtime_response_error" || outcome["http_status"] != float64(http.StatusBadGateway) || outcome["outcome"] != "failed" {
		t.Fatalf("unexpected response error log: %v", outcome)
	}
}

func TestAIRuntimeClientLogsBufferedAndStreamHTTPFailuresAtErrorLevel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "stream"}[stream], func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			client := NewAIRuntimeClient(AIRuntimeClientOptions{
				BaseURL: "http://ai-runtime.test",
				Logger:  logger,
				HTTPClient: &http.Client{Transport: aiRuntimeClientRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"message":"must-not-be-logged"}`)),
					}, nil
				})},
			})
			if stream {
				response, err := client.ForwardStream(context.Background(), http.MethodPost, "/v1/operations/summary", AIRuntimeEnvelope{RequestID: "runtime-upstream-error"})
				if err != nil {
					t.Fatalf("ForwardStream() error = %v", err)
				}
				_ = response.Body.Close()
			} else if _, err := client.ForwardResponse(context.Background(), http.MethodPost, "/v1/operations/summary", AIRuntimeEnvelope{RequestID: "runtime-upstream-error"}); err != nil {
				t.Fatalf("ForwardResponse() error = %v", err)
			}

			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			var outcome map[string]any
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &outcome); err != nil {
				t.Fatalf("decode outcome log: %v", err)
			}
			if outcome["level"] != "ERROR" || outcome["outcome"] != "upstream_error" || outcome["code"] != "ai_runtime_upstream_error" || outcome["http_status"] != float64(http.StatusTooManyRequests) {
				t.Fatalf("unexpected upstream error log: %v", outcome)
			}
			if strings.Contains(output.String(), "must-not-be-logged") {
				t.Fatalf("upstream response body leaked: %s", output.String())
			}
		})
	}
}
