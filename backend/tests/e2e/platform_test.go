//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetPlatformSettings(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Registries []interface{} `json:"registries"`
		Agents     []interface{} `json:"agents"`
	}
	h.admin.getJSON(t, "/api/v1/platform-settings", &body)
	r.NotNil(body.Registries)
	r.NotNil(body.Agents)
}

func TestGetPlatformSettingsRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/platform-settings")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestUpdatePlatformSettings(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var current map[string]any
	h.admin.getJSON(t, "/api/v1/platform-settings", &current)

	current["description"] = "e2e test run"

	var updated struct {
		Description string `json:"description"`
	}
	h.admin.putJSON(t, "/api/v1/platform-settings", current, &updated)
	r.Equal("e2e test run", updated.Description)
}

func TestSyncRegistryNotFound(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/platform-settings/registries/no-such-registry-"+randHex()+"/sync", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}
