package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddlewareSetsResponseHeader(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Fatal("expected request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}), RequestIDMiddleware())

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Header().Get(RequestIDHeader) == "" {
		t.Fatal("expected request id response header")
	}
}

func TestHandleStoresRoutePatternInContext(t *testing.T) {
	mux := http.NewServeMux()
	HandleFunc(mux, "GET /api/v1/example", func(w http.ResponseWriter, r *http.Request) {
		if got := RoutePatternFromContext(r.Context()); got != "GET /api/v1/example" {
			t.Fatalf("expected route pattern, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}

func TestCSRFMiddlewareIssuesCookieOnGet(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CSRFMiddleware())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/suites", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected CSRF cookie to be set on GET response")
	}
}

func TestCSRFMiddlewareBlocksMutationWithoutToken(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CSRFMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suites", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", rec.Code)
	}
}

func TestCSRFMiddlewareAllowsMutationWithValidToken(t *testing.T) {
	const token = "abc123validtoken"
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CSRFMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/suites", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	req.Header.Set(CSRFHeaderName, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid CSRF token, got %d", rec.Code)
	}
}

func TestCSRFMiddlewareBlocksMismatchedToken(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CSRFMiddleware())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/suites/123", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "cookie-token"})
	req.Header.Set(CSRFHeaderName, "different-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on token mismatch, got %d", rec.Code)
	}
}

func TestCSRFMiddlewareExemptsBearer(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CSRFMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.test.sig")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected Bearer-authenticated POST to pass CSRF check, got %d", rec.Code)
	}
}

func TestMiddlewarePreservesFlusherSupport(t *testing.T) {
	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := w.(http.Flusher); !ok {
				t.Fatal("expected wrapped response writer to preserve http.Flusher")
			}
			w.WriteHeader(http.StatusNoContent)
		}),
		RequestIDMiddleware(),
		NewHTTPMetrics().Middleware(),
		AuditMiddleware(nil),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/executions/run-123/events", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}
