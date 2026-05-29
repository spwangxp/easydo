package routers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

func TestRouterRegistersMCPRoutes(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	config.Init()
	gin.SetMode(gin.TestMode)
	originalDB := models.DB
	models.DB = openRouterTestDB(t)
	t.Cleanup(func() { models.DB = originalDB })
	router := InitRouter()

	for _, route := range router.Routes() {
		if route.Method == http.MethodPost && route.Path == "/mcp" {
			return
		}
	}

	t.Fatalf("POST /mcp route was not registered; routes=%#v", router.Routes())
}

func TestRouterRegistersMCPSSERoutes(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	config.Init()
	gin.SetMode(gin.TestMode)
	originalDB := models.DB
	models.DB = openRouterTestDB(t)
	t.Cleanup(func() { models.DB = originalDB })
	router := InitRouter()

	foundConnect := false
	foundMessage := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/mcp/sse" {
			foundConnect = true
		}
		if route.Method == http.MethodPost && route.Path == "/mcp/sse/:session_id/message" {
			foundMessage = true
		}
	}
	if !foundConnect || !foundMessage {
		t.Fatalf("SSE routes missing: connect=%v message=%v routes=%#v", foundConnect, foundMessage, router.Routes())
	}
}

func TestRouterPassesMCPAllowedOrigins(t *testing.T) {
	t.Setenv("MCP_ALLOWED_ORIGINS", "https://allowed.example")
	setupRouterUserCreateTestEnv(t)
	gin.SetMode(gin.TestMode)
	originalDB := models.DB
	models.DB = openRouterTestDB(t)
	t.Cleanup(func() { models.DB = originalDB })
	router := InitRouter()
	auth := "Bearer " + issueRouterTestToken(t, &models.User{BaseModel: models.BaseModel{ID: 7301}, Username: "router-mcp-user", Role: "admin"})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"origin","method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body=%q, want empty transport-level response", w.Body.String())
	}
}
