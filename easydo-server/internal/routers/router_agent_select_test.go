package routers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"
	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

func TestInitRouter_UsesConfiguredGinMode(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	db := openRouterTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})
	if err := db.AutoMigrate(&models.MasterKey{}); err != nil {
		t.Fatalf("auto migrate master key failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("load master key failed: %v", err)
	}
	config.Init()
	config.Config.Set("server.mode", "release")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("InitRouter should not panic: %v", r)
		}
	}()

	_ = InitRouter()

	if got := gin.Mode(); got != gin.ReleaseMode {
		t.Fatalf("gin mode=%s, want %s", got, gin.ReleaseMode)
	}
}

func TestLoginRoute_IsRateLimited(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	db := openRouterTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})
	if err := db.AutoMigrate(&models.MasterKey{}); err != nil {
		t.Fatalf("auto migrate master key failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("load master key failed: %v", err)
	}

	config.Config.Set("server.mode", "test")
	router := InitRouter()

	body := []byte(`{"username":"nobody","password":"bad"}`)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSelectAgentRouteRequiresAuthentication(t *testing.T) {
	setupRouterUserCreateTestEnv(t)
	db := openRouterTestDB(t)
	originalDB := models.DB
	models.DB = db
	t.Cleanup(func() {
		models.DB = originalDB
	})
	if err := db.AutoMigrate(&models.MasterKey{}); err != nil {
		t.Fatalf("auto migrate master key failed: %v", err)
	}
	if _, err := models.LoadOrCreateMasterKey(db); err != nil {
		t.Fatalf("load master key failed: %v", err)
	}

	agent := models.Agent{
		Name:               "public-route-agent",
		Host:               "host",
		Port:               9200,
		Token:              "token",
		Status:             models.AgentStatusOnline,
		RegistrationStatus: models.AgentRegistrationStatusApproved,
		ScopeType:          models.AgentScopePlatform,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := InitRouter()
	body, err := json.Marshal(map[string]interface{}{"workspace_id": 1})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agents/select", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d body=%s", w.Code, w.Body.String())
	}
}
