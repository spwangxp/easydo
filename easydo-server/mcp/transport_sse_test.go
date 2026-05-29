package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"
)

func newSSEHTTPTestServer(t *testing.T, opts ServerOptions) *httptest.Server {
	t.Helper()
	return httptest.NewServer(newTestHTTPServer(t, opts))
}

func openSSEConnection(t *testing.T, serverURL, authorization string, headers map[string]string) (*http.Response, *bufio.Reader, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, serverURL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("new SSE request failed: %v", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE connection failed: %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	event, data := readSSEEvent(t, reader)
	if event != "endpoint" {
		t.Fatalf("event=%q, want endpoint", event)
	}
	return resp, reader, data
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var event string
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event failed: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return event, strings.Join(dataLines, "\n")
		}
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
}

func postSSEMessage(t *testing.T, serverURL, endpointPath, authorization, payload string, headers map[string]string) *http.Response {
	t.Helper()
	url := endpointPath
	if strings.HasPrefix(endpointPath, "/") {
		url = serverURL + endpointPath
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("new SSE message request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post SSE message failed: %v", err)
	}
	return resp
}

func decodeRPCResponseBody(t *testing.T, resp *http.Response) JSONRPCResponse {
	t.Helper()
	defer resp.Body.Close()
	var decoded JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode JSON-RPC response failed: %v", err)
	}
	return decoded
}

func TestSSEConnectionRequiresAuth(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer server.Close()

	resp, err := http.Get(server.URL + "/mcp/sse")
	if err != nil {
		t.Fatalf("get SSE failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}
}

func TestSSEConnectionEmitsMessageEndpoint(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer server.Close()

	resp, _, endpoint := openSSEConnection(t, server.URL, validMCPBearer(t, 7101), nil)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type=%q, want text/event-stream", got)
	}
	if !strings.HasPrefix(endpoint, "/mcp/sse/") || !strings.HasSuffix(endpoint, "/message") {
		t.Fatalf("endpoint=%q, want /mcp/sse/:session_id/message", endpoint)
	}
}

func issueMCPBearerForSharedRedis(t *testing.T, userID uint64) string {
	t.Helper()
	user := &models.User{BaseModel: models.BaseModel{ID: userID}, Username: "mcp-user", Role: "admin"}
	token, _, err := middleware.IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}
	return "Bearer " + token
}

func TestSSEMessageEndpointEmitsJSONRPCResponseEvent(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer server.Close()
	auth := validMCPBearer(t, 7102)

	resp, reader, endpoint := openSSEConnection(t, server.URL, auth, nil)
	defer resp.Body.Close()

	messageResp := postSSEMessage(t, server.URL, endpoint, auth, `{"jsonrpc":"2.0","id":"init-1","method":"initialize"}`, nil)
	defer messageResp.Body.Close()
	if messageResp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d, want 202", messageResp.StatusCode)
	}

	event, data := readSSEEvent(t, reader)
	if event != "message" {
		t.Fatalf("event=%q, want message", event)
	}
	var decoded JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatalf("decode SSE message failed: %v; data=%s", err, data)
	}
	if decoded.ID != "init-1" || decoded.Error != nil || decoded.Result == nil {
		t.Fatalf("response=%#v, want successful initialize response", decoded)
	}
}

func TestSSEMessageRelaysAcrossServerReplicas(t *testing.T) {
	setupMCPAuthTestRedis(t)
	auth := issueMCPBearerForSharedRedis(t, 7120)
	owner := newSSEHTTPTestServer(t, ServerOptions{ServerID: "server-a", Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer owner.Close()
	peer := newSSEHTTPTestServer(t, ServerOptions{ServerID: "server-b", Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer peer.Close()

	resp, reader, endpoint := openSSEConnection(t, owner.URL, auth, nil)
	defer resp.Body.Close()

	messageResp := postSSEMessage(t, peer.URL, endpoint, auth, `{"jsonrpc":"2.0","id":"cross-replica-init","method":"initialize"}`, nil)
	defer messageResp.Body.Close()
	if messageResp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d, want 202", messageResp.StatusCode)
	}

	event, data := readSSEEvent(t, reader)
	if event != "message" {
		t.Fatalf("event=%q, want message", event)
	}
	var decoded JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatalf("decode SSE message failed: %v; data=%s", err, data)
	}
	result, ok := decoded.Result.(map[string]any)
	if decoded.ID != "cross-replica-init" || decoded.Error != nil || !ok || result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("response=%#v, want successful initialize response", decoded)
	}
}

func TestSSEMessageReturnsErrorWhenRelayOwnerUnavailable(t *testing.T) {
	setupMCPAuthTestRedis(t)
	auth := issueMCPBearerForSharedRedis(t, 7121)
	actor, err := AuthenticateRequest(context.Background(), auth)
	if err != nil {
		t.Fatalf("authenticate token failed: %v", err)
	}
	peer := newSSEHTTPTestServer(t, ServerOptions{ServerID: "server-b", Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer peer.Close()
	sessionID := "server-a.unavailable-session"
	payload, err := json.Marshal(sseSharedSession{SessionID: sessionID, ServerID: "server-a", UserID: actor.UserID, AuthSessionID: actor.SessionID})
	if err != nil {
		t.Fatalf("marshal shared session failed: %v", err)
	}
	if err := utils.RedisClient.Set(context.Background(), mcpSSESessionKey(sessionID), payload, time.Minute).Err(); err != nil {
		t.Fatalf("save shared session failed: %v", err)
	}

	messageResp := postSSEMessage(t, peer.URL, "/mcp/sse/"+sessionID+"/message", auth, `{"jsonrpc":"2.0","id":"missing-owner","method":"initialize"}`, nil)
	decoded := decodeRPCResponseBody(t, messageResp)
	if decoded.Error == nil || decoded.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("error=%#v, want not_found", decoded.Error)
	}
}

func TestSSEMessageReturnsErrorWhenRelayOwnerCannotFindSession(t *testing.T) {
	setupMCPAuthTestRedis(t)
	auth := issueMCPBearerForSharedRedis(t, 7122)
	actor, err := AuthenticateRequest(context.Background(), auth)
	if err != nil {
		t.Fatalf("authenticate token failed: %v", err)
	}
	owner := newSSEHTTPTestServer(t, ServerOptions{ServerID: "server-a", Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer owner.Close()
	peer := newSSEHTTPTestServer(t, ServerOptions{ServerID: "server-b", Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer peer.Close()
	waitForRelaySubscriber(t, "server-a")

	sessionID := "server-a.missing-local-session"
	payload, err := json.Marshal(sseSharedSession{SessionID: sessionID, ServerID: "server-a", UserID: actor.UserID, AuthSessionID: actor.SessionID})
	if err != nil {
		t.Fatalf("marshal shared session failed: %v", err)
	}
	if err := utils.RedisClient.Set(context.Background(), mcpSSESessionKey(sessionID), payload, time.Minute).Err(); err != nil {
		t.Fatalf("save shared session failed: %v", err)
	}

	messageResp := postSSEMessage(t, peer.URL, "/mcp/sse/"+sessionID+"/message", auth, `{"jsonrpc":"2.0","id":"missing-session","method":"initialize"}`, nil)
	defer messageResp.Body.Close()
	if messageResp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want JSON-RPC delivery error", messageResp.StatusCode)
	}
	var decoded JSONRPCResponse
	if err := json.NewDecoder(messageResp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode JSON-RPC response failed: %v", err)
	}
	if decoded.Error == nil || decoded.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("error=%#v, want not_found", decoded.Error)
	}
}

func waitForRelaySubscriber(t *testing.T, serverID string) {
	t.Helper()
	topic := mcpSSERelayTopic(serverID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		counts, err := utils.RedisClient.PubSubNumSub(context.Background(), topic).Result()
		if err == nil && counts[topic] > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for relay subscriber on %s", topic)
}

func TestSSEMessageEndpointRequiresSameSession(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer server.Close()
	firstAuth := validMCPBearer(t, 7103)

	resp, _, endpoint := openSSEConnection(t, server.URL, firstAuth, nil)
	defer resp.Body.Close()

	secondAuth := validMCPBearer(t, 7103)
	messageResp := postSSEMessage(t, server.URL, endpoint, secondAuth, `{"jsonrpc":"2.0","id":"wrong-session","method":"initialize"}`, nil)
	decoded := decodeRPCResponseBody(t, messageResp)
	if decoded.Error == nil || decoded.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeForbidden) {
		t.Fatalf("error=%#v, want forbidden", decoded.Error)
	}
}

func TestSSESessionExpiresAfterIdleTimeout(t *testing.T) {
	now := time.Unix(1700000000, 0)
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, Now: func() time.Time { return now }, SSEIdleTimeout: time.Second, SSEMaxSessionLifetime: time.Minute, SSECleanupInterval: time.Nanosecond})
	defer server.Close()
	auth := validMCPBearer(t, 7104)

	resp, _, endpoint := openSSEConnection(t, server.URL, auth, nil)
	defer resp.Body.Close()
	now = now.Add(2 * time.Second)

	messageResp := postSSEMessage(t, server.URL, endpoint, auth, `{"jsonrpc":"2.0","id":"expired-idle","method":"initialize"}`, nil)
	decoded := decodeRPCResponseBody(t, messageResp)
	if decoded.Error == nil || decoded.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("error=%#v, want not_found", decoded.Error)
	}
}

func TestSSESessionExpiresAfterMaxLifetime(t *testing.T) {
	now := time.Unix(1700000000, 0)
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, Now: func() time.Time { return now }, SSEIdleTimeout: time.Hour, SSEMaxSessionLifetime: time.Second, SSECleanupInterval: time.Nanosecond})
	defer server.Close()
	auth := validMCPBearer(t, 7105)

	resp, _, endpoint := openSSEConnection(t, server.URL, auth, nil)
	defer resp.Body.Close()
	now = now.Add(2 * time.Second)

	messageResp := postSSEMessage(t, server.URL, endpoint, auth, `{"jsonrpc":"2.0","id":"expired-life","method":"initialize"}`, nil)
	decoded := decodeRPCResponseBody(t, messageResp)
	if decoded.Error == nil || decoded.Error.Data.(map[string]any)["code"] != string(services.ErrorCodeNotFound) {
		t.Fatalf("error=%#v, want not_found", decoded.Error)
	}
}

func TestSSELiveConnectionClosesAfterIdleTimeout(t *testing.T) {
	now := time.Unix(1700000000, 0)
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, Now: func() time.Time { return now }, SSEIdleTimeout: 20 * time.Millisecond, SSEMaxSessionLifetime: time.Minute, SSECleanupInterval: 5 * time.Millisecond})
	defer server.Close()

	resp, reader, _ := openSSEConnection(t, server.URL, validMCPBearer(t, 7111), nil)
	defer resp.Body.Close()
	now = now.Add(time.Second)

	closed := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\n')
		closed <- err
	}()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("expected SSE stream to close after idle timeout")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for idle SSE session to close")
	}
}

func TestSSELiveConnectionClosesAfterMaxLifetime(t *testing.T) {
	now := time.Unix(1700000000, 0)
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, Now: func() time.Time { return now }, SSEIdleTimeout: time.Minute, SSEMaxSessionLifetime: 20 * time.Millisecond, SSECleanupInterval: 5 * time.Millisecond})
	defer server.Close()

	resp, reader, _ := openSSEConnection(t, server.URL, validMCPBearer(t, 7112), nil)
	defer resp.Body.Close()
	now = now.Add(time.Second)

	closed := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\n')
		closed <- err
	}()
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("expected SSE stream to close after max lifetime")
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for max lifetime SSE session to close")
	}
}

func TestSSEConnectionLimitIsPerUserAndIP(t *testing.T) {
	t.Run("per-user", func(t *testing.T) {
		server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, MaxSSEOpenConnectionsPerUser: 1, MaxSSEOpenConnectionsPerIP: 10})
		defer server.Close()
		auth := validMCPBearer(t, 7106)

		firstResp, _, _ := openSSEConnection(t, server.URL, auth, nil)
		defer firstResp.Body.Close()

		secondReq, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		secondReq.Header.Set("Authorization", auth)
		secondResp, err := http.DefaultClient.Do(secondReq)
		if err != nil {
			t.Fatalf("second connection failed: %v", err)
		}
		defer secondResp.Body.Close()
		if secondResp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status=%d, want 429", secondResp.StatusCode)
		}
	})

	t.Run("per-ip", func(t *testing.T) {
		server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, MaxSSEOpenConnectionsPerUser: 10, MaxSSEOpenConnectionsPerIP: 1})
		defer server.Close()

		firstResp, _, _ := openSSEConnection(t, server.URL, validMCPBearer(t, 7107), nil)
		defer firstResp.Body.Close()

		secondReq, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		secondReq.Header.Set("Authorization", validMCPBearer(t, 7108))
		secondResp, err := http.DefaultClient.Do(secondReq)
		if err != nil {
			t.Fatalf("second connection failed: %v", err)
		}
		defer secondResp.Body.Close()
		if secondResp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status=%d, want 429", secondResp.StatusCode)
		}
	})
}

func TestSSERejectsCrossOriginWhenAllowlistUnset(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}})
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Authorization", validMCPBearer(t, 7109))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}

func TestSSERejectsDisallowedOrigin(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, AllowedOrigins: []string{"https://allowed.example"}})
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Authorization", validMCPBearer(t, 7109))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}

func TestSSERejectsDisallowedOriginBeforeAuth(t *testing.T) {
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: &fakeAuditRecorder{}, AllowedOrigins: []string{"https://allowed.example"}})
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}

func TestSSEMessageRejectsDisallowedOriginBeforeParseAndAuth(t *testing.T) {
	audit := &fakeAuditRecorder{}
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}, MaxBodySizeBytes: 64})
	defer server.Close()

	resp := postSSEMessage(t, server.URL, "/mcp/sse/fake/message", "", `{"jsonrpc":"2.0","id":"origin-before-parse","method":"tools/list","params":{"x":"`+strings.Repeat("a", 128)+`"}}`, map[string]string{"Origin": "https://evil.example"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
	records := audit.snapshot()
	if len(records) != 1 || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want one forbidden failure", records)
	}
}

func TestSSERejectedConnectionIsAudited(t *testing.T) {
	audit := &fakeAuditRecorder{}
	server := newSSEHTTPTestServer(t, ServerOptions{Registry: NewRegistry(), AuditRecorder: audit, AllowedOrigins: []string{"https://allowed.example"}})
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Authorization", validMCPBearer(t, 7110))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	records := audit.snapshot()
	if len(records) != 1 || records[0].Status != string(AuditStatusFailure) || records[0].ErrorCode != string(services.ErrorCodeForbidden) {
		t.Fatalf("audit=%#v, want one forbidden failure", records)
	}
}
