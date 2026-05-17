//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignIn(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Token string `json:"token"`
	}
	h.admin.mustPostJSON(t, "/api/v1/auth/sign-in", map[string]string{
		"email":    testAdminEmail,
		"password": testAdminPassword,
	}, &body)
	r.NotEmpty(body.Token)
}

func TestSignInWrongPassword(t *testing.T) {
	h := newHarness(t)

	err := h.admin.noAuth().postJSON(t, "/api/v1/auth/sign-in", map[string]string{
		"email":    testAdminEmail,
		"password": "definitely-wrong",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, httpStatusCode(err))
}

func TestSignInUnknownEmail(t *testing.T) {
	h := newHarness(t)

	err := h.admin.noAuth().postJSON(t, "/api/v1/auth/sign-in", map[string]string{
		"email":    "nobody@e2e.local",
		"password": testAdminPassword,
	}, nil)
	require.Equal(t, http.StatusUnauthorized, httpStatusCode(err))
}

func TestGetMe(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	h.admin.getJSON(t, "/api/v1/auth/me", &body)
	r.Equal(testAdminEmail, body.User.Email)
}

func TestGetMeRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/auth/me")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestProtectedEndpointNoToken(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/suites")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestProtectedEndpointInvalidToken(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.withToken("not.a.valid.jwt").getRaw("/api/v1/suites")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestSignInRateLimitReturns429(t *testing.T) {
	h := newHarness(t)

	// The sign-in rate limiter allows 10 requests per minute per IP.
	// Send 11 sequential requests with wrong credentials — the 11th must be 429.
	got429 := false
	for i := 0; i < 11; i++ {
		resp, err := h.admin.noAuth().postRaw("/api/v1/auth/sign-in", map[string]string{
			"email":    "rate-limit-probe@e2e.local",
			"password": "wrong",
		})
		require.NoError(t, err)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	require.True(t, got429, "expected HTTP 429 after exceeding the sign-in rate limit")
}
