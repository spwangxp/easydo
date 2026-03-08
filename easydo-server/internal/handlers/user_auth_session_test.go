package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

func setupUserAuthTestEnv(t *testing.T, tokenTTL, refreshInterval time.Duration) *miniredis.Miniredis {
	t.Helper()

	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AUTH_TOKEN_TTL", tokenTTL.String())
	t.Setenv("AUTH_REFRESH_INTERVAL", refreshInterval.String())
	config.Init()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		_ = utils.RedisClient.Close()
		mini.Close()
	})
	return mini
}

func TestLoginRefreshLogoutFlowWithStableToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupUserAuthTestEnv(t, 4*time.Hour, 10*time.Minute)

	db := openHandlerTestDB(t)
	user := models.User{
		Username: "login-refresh-user",
		Role:     "admin",
		Status:   "active",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	h := &UserHandler{DB: db}
	router := gin.New()
	auth := router.Group("/api/auth")
	auth.POST("/login", h.Login)
	auth.POST("/refresh", middleware.JWTAuth(), h.RefreshToken)
	auth.POST("/logout", middleware.JWTAuth(), h.Logout)
	auth.GET("/userinfo", middleware.JWTAuth(), h.GetUserInfo)

	loginBody := map[string]string{
		"username": user.Username,
		"password": "1qaz2WSX",
	}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginW.Code, loginW.Body.String())
	}

	var loginResp struct {
		Code int `json:"code"`
		Data struct {
			Token           string `json:"token"`
			ExpiresAt       int64  `json:"expires_at"`
			ExpiresIn       int64  `json:"expires_in"`
			RefreshInterval int64  `json:"refresh_interval"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	if loginResp.Code != 200 || loginResp.Data.Token == "" {
		t.Fatalf("unexpected login response: %s", loginW.Body.String())
	}
	if loginResp.Data.ExpiresIn != int64((4 * time.Hour).Seconds()) {
		t.Fatalf("unexpected expires_in: %d", loginResp.Data.ExpiresIn)
	}
	if loginResp.Data.RefreshInterval != int64((10 * time.Minute).Seconds()) {
		t.Fatalf("unexpected refresh_interval: %d", loginResp.Data.RefreshInterval)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	refreshW := httptest.NewRecorder()
	router.ServeHTTP(refreshW, refreshReq)
	if refreshW.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshW.Code, refreshW.Body.String())
	}

	var refreshResp struct {
		Code int `json:"code"`
		Data struct {
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(refreshW.Body.Bytes(), &refreshResp); err != nil {
		t.Fatalf("parse refresh response failed: %v", err)
	}
	if refreshResp.Code != 200 {
		t.Fatalf("unexpected refresh code: %d body=%s", refreshResp.Code, refreshW.Body.String())
	}
	if refreshResp.Data.Token != loginResp.Data.Token {
		t.Fatalf("refresh should keep token unchanged")
	}
	if refreshResp.Data.ExpiresAt < loginResp.Data.ExpiresAt {
		t.Fatalf("refresh should not reduce expires_at: login=%d refresh=%d", loginResp.Data.ExpiresAt, refreshResp.Data.ExpiresAt)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	if logoutW.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logoutW.Code, logoutW.Body.String())
	}

	userInfoReq := httptest.NewRequest(http.MethodGet, "/api/auth/userinfo", nil)
	userInfoReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	userInfoW := httptest.NewRecorder()
	router.ServeHTTP(userInfoW, userInfoReq)
	if userInfoW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized after logout, got %d body=%s", userInfoW.Code, userInfoW.Body.String())
	}
}

func TestRefreshReturns401WhenSessionMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mini := setupUserAuthTestEnv(t, 4*time.Hour, 10*time.Minute)

	db := openHandlerTestDB(t)
	user := models.User{
		Username: "refresh-session-missing",
		Role:     "admin",
		Status:   "active",
	}
	if err := user.SetPassword("1qaz2WSX"); err != nil {
		t.Fatalf("set password failed: %v", err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	h := &UserHandler{DB: db}
	router := gin.New()
	auth := router.Group("/api/auth")
	auth.POST("/login", h.Login)
	auth.POST("/refresh", middleware.JWTAuth(), h.RefreshToken)

	loginBody := map[string]string{
		"username": user.Username,
		"password": "1qaz2WSX",
	}
	loginBytes, _ := json.Marshal(loginBody)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginW.Code, loginW.Body.String())
	}

	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}
	if loginResp.Data.Token == "" {
		t.Fatalf("empty token in login response: %s", loginW.Body.String())
	}

	mini.FlushAll()

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	refreshW := httptest.NewRecorder()
	router.ServeHTTP(refreshW, refreshReq)
	if refreshW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when session missing, got %d body=%s", refreshW.Code, refreshW.Body.String())
	}
}

func TestProtectedRouteRejectsLegacyTokenWithoutSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupUserAuthTestEnv(t, 4*time.Hour, 10*time.Minute)

	db := openHandlerTestDB(t)
	h := &UserHandler{DB: db}
	router := gin.New()
	auth := router.Group("/api/auth")
	auth.GET("/userinfo", middleware.JWTAuth(), h.GetUserInfo)

	now := time.Now()
	legacyToken := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{
		UserID:   12345,
		Username: "legacy-user",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "easydo",
			Subject:   "legacy-user",
		},
	})
	tokenValue, err := legacyToken.SignedString([]byte(config.Config.GetString("jwt.secret")))
	if err != nil {
		t.Fatalf("sign legacy token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected legacy token rejected with 401, got %d body=%s", w.Code, w.Body.String())
	}
}
