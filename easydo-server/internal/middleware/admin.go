package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminRequired 检查当前用户是否为管理员
func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "未登录",
			})
			c.Abort()
			return
		}

		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "权限不足，需要管理员权限",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetCurrentUserID 获取当前用户ID
func GetCurrentUserID(c *gin.Context) uint64 {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	return userID.(uint64)
}

// GetCurrentUserRole 获取当前用户角色
func GetCurrentUserRole(c *gin.Context) string {
	role, _ := c.Get("role")
	if role == nil {
		return "user"
	}
	return role.(string)
}
