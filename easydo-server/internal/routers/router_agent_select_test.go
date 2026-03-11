package routers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/models"

	"github.com/gin-gonic/gin"
)

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
