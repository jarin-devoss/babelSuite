//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetEngineOverview(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/engine/overview")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

func TestGetEngineOverviewRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/engine/overview")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}
