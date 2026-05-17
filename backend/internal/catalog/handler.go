package catalog

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/domain"
	"github.com/babelsuite/babelsuite/internal/httpserver"
)

type Handler struct {
	service   Reader
	favorites favoriteStore
	jwt       *auth.JWTService
}

type favoriteStore interface {
	ListFavoritePackageIDs(ctx context.Context, userID string) ([]string, error)
	SaveFavoritePackage(ctx context.Context, favorite *domain.FavoritePackage) error
	RemoveFavoritePackage(ctx context.Context, userID, packageID string) error
}

func NewHandler(service Reader, favorites favoriteStore, jwt *auth.JWTService) *Handler {
	return &Handler{service: service, favorites: favorites, jwt: jwt}
}

func (h *Handler) Register(mux *http.ServeMux) {
	protected := auth.RequireSession(h.jwt, auth.VerifyOptions{})
	httpserver.HandleFunc(mux, "GET /api/v1/catalog/packages", h.listPackages, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/catalog/packages/{packageId}", h.getPackage, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/catalog/favorites", h.listFavorites, protected)
	httpserver.HandleFunc(mux, "POST /api/v1/catalog/favorites/{packageId}", h.addFavorite, protected)
	httpserver.HandleFunc(mux, "DELETE /api/v1/catalog/favorites/{packageId}", h.removeFavorite, protected)
}

func (h *Handler) listPackages(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, http.StatusUnauthorized, "Sign in required.")
		return
	}

	packages, err := h.service.ListPackages(r.Context())
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load catalog packages from the configured registries.")
		return
	}

	favoriteSet, err := h.favoriteSet(r.Context(), claims.UserID)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load saved stars.")
		return
	}

	for index := range packages {
		packages[index].Starred = favoriteSet[packages[index].ID]
	}

	const defaultLimit = 50
	const maxLimit = 200
	limit := defaultLimit
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}

	total := len(packages)
	if offset >= total {
		packages = packages[:0]
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		packages = packages[offset:end]
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"packages": packages, "total": total})
}

func (h *Handler) getPackage(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, http.StatusUnauthorized, "Sign in required.")
		return
	}

	item, err := h.service.GetPackage(r.Context(), r.PathValue("packageId"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Catalog package not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load catalog package.")
		return
	}

	favoriteSet, err := h.favoriteSet(r.Context(), claims.UserID)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load saved stars.")
		return
	}

	item.Starred = favoriteSet[item.ID]
	httpserver.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) listFavorites(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, http.StatusUnauthorized, "Sign in required.")
		return
	}

	packageIDs, err := h.favorites.ListFavoritePackageIDs(r.Context(), claims.UserID)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load saved stars.")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"packageIds": packageIDs})
}

func (h *Handler) addFavorite(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, http.StatusUnauthorized, "Sign in required.")
		return
	}

	packageID := strings.TrimSpace(r.PathValue("packageId"))
	if packageID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Catalog package is required.")
		return
	}

	if _, err := h.service.GetPackage(r.Context(), packageID); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Catalog package not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load catalog package.")
		return
	}

	err := h.favorites.SaveFavoritePackage(r.Context(), &domain.FavoritePackage{
		UserID:    claims.UserID,
		PackageID: packageID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not save star.")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"packageId": packageID, "starred": true})
}

func (h *Handler) removeFavorite(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.SessionFromContext(r.Context())
	if !ok {
		httpserver.WriteError(w, http.StatusUnauthorized, "Sign in required.")
		return
	}

	packageID := strings.TrimSpace(r.PathValue("packageId"))
	if packageID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "Catalog package is required.")
		return
	}

	if err := h.favorites.RemoveFavoritePackage(r.Context(), claims.UserID, packageID); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not remove star.")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"packageId": packageID, "starred": false})
}
func (h *Handler) favoriteSet(ctx context.Context, userID string) (map[string]bool, error) {
	packageIDs, err := h.favorites.ListFavoritePackageIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	favorites := make(map[string]bool, len(packageIDs))
	for _, packageID := range packageIDs {
		favorites[packageID] = true
	}
	return favorites, nil
}

