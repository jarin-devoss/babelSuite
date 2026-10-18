package agent

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/babelsuite/babelsuite/internal/httpserver"
)

func agentHandlerSecretMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret != "" {
				bearer := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
				if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(bearer)), []byte(secret)) != 1 {
					httpserver.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "agent secret required"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func NewHandler(service *Service, secret string) http.Handler {
	mux := http.NewServeMux()
	requireSecret := agentHandlerSecretMiddleware(secret)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /api/v1/agent/info", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpserver.WriteJSON(w, http.StatusOK, service.Info())
	})))
	mux.Handle("POST /api/v1/agent/run", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request StepRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			httpserver.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "request payload is invalid"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			httpserver.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming is not supported"})
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		writer := bufio.NewWriter(w)
		service.Stream(r.Context(), request, func(message StreamMessage) {
			_ = json.NewEncoder(writer).Encode(message)
			_ = writer.Flush()
			flusher.Flush()
		})
	})))
	mux.Handle("GET /api/v1/agent/inventory", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service.inventory == nil {
			httpserver.WriteJSON(w, http.StatusNotImplemented, map[string]string{"error": "this agent does not report an inventory"})
			return
		}
		executions, err := service.inventory(r.Context())
		if err != nil {
			httpserver.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"executions": executions})
	})))
	mux.Handle("POST /api/v1/agent/executions/{executionId}/reap", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service.reap == nil {
			httpserver.WriteJSON(w, http.StatusNotImplemented, map[string]string{"error": "this agent cannot reap executions"})
			return
		}
		if err := service.reap(r.Context(), r.PathValue("executionId")); err != nil {
			httpserver.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "reaped"})
	})))
	mux.Handle("POST /api/v1/agent/jobs/{jobId}/cancel", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service.Cancel(r.PathValue("jobId")) {
			httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
			return
		}
		httpserver.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
	})))
	mux.Handle("POST /api/v1/agent/jobs/{jobId}/cleanup", requireSecret(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service.Cleanup(r.PathValue("jobId")) {
			httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleaned"})
			return
		}
		httpserver.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
	})))
	return mux
}

