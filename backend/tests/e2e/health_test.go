//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveness(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Status string `json:"status"`
	}
	h.admin.getJSON(t, "/healthz", &body)
	r.Equal("ok", body.Status)
}

func TestReadiness(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Status     string `json:"status"`
		Components []struct {
			Name     string `json:"name"`
			Status   string `json:"status"`
			Required bool   `json:"required"`
			Ready    bool   `json:"ready"`
		} `json:"components"`
	}
	h.admin.getJSON(t, "/readyz", &body)
	r.Equal("ready", body.Status)

	required := map[string]bool{"database": false, "platform": false, "profiles": false}
	for _, c := range body.Components {
		if _, ok := required[c.Name]; !ok {
			continue
		}
		r.Truef(c.Ready, "required component %q is not ready (status=%s)", c.Name, c.Status)
		required[c.Name] = true
	}
	for name, seen := range required {
		r.Truef(seen, "required component %q missing from /readyz response", name)
	}
}

func TestReadinessSubsystemDatabase(t *testing.T) {
	h := newHarness(t)
	resp, err := h.admin.getRaw("/readyz/database")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

func TestReadinessUnknownSubsystem(t *testing.T) {
	h := newHarness(t)
	resp, err := h.admin.getRaw("/readyz/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}
