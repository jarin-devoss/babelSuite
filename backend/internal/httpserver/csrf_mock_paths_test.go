package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The mock engine's endpoints verify a secret of their own on every call, so CSRF
// does not guard them. Before, they got past it only because the gateway happened
// to send a bearer header.
func TestCSRFDoesNotGuardTheSelfAuthenticatingMockPaths(t *testing.T) {
	reached := 0
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/mocks/rest/suite/surface/orders", "/mocks/async/s/x/y", "/internal/mock-data/suite/surface/op"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("POST %s with no CSRF cookie and no bearer was refused: %d", path, recorder.Code)
		}
	}
	if reached != 3 {
		t.Fatalf("handler reached %d times, want 3", reached)
	}
}

func TestCSRFStillGuardsTheRestOfTheAPI(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/api/v1/executions", "/api/v1/mocks-admin", "/mocksX/rest/a", "/x/mocks/rest/a"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusForbidden {
			t.Errorf("POST %s with no CSRF cookie was not refused: %d", path, recorder.Code)
		}
	}
}
