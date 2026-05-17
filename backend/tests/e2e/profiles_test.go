//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func createSuiteWithProfile(t *testing.T, h *harness) (suiteID, profileID string) {
	t.Helper()
	r := require.New(t)

	suiteID = registerSuite(t, h)

	var created struct {
		Profiles []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	h.admin.mustPostJSON(t, "/api/v1/profiles/suites/"+suiteID, map[string]any{
		"name":     "staging",
		"fileName": "staging.yaml",
		"scope":    "suite",
		"yaml":     "env:\n  TARGET: staging\n",
	}, &created)
	r.NotEmpty(created.Profiles)
	return suiteID, created.Profiles[len(created.Profiles)-1].ID
}

func TestListProfileSuites(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Suites []interface{} `json:"suites"`
	}
	h.admin.getJSON(t, "/api/v1/profiles/suites", &body)
	r.NotNil(body.Suites)
}

func TestGetSuiteProfilesEmpty(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := registerSuite(t, h)

	var body struct {
		SuiteID  string        `json:"suiteId"`
		Profiles []interface{} `json:"profiles"`
	}
	h.admin.getJSON(t, "/api/v1/profiles/suites/"+id, &body)
	r.Equal(id, body.SuiteID)
	r.Empty(body.Profiles)
}

func TestCreateAndListProfile(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID, profileID := createSuiteWithProfile(t, h)

	var list struct {
		Profiles []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	h.admin.getJSON(t, "/api/v1/profiles/suites/"+suiteID, &list)

	for _, p := range list.Profiles {
		if p.ID == profileID {
			return
		}
	}
	r.Failf("not found", "profile %q not found in list", profileID)
}

func TestUpdateProfile(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID, profileID := createSuiteWithProfile(t, h)

	var result struct {
		Profiles []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"profiles"`
	}
	h.admin.putJSON(t, "/api/v1/profiles/suites/"+suiteID+"/"+profileID, map[string]any{
		"name":     "production",
		"fileName": "production.yaml",
		"scope":    "suite",
		"yaml":     "env:\n  TARGET: production\n",
	}, &result)

	for _, p := range result.Profiles {
		if p.ID == profileID {
			r.Equal("production", p.Name)
			return
		}
	}
	r.Failf("not found", "profile %q not found after update", profileID)
}

func TestDeleteProfile(t *testing.T) {
	h := newHarness(t)

	suiteID, profileID := createSuiteWithProfile(t, h)

	resp, err := h.admin.deleteRaw("/api/v1/profiles/suites/" + suiteID + "/" + profileID)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireOK(t, resp)
}

func TestSetDefaultProfile(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID, profileID := createSuiteWithProfile(t, h)

	var result struct {
		Profiles []struct {
			ID      string `json:"id"`
			Default bool   `json:"default"`
		} `json:"profiles"`
	}
	h.admin.mustPostJSON(t, "/api/v1/profiles/suites/"+suiteID+"/"+profileID+"/default", nil, &result)

	for _, p := range result.Profiles {
		if p.ID == profileID {
			r.True(p.Default)
			return
		}
	}
	r.Failf("not found", "profile %q not found after set-default", profileID)
}

func TestUpdateProfileUnknown(t *testing.T) {
	h := newHarness(t)

	suiteID := registerSuite(t, h)
	resp, err := h.admin.do(http.MethodPut,
		"/api/v1/profiles/suites/"+suiteID+"/no-such-profile",
		map[string]any{"name": "x", "fileName": "x.yaml", "scope": "suite", "yaml": ""})
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestGetProfilesUnknownSuite(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/profiles/suites/no-such-suite-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
}
