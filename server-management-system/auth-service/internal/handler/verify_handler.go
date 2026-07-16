package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
)

// VerifyHandler handles internal ForwardAuth requests from Traefik.
type VerifyHandler struct {
	secret string
}

// NewVerifyHandler creates a new VerifyHandler.
func NewVerifyHandler(jwtSecret string) *VerifyHandler {
	return &VerifyHandler{
		secret: jwtSecret,
	}
}

// Verify authenticates requests from Traefik and injects identity headers.
func (h *VerifyHandler) Verify(c *gin.Context) {
	tokenString := ""

	// 1. Check Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		tokenString = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		// 2. Fallback to cookie
		cookie, err := c.Cookie("access_token")
		if err == nil && cookie != "" {
			tokenString = cookie
		}
	}

	if tokenString == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// 3. Validate token
	claims, err := sharedjwt.ValidateToken(tokenString, h.secret)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// 4. Inject identity headers for downstream services
	c.Header("X-User-Id", claims.UserID)
	c.Header("X-User-Email", claims.Email)
	c.Header("X-User-Role", claims.Role)
	c.Header("X-User-Scopes", strings.Join(claims.Scopes, ","))

	c.Status(http.StatusOK)
}
