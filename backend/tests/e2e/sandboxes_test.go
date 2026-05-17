//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListSandboxes(t *testing.T) {
	h := newHarness(t)

	var body struct {
		DockerAvailable bool `json:"dockerAvailable"`
	}
	h.admin.getJSON(t, "/api/v1/sandboxes", &body)
}

func TestListSandboxesRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/sandboxes")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestReapAllSandboxes(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/sandboxes/reap-all", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	// 200 when Docker available; 503 when not — both are valid
	require.True(t,
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable,
		"expected 200 or 503, got %d", resp.StatusCode)
}

func TestReapAllRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().postRaw("/api/v1/sandboxes/reap-all", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestReapUnknownSandbox(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/sandboxes/no-such-sandbox-"+randHex()+"/reap", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
}
