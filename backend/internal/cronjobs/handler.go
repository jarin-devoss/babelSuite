package cronjobs

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/httpserver"
)

type Handler struct {
	svc *Service
	jwt *auth.JWTService
}

func NewHandler(svc *Service, jwt *auth.JWTService) *Handler {
	return &Handler{svc: svc, jwt: jwt}
}

func (h *Handler) Register(mux *http.ServeMux) {
	protected := auth.RequireSession(h.jwt, auth.VerifyOptions{})
	httpserver.HandleFunc(mux, "GET /api/v1/cron-jobs", h.list, protected)
	httpserver.HandleFunc(mux, "POST /api/v1/cron-jobs", h.create, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/cron-jobs/{id}", h.get, protected)
	httpserver.HandleFunc(mux, "PUT /api/v1/cron-jobs/{id}", h.update, protected)
	httpserver.HandleFunc(mux, "DELETE /api/v1/cron-jobs/{id}", h.delete, protected)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.svc.List(r.Context())
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load cron jobs.")
		return
	}
	if jobs == nil {
		jobs = []CronJob{}
	}
	httpserver.WriteJSON(w, http.StatusOK, jobs)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var job CronJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	created, err := h.svc.Create(r.Context(), &job)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not create cron job.")
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := h.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Cron job not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load cron job.")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, job)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var patch CronJob
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	patch.ID = id
	updated, err := h.svc.Update(r.Context(), &patch)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Cron job not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not update cron job.")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Cron job not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not delete cron job.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
