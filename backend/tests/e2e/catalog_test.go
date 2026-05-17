//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListPackages(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		Packages []interface{} `json:"packages"`
	}
	h.admin.getJSON(t, "/api/v1/catalog/packages", &body)
	r.NotNil(body.Packages)
}

func TestListPackagesRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/catalog/packages")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestGetPackageNotFound(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/catalog/packages/no-such-package-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestListFavorites(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	var body struct {
		PackageIDs []string `json:"packageIds"`
	}
	h.admin.getJSON(t, "/api/v1/catalog/favorites", &body)
	r.NotNil(body.PackageIDs)
}

func TestListFavoritesRequiresAuth(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.noAuth().getRaw("/api/v1/catalog/favorites")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusUnauthorized)
}

func TestAddFavoriteUnknownPackage(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/catalog/favorites/no-such-pkg-"+randHex(), nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestRemoveFavoriteIdempotent(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.deleteRaw("/api/v1/catalog/favorites/any-package-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 500, "expected non-5xx for remove favorite")
}
