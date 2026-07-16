package jwt

import (
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims represents custom JWT claims for the VCS-SMS system.
type Claims struct {
	UserID   string   `json:"user_id"`
	Email string   `json:"Email"`
	Role     string   `json:"role"`
	Scopes   []string `json:"scopes"`
	jwtlib.RegisteredClaims
}

// TokenConfig holds JWT configuration parameters.
type TokenConfig struct {
	Secret               string
	AccessTokenDuration  time.Duration
	RefreshTokenDuration time.Duration
}

var signToken = func(token *jwtlib.Token, secret string) (string, error) {
	return token.SignedString([]byte(secret))
}

// DefaultTokenConfig returns sensible defaults for token durations.
func DefaultTokenConfig(secret string) TokenConfig {
	return TokenConfig{
		Secret:               secret,
		AccessTokenDuration:  15 * time.Minute,
		RefreshTokenDuration: 7 * 24 * time.Hour,
	}
}

// GenerateAccessToken creates a signed JWT access token.
func GenerateAccessToken(cfg TokenConfig, userID, Email, role string, scopes []string) (string, string, error) {
	now := time.Now().UTC()
	jti := uuid.New().String()

	claims := &Claims{
		UserID:   userID,
		Email: Email,
		Role:     role,
		Scopes:   scopes,
		RegisteredClaims: jwtlib.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(cfg.AccessTokenDuration)),
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	tokenString, err := signToken(token, cfg.Secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign access token: %w", err)
	}

	return tokenString, jti, nil
}

// GenerateRefreshToken creates a signed JWT refresh token.
func GenerateRefreshToken(cfg TokenConfig, userID string) (string, string, error) {
	now := time.Now().UTC()
	jti := uuid.New().String()

	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwtlib.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(cfg.RefreshTokenDuration)),
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	tokenString, err := signToken(token, cfg.Secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return tokenString, jti, nil
}

// ValidateToken parses and validates a JWT token string, returning its claims.
func ValidateToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(tokenString, &Claims{},
		func(token *jwtlib.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwtlib.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(secret), nil
		},
		jwtlib.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// ExtractClaims parses a token without full validation (useful for extracting user info).
func ExtractClaims(tokenString string, secret string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(tokenString, &Claims{},
		func(token *jwtlib.Token) (interface{}, error) {
			return []byte(secret), nil
		},
		jwtlib.WithoutClaimsValidation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	return claims, nil
}
