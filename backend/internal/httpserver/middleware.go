package httpserver

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/auth"
	"github.com/babelsuite/babelsuite/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// AuditWriter is implemented by any store that can persist HTTP audit entries.
type AuditWriter interface {
	WriteAuditLog(ctx context.Context, entry *domain.AuditEntry) error
}

type Middleware func(http.Handler) http.Handler

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}


type HTTPMetrics struct {
	requests  metric.Int64Counter
	active    metric.Int64UpDownCounter
	durations metric.Float64Histogram
}

func Chain(next http.Handler, middleware ...Middleware) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	for index := len(middleware) - 1; index >= 0; index-- {
		next = middleware[index](next)
	}
	return next
}

func Handle(mux *http.ServeMux, pattern string, handler http.Handler, middleware ...Middleware) {
	if mux == nil {
		return
	}
	chain := append([]Middleware{routePatternMiddleware(pattern)}, middleware...)
	mux.Handle(pattern, Chain(handler, chain...))
}

func HandleFunc(mux *http.ServeMux, pattern string, handler func(http.ResponseWriter, *http.Request), middleware ...Middleware) {
	Handle(mux, pattern, http.HandlerFunc(handler), middleware...)
}

func NewHTTPMetrics() *HTTPMetrics {
	meter := otel.Meter("github.com/babelsuite/babelsuite/internal/httpserver")
	requests, _ := meter.Int64Counter("http.server.requests")
	active, _ := meter.Int64UpDownCounter("http.server.active_requests")
	durations, _ := meter.Float64Histogram("http.server.duration.ms")
	return &HTTPMetrics{
		requests:  requests,
		active:    active,
		durations: durations,
	}
}

func (m *HTTPMetrics) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			startedAt := time.Now()
			attrs := requestAttributes(r, http.StatusOK)

			if m != nil {
				m.active.Add(r.Context(), 1, metric.WithAttributes(attrs...))
				defer m.active.Add(r.Context(), -1, metric.WithAttributes(attrs...))
			}

			next.ServeHTTP(recorder, r)

			attrs = requestAttributes(r, recorder.status)
			if m != nil {
				m.requests.Add(r.Context(), 1, metric.WithAttributes(attrs...))
				m.durations.Record(r.Context(), float64(time.Since(startedAt).Milliseconds()), metric.WithAttributes(attrs...))
			}
		})
	}
}

func TraceContextMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			if span.IsRecording() {
				if requestID := RequestIDFromContext(r.Context()); requestID != "" {
					span.SetAttributes(attribute.String("http.request_id", requestID))
				}
				if claims, ok := auth.SessionFromContext(r.Context()); ok {
					span.SetAttributes(
						attribute.String("enduser.id", claims.UserID),
						attribute.String("enduser.workspace_id", claims.WorkspaceID),
						attribute.Bool("enduser.admin", claims.IsAdmin),
					)
				}
				// Defer route annotation until after the mux has matched the pattern
				// so span names are "METHOD /path/{param}" rather than raw URLs.
				defer func() {
					if pattern := effectiveRoute(r); pattern != "" {
						span.SetName(r.Method + " " + pattern)
						span.SetAttributes(attribute.String("http.route", pattern))
					}
				}()
			}
			next.ServeHTTP(w, r)
		})
	}
}

func AuditMiddleware(writer AuditWriter) Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			startedAt := time.Now()
			next.ServeHTTP(recorder, r)

			if !shouldAuditRequest(r) {
				return
			}

			durationMs := time.Since(startedAt).Milliseconds()
			entry := &domain.AuditEntry{
				Method:     r.Method,
				Path:       r.URL.Path,
				Status:     recorder.status,
				DurationMs: durationMs,
				RemoteAddr: strings.TrimSpace(r.RemoteAddr),
				CreatedAt:  startedAt.UTC(),
			}
			if id := RequestIDFromContext(r.Context()); id != "" {
				entry.RequestID = id
			}
			if route := effectiveRoute(r); route != "" {
				entry.Route = route
			}
			if claims, ok := auth.SessionFromContext(r.Context()); ok {
				entry.UserID = claims.UserID
				entry.WorkspaceID = claims.WorkspaceID
			}

			attrs := []slog.Attr{
				slog.String("method", entry.Method),
				slog.String("path", entry.Path),
				slog.Int("status", entry.Status),
				slog.Int64("durationMs", durationMs),
				slog.String("remoteAddr", entry.RemoteAddr),
			}
			if entry.RequestID != "" {
				attrs = append(attrs, slog.String("requestId", entry.RequestID))
			}
			if entry.Route != "" {
				attrs = append(attrs, slog.String("route", entry.Route))
			}
			if entry.UserID != "" {
				attrs = append(attrs, slog.String("userId", entry.UserID))
				attrs = append(attrs, slog.String("workspaceId", entry.WorkspaceID))
			}
			slog.LogAttrs(r.Context(), slog.LevelInfo, "http.request", attrs...)

			if writer != nil {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := writer.WriteAuditLog(ctx, entry); err != nil {
						slog.Warn("audit log write failed", "error", err)
					}
				}()
			}
		})
	}
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(payload)
	w.bytes += written
	return written, err
}

func (w *statusRecorder) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()
}

func requestAttributes(r *http.Request, status int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("http.method", r.Method),
		attribute.String("http.route", effectiveRoute(r)),
		attribute.String("http.status_code", strconv.Itoa(status)),
	}
}

func effectiveRoute(r *http.Request) string {
	if pattern := strings.TrimSpace(RoutePatternFromContext(r.Context())); pattern != "" {
		return pattern
	}
	if pattern := strings.TrimSpace(r.Pattern); pattern != "" {
		return pattern
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		return "/"
	}
	return path
}

const contentSecurityPolicy = "default-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"connect-src 'self' ws: wss:; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

func RecoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.ErrorContext(r.Context(), "handler panic",
						slog.Any("error", rec),
						slog.String("stack", string(debug.Stack())),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"An unexpected error occurred."}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeadersMiddleware(trust *ProxyTrust) Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
			w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
			if IsSecureRequest(r, trust) {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func BodyLimitMiddleware(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFCookieName is the name of the cookie that carries the CSRF token.
const CSRFCookieName = "csrf_token"

// CSRFHeaderName is the request header the client must echo back.
const CSRFHeaderName = "X-CSRF-Token"

// CSRFMiddleware issues a per-session CSRF token as a SameSite=Strict cookie
// and validates it on every state-mutating request (POST, PUT, PATCH, DELETE).
//
// Requests that supply a valid Authorization: Bearer header are exempt because
// custom headers cannot be set by cross-origin HTML forms or img/script tags,
// making Bearer-authenticated endpoints inherently CSRF-safe.
func CSRFMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bearerTokenPresent(r) {
				next.ServeHTTP(w, r)
				return
			}

			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
				ensureCSRFCookie(w, r)
				next.ServeHTTP(w, r)
			default:
				cookie, err := r.Cookie(CSRFCookieName)
				if err != nil || strings.TrimSpace(cookie.Value) == "" {
					http.Error(w, `{"error":"CSRF token missing."}`, http.StatusForbidden)
					return
				}
				if r.Header.Get(CSRFHeaderName) != cookie.Value {
					http.Error(w, `{"error":"CSRF token invalid."}`, http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			}
		})
	}
}

func bearerTokenPresent(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return strings.HasPrefix(auth, "Bearer ") && len(auth) > len("Bearer ")
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CSRFCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
		return
	}
	token := generateCSRFToken()
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func shouldAuditRequest(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/readyz/") {
		return false
	}
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/auth/")
}
