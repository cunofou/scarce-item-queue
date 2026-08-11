package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowMethods = "GET, POST, DELETE, OPTIONS"
	corsAllowHeaders = "Content-Type, X-User-ID, Idempotency-Key, X-Request-ID"
)

func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", corsAllowMethods)
			c.Header("Access-Control-Allow-Headers", corsAllowHeaders)
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			if origin == "" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			if _, ok := allowed[origin]; !ok {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
