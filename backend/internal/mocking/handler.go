package mocking

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/babelsuite/babelsuite/internal/httpserver"
	"github.com/babelsuite/babelsuite/internal/suites"
)

type Handler struct {
	service *Service
	secret  string
}

func NewHandler(service *Service, secret string) *Handler {
	return &Handler{service: service, secret: strings.TrimSpace(secret)}
}

func (h *Handler) mockAuthMiddleware() httpserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.secret != "" {
				bearer := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
				if strings.TrimSpace(bearer) != h.secret {
					httpserver.WriteError(w, http.StatusUnauthorized, "Mock access requires a valid secret.")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	auth := h.mockAuthMiddleware()
	httpserver.HandleFunc(mux, "/internal/mock-data/", h.resolveOperation, auth)
	httpserver.HandleFunc(mux, "/mocks/rest/", h.invokeREST, auth)
	httpserver.HandleFunc(mux, "POST /mocks/grpc/{suiteId}/{surfaceId}/{operationId}", h.invokeGRPC, auth)
	httpserver.HandleFunc(mux, "POST /mocks/async/{suiteId}/{surfaceId}/{operationId}", h.invokeAsync, auth)
}

func (h *Handler) resolveOperation(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/internal/mock-data/")
	parts := strings.SplitN(strings.Trim(trimmed, "/"), "/", 3)
	if len(parts) < 3 {
		httpserver.WriteError(w, http.StatusNotFound, "Mock resolver route not found.")
		return
	}

	result, err := h.service.ResolveOperation(r.Context(), parts[0], parts[1], parts[2], r)
	if err != nil {
		h.writeLookupError(w, err)
		return
	}
	writeResolverEnvelope(w, result)
}

func (h *Handler) invokeREST(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/mocks/rest/")
	parts := strings.SplitN(strings.Trim(trimmed, "/"), "/", 3)
	if len(parts) < 3 {
		httpserver.WriteError(w, http.StatusNotFound, "Mock route not found.")
		return
	}

	result, err := h.service.InvokeREST(r.Context(), parts[0], parts[1], "/"+parts[2], r)
	if err != nil {
		h.writeLookupError(w, err)
		return
	}
	writeResult(w, result)
}

func (h *Handler) invokeGRPC(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.InvokeAdapter(r.Context(), r.PathValue("suiteId"), r.PathValue("surfaceId"), r.PathValue("operationId"), "grpc", r)
	if err != nil {
		h.writeLookupError(w, err)
		return
	}
	writeResult(w, result)
}

func (h *Handler) invokeAsync(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.InvokeAdapter(r.Context(), r.PathValue("suiteId"), r.PathValue("surfaceId"), r.PathValue("operationId"), "async", r)
	if err != nil {
		h.writeLookupError(w, err)
		return
	}
	writeResult(w, result)
}

func (h *Handler) writeLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, suites.ErrNotFound):
		httpserver.WriteError(w, http.StatusNotFound, "Suite not found.")
	case errors.Is(err, ErrSurfaceNotFound):
		httpserver.WriteError(w, http.StatusNotFound, "Mock surface not found.")
	case errors.Is(err, ErrOperationNotFound):
		httpserver.WriteError(w, http.StatusNotFound, "Mock operation not found.")
	default:
		httpserver.WriteError(w, http.StatusInternalServerError, "Could not process mock invocation.")
	}
}

func writeResult(w http.ResponseWriter, result *Result) {
	if result == nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Mock result was empty.")
		return
	}

	for key, values := range result.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if strings.TrimSpace(result.MediaType) != "" {
		w.Header().Set("Content-Type", result.MediaType)
	}
	if strings.TrimSpace(result.RuntimeURL) != "" {
		w.Header().Set("X-Babelsuite-Runtime-Url", result.RuntimeURL)
	}
	if strings.TrimSpace(result.Adapter) != "" {
		w.Header().Set("X-Babelsuite-Mock-Adapter", result.Adapter)
	}
	if strings.TrimSpace(result.Dispatcher) != "" {
		w.Header().Set("X-Babelsuite-Dispatcher", result.Dispatcher)
	}
	if strings.TrimSpace(result.ResolverURL) != "" {
		w.Header().Set("X-Babelsuite-Resolver-Url", result.ResolverURL)
	}
	if strings.TrimSpace(result.MatchedExample) != "" {
		w.Header().Set("X-Babelsuite-Mock-Example", result.MatchedExample)
	}
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

func writeResolverEnvelope(w http.ResponseWriter, result *Result) {
	if result == nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "Mock result was empty.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	envelope := map[string]any{
		"status":         result.Status,
		"mediaType":      result.MediaType,
		"headers":        result.Headers,
		"body":           string(result.Body),
		"adapter":        result.Adapter,
		"dispatcher":     result.Dispatcher,
		"resolverUrl":    result.ResolverURL,
		"runtimeUrl":     result.RuntimeURL,
		"matchedExample": result.MatchedExample,
	}
	_ = json.NewEncoder(w).Encode(envelope)
}

