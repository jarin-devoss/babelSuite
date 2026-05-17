//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterSuite(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := uniqueID("suite")
	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        id,
		"suiteStar": minimalSuiteStar(),
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	r.Equal(http.StatusCreated, resp.StatusCode)
}

func TestRegisterAndGetSuite(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := uniqueID("suite")
	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        id,
		"suiteStar": minimalSuiteStar(),
	})
	require.NoError(t, err)
	resp.Body.Close()
	r.Equal(http.StatusCreated, resp.StatusCode)

	var body struct {
		ID string `json:"id"`
	}
	h.admin.getJSON(t, "/api/v1/suites/"+id, &body)
	r.Equal(id, body.ID)
}

func TestRegisteredSuiteAppearsInList(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := uniqueID("suite")
	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        id,
		"suiteStar": minimalSuiteStar(),
	})
	require.NoError(t, err)
	resp.Body.Close()

	var list struct {
		Suites []struct {
			ID string `json:"id"`
		} `json:"suites"`
	}
	h.admin.getJSON(t, "/api/v1/suites", &list)

	for _, s := range list.Suites {
		if s.ID == id {
			return
		}
	}
	r.Failf("not found", "suite %q not found in list (%d entries)", id, len(list.Suites))
}

func TestRegisterDuplicateSuite(t *testing.T) {
	h := newHarness(t)

	id := uniqueID("suite")
	payload := map[string]string{"id": id, "suiteStar": minimalSuiteStar()}

	resp, err := h.admin.postRaw("/api/v1/suites", payload)
	require.NoError(t, err)
	resp.Body.Close()

	resp, err = h.admin.postRaw("/api/v1/suites", payload)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusConflict)
}

func TestRegisterSuiteInvalidStarlark(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        uniqueID("suite"),
		"suiteStar": "this is not valid starlark !!!",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRegisterSuiteEmptySuiteStar(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        uniqueID("suite"),
		"suiteStar": "",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetUnknownSuite(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/suites/no-such-suite-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}
