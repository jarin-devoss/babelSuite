package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/engine"
	"github.com/babelsuite/babelsuite/internal/httpserver"
)

type Handler struct {
	service *Service
	engine  *engine.Store
	jwt     *auth.JWTService
}

func NewHandler(service *Service, engineStore *engine.Store, jwt *auth.JWTService) *Handler {
	return &Handler{service: service, engine: engineStore, jwt: jwt}
}

func (h *Handler) Register(mux *http.ServeMux) {
	protected := auth.RequireSession(h.jwt, auth.VerifyOptions{})
	streaming := auth.RequireSession(h.jwt, auth.VerifyOptions{AllowQueryToken: false})
	createLimit := auth.NewIPRateLimiter(30, time.Minute).Middleware()
	httpserver.HandleFunc(mux, "GET /api/v1/executions/launch-suites", h.listLaunchSuites, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/resolve-ref", h.resolveRef, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/overview", h.getOverview, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/executions", h.listExecutions, protected)
	httpserver.HandleFunc(mux, "POST /api/v1/executions", h.createExecution, protected, createLimit)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/{executionId}", h.getExecution, protected)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/{executionId}/events", h.streamEvents, streaming)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/{executionId}/logs", h.streamLogs, streaming)
	httpserver.HandleFunc(mux, "GET /api/v1/executions/{executionId}/logs/snapshot", h.snapshotLogs, protected)
}

func (h *Handler) listLaunchSuites(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"suites": h.service.ListLaunchSuites()})
}

func (h *Handler) resolveRef(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "ref query parameter is required.")
		return
	}
	suite, err := h.service.ResolveRef(ref)
	if err != nil {
		httpserver.WriteError(w, http.StatusNotFound, "Suite not found.")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, suite)
}

func (h *Handler) getOverview(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		httpserver.WriteJSON(w, http.StatusOK, engine.Overview{})
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, h.engine.Overview())
}

func (h *Handler) listExecutions(w http.ResponseWriter, r *http.Request) {
	workspaceID := ""
	if claims, ok := auth.SessionFromContext(r.Context()); ok {
		workspaceID = claims.WorkspaceID
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

	executions, total := h.service.ListExecutions(workspaceID, offset, limit)
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"executions": executions, "total": total})
}

func (h *Handler) createExecution(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(executionScope).Start(r.Context(), "execution.create",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	var request CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "Execution payload is invalid.")
		return
	}
	if claims, ok := auth.SessionFromContext(ctx); ok {
		request.WorkspaceID = claims.WorkspaceID
	}

	execution, err := h.service.CreateExecution(ctx, request)
	if err != nil {
		switch {
		case errors.Is(err, ErrSuiteNotFound):
			httpserver.WriteError(w, http.StatusNotFound, "Suite not found.")
		case errors.Is(err, ErrProfileNotFound):
			httpserver.WriteError(w, http.StatusBadRequest, "Selected profile does not belong to this suite.")
		case errors.Is(err, ErrProfileRuntime):
			httpserver.WriteError(w, http.StatusBadRequest, "Profile runtime configuration is invalid.")
		case errors.Is(err, ErrBackendNotFound):
			httpserver.WriteError(w, http.StatusBadRequest, "Selected backend does not exist.")
		case errors.Is(err, ErrBackendUnavailable):
			httpserver.WriteError(w, http.StatusServiceUnavailable, "Selected backend is unavailable.")
		case errors.Is(err, ErrInvalidTopology):
			httpserver.WriteError(w, http.StatusBadRequest, "Suite topology is invalid.")
		default:
			httpserver.WriteError(w, http.StatusInternalServerError, "Could not create execution.")
		}
		return
	}

	httpserver.WriteJSON(w, http.StatusCreated, execution)
}

func (h *Handler) getExecution(w http.ResponseWriter, r *http.Request) {
	_, span := otel.Tracer(executionScope).Start(r.Context(), "execution.get",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("execution.id", r.PathValue("executionId"))),
	)
	defer span.End()

	workspaceID := ""
	if claims, ok := auth.SessionFromContext(r.Context()); ok {
		workspaceID = claims.WorkspaceID
	}
	execution, err := h.service.GetExecution(r.PathValue("executionId"), workspaceID)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Execution not found.")
			return
		}

		httpserver.WriteError(w, http.StatusInternalServerError, "Could not load execution.")
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, execution)
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request) {
	executionID := r.PathValue("executionId")
	_, span := otel.Tracer(executionScope).Start(r.Context(), "execution.stream_events",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("execution.id", executionID)),
	)
	defer span.End()

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpserver.WriteError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	since := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			since = parsed
		}
	}
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > since {
			since = parsed
		}
	}

	eventsWorkspaceID := ""
	if claims, ok := auth.SessionFromContext(r.Context()); ok {
		eventsWorkspaceID = claims.WorkspaceID
	}
	stream, err := h.service.SubscribeEvents(r.Context(), r.PathValue("executionId"), eventsWorkspaceID, since)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Execution not found.")
			return
		}

		httpserver.WriteError(w, http.StatusInternalServerError, "Could not stream execution events.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case event := <-stream:
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %d\n", event.ID)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (h *Handler) streamLogs(w http.ResponseWriter, r *http.Request) {
	executionID := r.PathValue("executionId")
	_, span := otel.Tracer(executionScope).Start(r.Context(), "execution.stream_logs",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("execution.id", executionID)),
	)
	defer span.End()

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpserver.WriteError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	since := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			since = parsed
		}
	}
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > since {
			since = parsed
		}
	}

	logsWorkspaceID := ""
	if claims, ok := auth.SessionFromContext(r.Context()); ok {
		logsWorkspaceID = claims.WorkspaceID
	}
	stream, err := h.service.SubscribeLogs(r.Context(), r.PathValue("executionId"), logsWorkspaceID, since)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Execution not found.")
			return
		}

		httpserver.WriteError(w, http.StatusInternalServerError, "Could not stream execution logs.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case record := <-stream:
			payload, err := json.Marshal(record)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %d\n", record.ID)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (h *Handler) snapshotLogs(w http.ResponseWriter, r *http.Request) {
	executionID := r.PathValue("executionId")
	workspaceID := ""
	if claims, ok := auth.SessionFromContext(r.Context()); ok {
		workspaceID = claims.WorkspaceID
	}
	lines, err := h.service.SnapshotLogs(executionID, workspaceID)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			httpserver.WriteError(w, http.StatusNotFound, "Execution not found.")
			return
		}
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not read logs.")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"executionId": executionID, "lines": lines})
}
