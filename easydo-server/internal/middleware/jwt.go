package middleware

import (
	"easydo-server/internal/config"
	"easydo-server/internal/models"
	"easydo-server/pkg/utils"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret []byte
	once     sync.Once
)

func getJwtSecret() []byte {
	once.Do(func() {
		jwtSecret = []byte(config.Config.GetString("jwt.secret"))
	})
	return jwtSecret
}

type Claims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func GenerateToken(user *models.User) (string, error) {
	nowTime := time.Now()
	expireTime := nowTime.Add(config.Config.GetDuration("jwt.expire"))
	
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime),
			IssuedAt:  jwt.NewNumericDate(nowTime),
			NotBefore: jwt.NewNumericDate(nowTime),
			Issuer:    "easydo",
			Subject:   user.Username,
		},
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJwtSecret())
}

func ParseToken(token string) (*Claims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
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
	
	return nil, err
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未登录",
			})
			c.Abort()
			return
		}
		
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
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
		
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		
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
