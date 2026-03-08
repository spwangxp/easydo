package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

func setupAuthTestRedis(t *testing.T, tokenTTL, refreshInterval time.Duration) *miniredis.Miniredis {
	t.Helper()

	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("AUTH_TOKEN_TTL", tokenTTL.String())
	t.Setenv("AUTH_REFRESH_INTERVAL", refreshInterval.String())
	config.Init()

	once = sync.Once{}
	jwtSecret = nil

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis failed: %v", err)
	}
	utils.RedisClient = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		_ = utils.RedisClient.Close()
		mini.Close()
		_ = os.Unsetenv("JWT_SECRET")
		_ = os.Unsetenv("AUTH_TOKEN_TTL")
		_ = os.Unsetenv("AUTH_REFRESH_INTERVAL")
	})
	return mini
}

func TestJWTAuthRejectsLegacyTokenWithoutSessionID(t *testing.T) {
	setupAuthTestRedis(t, 4*time.Hour, 10*time.Minute)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/protected", JWTAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	now := time.Now()
	legacyToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:   1001,
		Username: "legacy",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "easydo",
			Subject:   "legacy",
		},
	})
	tokenValue, err := legacyToken.SignedString(getJwtSecret())
	if err != nil {
		t.Fatalf("sign token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestJWTAuthRejectsMissingRedisSession(t *testing.T) {
	mini := setupAuthTestRedis(t, 4*time.Hour, 10*time.Minute)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/protected", JWTAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	user := &models.User{
		BaseModel: models.BaseModel{ID: 2001},
		Username:  "alice",
		Role:      "admin",
	}
	tokenValue, _, err := IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}

	mini.FlushAll()

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRefreshTokenSessionExtendsTTLWithoutChangingToken(t *testing.T) {
	mini := setupAuthTestRedis(t, 2*time.Hour, 10*time.Minute)

	user := &models.User{
		BaseModel: models.BaseModel{ID: 3001},
		Username:  "bob",
		Role:      "user",
	}
	tokenValue, _, err := IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}

	claims, err := ParseToken(tokenValue)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	key := getAuthSessionKey(claims.SessionID)

	mini.FastForward(90 * time.Minute)
	ttlBefore := mini.TTL(key)
	if ttlBefore <= 0 {
		t.Fatalf("expected ttl before refresh > 0, got %v", ttlBefore)
	}

	expiresAt, err := RefreshTokenSession(context.Background(), tokenValue)
	if err != nil {
		t.Fatalf("refresh token failed: %v", err)
	}
	if expiresAt <= time.Now().Unix() {
		t.Fatalf("expected expiresAt in future, got %d", expiresAt)
	}

	ttlAfter := mini.TTL(key)
	if ttlAfter <= ttlBefore {
		t.Fatalf("expected ttl after refresh (%v) > ttl before refresh (%v)", ttlAfter, ttlBefore)
	}
}

func TestRevokeTokenSessionRemovesRedisSession(t *testing.T) {
	mini := setupAuthTestRedis(t, 4*time.Hour, 10*time.Minute)

	user := &models.User{
		BaseModel: models.BaseModel{ID: 4001},
		Username:  "charlie",
		Role:      "user",
	}
	tokenValue, _, err := IssueTokenSession(context.Background(), user)
	if err != nil {
		t.Fatalf("issue token failed: %v", err)
	}

	claims, err := ParseToken(tokenValue)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	key := getAuthSessionKey(claims.SessionID)
	if !mini.Exists(key) {
		t.Fatalf("expected session key %s to exist", key)
	}

	if err := RevokeTokenSession(context.Background(), tokenValue); err != nil {
		t.Fatalf("revoke token session failed: %v", err)
	}
	if mini.Exists(key) {
		t.Fatalf("expected session key %s removed", key)
	}
}
