package auth

import (
	"testing"
	"time"
)

func TestSignAndVerifyRefresh(t *testing.T) {
	t.Parallel()
	svc := NewJWT("test-secret-for-refresh")
	token, expiresAt, err := svc.SignRefresh("user-1", "ws-1", false, "password")
	if err != nil {
		t.Fatalf("SignRefresh error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty refresh token")
	}
	if expiresAt.Before(time.Now().Add(6 * 24 * time.Hour)) {
		t.Fatalf("expected refresh token to expire in ~7 days, expires at %v", expiresAt)
	}

	claims, err := svc.VerifyRefresh(token)
	if err != nil {
		t.Fatalf("VerifyRefresh error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("expected userId=user-1, got %s", claims.UserID)
	}
	if claims.WorkspaceID != "ws-1" {
		t.Fatalf("expected workspaceId=ws-1, got %s", claims.WorkspaceID)
	}
}

func TestRefreshTokenRejectedAsAccessToken(t *testing.T) {
	t.Parallel()
	svc := NewJWT("test-secret-for-refresh")
	token, _, err := svc.SignRefresh("user-1", "ws-1", false, "password")
	if err != nil {
		t.Fatalf("SignRefresh error: %v", err)
	}
	_, err = svc.Verify(token)
	if err == nil {
		t.Fatal("expected Verify to reject a refresh token, but it accepted it")
	}
}

func TestAccessTokenRejectedAsRefreshToken(t *testing.T) {
	t.Parallel()
	svc := NewJWT("test-secret-for-refresh")
	token, _, err := svc.Sign("user-1", "ws-1", false, nil, "password")
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	_, err = svc.VerifyRefresh(token)
	if err == nil {
		t.Fatal("expected VerifyRefresh to reject an access token, but it accepted it")
	}
}

func TestRefreshTokenRevocation(t *testing.T) {
	t.Parallel()
	svc := NewJWT("test-secret-for-refresh")
	token, _, err := svc.SignRefresh("user-1", "ws-1", false, "password")
	if err != nil {
		t.Fatalf("SignRefresh error: %v", err)
	}

	svc.Revoke(token)
	_, err = svc.VerifyRefresh(token)
	if err == nil {
		t.Fatal("expected VerifyRefresh to fail after revocation")
	}
}

func TestRefreshTokenHasLongerTTLThanAccess(t *testing.T) {
	t.Parallel()
	if RefreshTokenTTL <= TokenTTL {
		t.Fatalf("RefreshTokenTTL (%v) must be longer than TokenTTL (%v)", RefreshTokenTTL, TokenTTL)
	}
}
