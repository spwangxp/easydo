package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient wraps the standard HTTP client with retry logic
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// NewHTTPClient creates a new HTTP client
func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Request represents an HTTP request
type Request struct {
	Method  string
	Path    string
	Body    interface{}
	Headers map[string]string
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Body       map[string]interface{}
	RawBody    []byte
}

// Do performs an HTTP request with optional retry
func (c *HTTPClient) Do(ctx context.Context, req *Request, retryCount int, retryInterval time.Duration) (*Response, error) {
	var lastErr error

	for attempt := 0; attempt <= retryCount; attempt++ {
		if attempt > 0 {
			time.Sleep(retryInterval)
		}

		resp, err := c.doSingle(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}

		// Don't retry on client errors (4xx) except 429 (Too Many Requests)
		if resp.StatusCode >= 400 && resp.StatusCode != 429 {
			return resp, nil
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts: %v", retryCount+1, lastErr)
}

// doSingle performs a single HTTP request
func (c *HTTPClient) doSingle(ctx context.Context, req *Request) (*Response, error) {
	var bodyReader io.Reader
	if req.Body != nil {
		bodyData, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyData)
	}

	url := c.baseURL + req.Path
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default content type
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Set custom headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var bodyMap map[string]interface{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &bodyMap); err != nil {
			// If unmarshal fails, just return raw body
			bodyMap = map[string]interface{}{
				"raw": string(body),
			}
		}
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       bodyMap,
		RawBody:    body,
	}, nil
}

// Get performs a GET request
func (c *HTTPClient) Get(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, &Request{
		Method: http.MethodGet,
		Path:   path,
	}, 0, 0)
}

// Post performs a POST request
func (c *HTTPClient) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, &Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	}, 0, 0)
}
