package jwt

import (
	"fmt"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

func TestGenerateAccessToken_Success(t *testing.T) {
	cfg := TokenConfig{
		Secret:               "test-secret-key-for-jwt",
		AccessTokenDuration:  15 * time.Minute,
		RefreshTokenDuration: 7 * 24 * time.Hour,
	}

	token, jti, err := GenerateAccessToken(cfg, "user-123", "testuser", "admin", []string{"server:read", "server:create"})
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if jti == "" {
		t.Fatal("expected non-empty JTI")
	}

	// Validate the generated token
	claims, err := ValidateToken(token, cfg.Secret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected UserID 'user-123', got '%s'", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("expected Username 'testuser', got '%s'", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected Role 'admin', got '%s'", claims.Role)
	}
	if len(claims.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(claims.Scopes))
	}
}

func TestGenerateRefreshToken_Success(t *testing.T) {
	cfg := TokenConfig{
		Secret:               "test-secret",
		AccessTokenDuration:  15 * time.Minute,
		RefreshTokenDuration: 7 * 24 * time.Hour,
	}

	token, jti, err := GenerateRefreshToken(cfg, "user-456")
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if jti == "" {
		t.Fatal("expected non-empty JTI")
	}

	claims, err := ValidateToken(token, cfg.Secret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != "user-456" {
		t.Errorf("expected UserID 'user-456', got '%s'", claims.UserID)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	cfg := TokenConfig{Secret: "real-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}

	token, _, err := GenerateAccessToken(cfg, "user-1", "test", "admin", nil)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	// Try to validate with wrong secret
	_, err = ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: -1 * time.Hour, RefreshTokenDuration: 7 * 24 * time.Hour}

	token, _, err := GenerateAccessToken(cfg, "user-1", "test", "admin", nil)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	_, err = ValidateToken(token, cfg.Secret)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	_, err := ValidateToken("", "secret")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestValidateToken_MalformedToken(t *testing.T) {
	_, err := ValidateToken("this.is.not.a.jwt", "secret")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestExtractClaims_Success(t *testing.T) {
	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}

	token, _, err := GenerateAccessToken(cfg, "user-789", "alice", "operator", []string{"server:read"})
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}

	claims, err := ExtractClaims(token, cfg.Secret)
	if err != nil {
		t.Fatalf("ExtractClaims failed: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("expected Username 'alice', got '%s'", claims.Username)
	}
}

func TestDefaultTokenConfig(t *testing.T) {
	cfg := DefaultTokenConfig("my-secret")
	if cfg.Secret != "my-secret" {
		t.Errorf("expected secret 'my-secret', got '%s'", cfg.Secret)
	}
	if cfg.AccessTokenDuration != 15*time.Minute {
		t.Errorf("expected 15min access token duration")
	}
	if cfg.RefreshTokenDuration != 7*24*time.Hour {
		t.Errorf("expected 7-day refresh token duration")
	}
}

func TestGenerateAccessToken_NilScopes(t *testing.T) {
	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	token, jti, err := GenerateAccessToken(cfg, "user-1", "test", "viewer", nil)
	if err != nil {
		t.Fatalf("GenerateAccessToken with nil scopes failed: %v", err)
	}
	if token == "" || jti == "" {
		t.Fatal("expected non-empty token and jti")
	}
}

func TestGenerateAccessToken_SignError(t *testing.T) {
	original := signToken
	signToken = func(token *jwtlib.Token, secret string) (string, error) {
		return "", fmt.Errorf("sign failed")
	}
	t.Cleanup(func() { signToken = original })

	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	_, _, err := GenerateAccessToken(cfg, "user-1", "test", "viewer", nil)
	if err == nil {
		t.Fatal("expected sign error")
	}
}

func TestGenerateRefreshToken_SignError(t *testing.T) {
	original := signToken
	signToken = func(token *jwtlib.Token, secret string) (string, error) {
		return "", fmt.Errorf("sign failed")
	}
	t.Cleanup(func() { signToken = original })

	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	_, _, err := GenerateRefreshToken(cfg, "user-1")
	if err == nil {
		t.Fatal("expected sign error")
	}
}

func TestGenerateRefreshToken_VerifyClaims(t *testing.T) {
	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	token, jti, err := GenerateRefreshToken(cfg, "user-999")
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}
	if token == "" || jti == "" {
		t.Fatal("expected non-empty token and jti")
	}
	claims, _ := ValidateToken(token, cfg.Secret)
	if claims.UserID != "user-999" {
		t.Errorf("expected UserID 'user-999', got '%s'", claims.UserID)
	}
}

func TestExtractClaims_MalformedToken(t *testing.T) {
	_, err := ExtractClaims("not.a.jwt", "secret")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestExtractClaims_EmptyToken(t *testing.T) {
	_, err := ExtractClaims("", "secret")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestValidateToken_NoneAlgorithm(t *testing.T) {
	token := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIxMjM0NTY3ODkwIn0."
	_, err := ValidateToken(token, "secret")
	if err == nil {
		t.Fatal("expected error for none algorithm")
	}
}

func TestExtractClaims_ExpiredToken(t *testing.T) {
	cfg := TokenConfig{Secret: "test-secret", AccessTokenDuration: -1 * time.Hour, RefreshTokenDuration: 7 * 24 * time.Hour}
	token, _, _ := GenerateAccessToken(cfg, "user-1", "expired", "admin", nil)
	claims, err := ExtractClaims(token, cfg.Secret)
	if err != nil {
		t.Fatalf("ExtractClaims failed: %v", err)
	}
	if claims.Username != "expired" {
		t.Errorf("expected 'expired', got '%s'", claims.Username)
	}
}
