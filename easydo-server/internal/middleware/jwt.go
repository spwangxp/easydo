package middleware

import (
	"context"
	"crypto/rand"
	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

var (
	jwtSecret             []byte
	once                  sync.Once
	validatedSessionCache sync.Map
)

const (
	defaultAuthTokenTTL        = 4 * time.Hour
	defaultAuthRefreshInterval = 10 * time.Minute
	authSessionPrefix          = "auth:sess:"
	validatedSessionCacheTTL   = 30 * time.Second
)

type cachedValidatedSession struct {
	UserID    uint64
	ExpiresAt time.Time
}

func getJwtSecret() []byte {
	once.Do(func() {
		jwtSecret = []byte(config.Config.GetString("jwt.secret"))
	})
	return jwtSecret
}

type Claims struct {
	UserID    uint64 `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

type userSession struct {
	UserID        uint64 `json:"user_id"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	IssuedAt      int64  `json:"issued_at"`
	LastRefreshAt int64  `json:"last_refresh_at"`
}

func GetAuthTokenTTL() time.Duration {
	ttl := config.Config.GetDuration("auth.token_ttl")
	if ttl <= 0 {
		return defaultAuthTokenTTL
	}
	return ttl
}

func GetAuthRefreshInterval() time.Duration {
	interval := config.Config.GetDuration("auth.refresh_interval")
	if interval <= 0 {
		return defaultAuthRefreshInterval
	}
	return interval
}

func getAuthSessionKey(sessionID string) string {
	return authSessionPrefix + sessionID
}

func generateSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func saveUserSession(ctx context.Context, sessionID string, session userSession) error {
	if utils.RedisClient == nil {
		return errors.New("redis client is not initialized")
	}

	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return utils.RedisClient.Set(ctx, getAuthSessionKey(sessionID), payload, GetAuthTokenTTL()).Err()
}

func loadUserSession(ctx context.Context, sessionID string) (*userSession, error) {
	if utils.RedisClient == nil {
		return nil, errors.New("redis client is not initialized")
	}

	raw, err := utils.RedisClient.Get(ctx, getAuthSessionKey(sessionID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var session userSession
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func deleteUserSession(ctx context.Context, sessionID string) error {
	if utils.RedisClient == nil {
		return errors.New("redis client is not initialized")
	}
	return utils.RedisClient.Del(ctx, getAuthSessionKey(sessionID)).Err()
}

func GenerateToken(user *models.User) (string, error) {
	token, _, err := IssueTokenSession(context.Background(), user)
	return token, err
}

func IssueTokenSession(ctx context.Context, user *models.User) (string, int64, error) {
	if user == nil {
		return "", 0, errors.New("user is nil")
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return "", 0, err
	}

	nowTime := time.Now()
	claims := Claims{
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(nowTime),
			NotBefore: jwt.NewNumericDate(nowTime),
			Issuer:    "easydo",
			Subject:   user.Username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(getJwtSecret())
	if err != nil {
		return "", 0, err
	}

	session := userSession{
		UserID:        user.ID,
		Username:      user.Username,
		Role:          user.Role,
		IssuedAt:      nowTime.Unix(),
		LastRefreshAt: nowTime.Unix(),
	}
	if err := saveUserSession(ctx, sessionID, session); err != nil {
		return "", 0, err
	}

	expiresAt := nowTime.Add(GetAuthTokenTTL()).Unix()
	return tokenStr, expiresAt, nil
}

func ParseToken(token string) (*Claims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return getJwtSecret(), nil
	})

	if err != nil {
		return nil, err
	}

	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*Claims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}

	return nil, errors.New("invalid token")
}

func ValidateTokenSession(ctx context.Context, claims *Claims) error {
	if claims == nil || claims.SessionID == "" {
		return errors.New("invalid session")
	}
	if cached, ok := getCachedValidatedSession(claims.SessionID); ok {
		if cached.UserID == claims.UserID {
			return nil
		}
		validatedSessionCache.Delete(claims.SessionID)
	}

	session, err := loadUserSession(ctx, claims.SessionID)
	if err != nil {
		if cached, ok := getCachedValidatedSession(claims.SessionID); ok && cached.UserID == claims.UserID {
			return nil
		}
		return err
	}
	if session == nil {
		return errors.New("session expired")
	}
	if session.UserID != claims.UserID {
		return errors.New("session mismatch")
	}
	cacheValidatedSession(claims.SessionID, claims.UserID)
	return nil
}

func cacheValidatedSession(sessionID string, userID uint64) {
	validatedSessionCache.Store(sessionID, cachedValidatedSession{
		UserID:    userID,
		ExpiresAt: time.Now().Add(validatedSessionCacheTTL),
	})
}

func getCachedValidatedSession(sessionID string) (cachedValidatedSession, bool) {
	if sessionID == "" {
		return cachedValidatedSession{}, false
	}
	raw, ok := validatedSessionCache.Load(sessionID)
	if !ok {
		return cachedValidatedSession{}, false
	}
	cached, ok := raw.(cachedValidatedSession)
	if !ok {
		validatedSessionCache.Delete(sessionID)
		return cachedValidatedSession{}, false
	}
	if time.Now().After(cached.ExpiresAt) {
		validatedSessionCache.Delete(sessionID)
		return cachedValidatedSession{}, false
	}
	return cached, true
}

func RefreshTokenSession(ctx context.Context, token string) (int64, error) {
	claims, err := ParseToken(token)
	if err != nil {
		return 0, err
	}
	if err := ValidateTokenSession(ctx, claims); err != nil {
		return 0, err
	}

	session, err := loadUserSession(ctx, claims.SessionID)
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, errors.New("session expired")
	}

	now := time.Now()
	session.LastRefreshAt = now.Unix()
	if err := saveUserSession(ctx, claims.SessionID, *session); err != nil {
		return 0, err
	}

	return now.Add(GetAuthTokenTTL()).Unix(), nil
}

func RevokeTokenSession(ctx context.Context, token string) error {
	claims, err := ParseToken(token)
	if err != nil {
		return err
	}
	if claims.SessionID == "" {
		return errors.New("invalid session")
	}
	validatedSessionCache.Delete(claims.SessionID)
	return deleteUserSession(ctx, claims.SessionID)
}

func RevokeSessionByID(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("invalid session")
	}
	validatedSessionCache.Delete(sessionID)
	return deleteUserSession(ctx, sessionID)
}

func ExtractBearerToken(header string) (string, error) {
	token := strings.TrimSpace(header)
	if token == "" {
		return "", errors.New("missing authorization token")
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return "", errors.New("missing authorization token")
	}
	return token, nil
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := ExtractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未登录",
			})
			c.Abort()
			return
		}

		claims, err := ParseToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "登录已过期",
			})
			c.Abort()
			return
		}

		if err := ValidateTokenSession(c.Request.Context(), claims); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "登录已过期",
			})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Set("session_id", claims.SessionID)
		c.Set("auth_token", token)

		c.Next()
	}
}

func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := "rate_limit:" + ip

		count, _ := utils.RedisClient.Incr(c.Request.Context(), key).Result()
		if count == 1 {
			utils.RedisClient.Expire(c.Request.Context(), key, time.Minute)
		}

		if count > 100 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "请求过于频繁",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
