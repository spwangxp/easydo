package middleware

import (
	"net/http"

	"easydo-server/pkg/utils"

	"github.com/gin-gonic/gin"
)

func InternalServerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expectedToken := utils.ServerInternalToken()
		providedToken := c.GetHeader(utils.InternalTokenHeader)
		if expectedToken == "" || providedToken == "" || providedToken != expectedToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"message": "unauthorized internal request",
			})
			return
		}
		c.Next()
	}
}
