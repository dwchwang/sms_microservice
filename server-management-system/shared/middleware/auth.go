package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apperrors "github.com/vcs-sms/shared/errors"
	"github.com/vcs-sms/shared/response"
)

// AuthFromForwardAuth extracts user info from Traefik ForwardAuth headers.
// Traefik đã verify JWT qua Identity Service, service chỉ cần đọc header.
func AuthFromForwardAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetHeader("X-User-Id")
		scopesStr := c.GetHeader("X-User-Scopes")

		if userID == "" {
			response.ErrorWithCode(c, http.StatusUnauthorized,
				apperrors.CodeUnauthorized, "Missing authentication")
			c.Abort()
			return
		}

		// Parse scopes
		var scopes []string
		if scopesStr != "" {
			scopes = strings.Split(scopesStr, ",")
		}

		// Set vào context cho handler dùng
		c.Set("user_id", userID)
		c.Set("user_scopes", scopes)
		c.Next()
	}
}

// RequireScope checks if the authenticated user has a required scope.
func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scopesVal, exists := c.Get("user_scopes")
		if !exists {
			response.ErrorWithCode(c, http.StatusForbidden,
				apperrors.CodeForbiddenScope, "No scopes available")
			c.Abort()
			return
		}

		scopes, ok := scopesVal.([]string)
		if !ok {
			response.ErrorWithCode(c, http.StatusForbidden,
				apperrors.CodeForbiddenScope, "Invalid scopes format")
			c.Abort()
			return
		}

		for _, s := range scopes {
			if strings.TrimSpace(s) == scope {
				c.Next()
				return
			}
		}

		response.ErrorWithCode(c, http.StatusForbidden,
			apperrors.CodeForbiddenScope,
			"Insufficient permissions: required scope '"+scope+"'")
		c.Abort()
	}
}
