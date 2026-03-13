package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"easydo-server/internal/config"

	"github.com/gin-gonic/gin"
)

func TestInternalServerAuth_RejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Init()
	config.Config.Set("server.internal_token", "shared-secret")

	router := gin.New()
	router.GET("/internal/test", InternalServerAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestInternalServerAuth_AllowsMatchingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Init()
	config.Config.Set("server.internal_token", "shared-secret")

	router := gin.New()
	router.GET("/internal/test", InternalServerAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/internal/test", nil)
	req.Header.Set("X-EasyDo-Internal-Token", "shared-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}
