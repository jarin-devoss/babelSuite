package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraceProxyHandler_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	h := NewTraceProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
}

func TestMetricsProxyHandler_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	h := NewMetricsProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
}

func TestTraceProxyHandler_ForwardsToCollector(t *testing.T) {
	body := `{"resourceSpans":[]}`
	var receivedPath string
	var receivedBody string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", upstream.URL)

	h := NewTraceProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if receivedPath != "/v1/traces" {
		t.Errorf("upstream received path %q, want /v1/traces", receivedPath)
	}
	if receivedBody != body {
		t.Errorf("upstream received body %q, want %q", receivedBody, body)
	}
}

func TestMetricsProxyHandler_ForwardsToCollector(t *testing.T) {
	body := `{"resourceMetrics":[]}`
	var receivedPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", upstream.URL)

	h := NewMetricsProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if receivedPath != "/v1/metrics" {
		t.Errorf("upstream received path %q, want /v1/metrics", receivedPath)
	}
}

func TestTraceProxyHandler_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", upstream.URL)

	h := NewTraceProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestTraceProxyHandler_ForwardsCustomHeaders(t *testing.T) {
	var receivedHeader string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Tenant")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", upstream.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Tenant=acme")

	h := NewTraceProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if receivedHeader != "acme" {
		t.Errorf("upstream X-Tenant = %q, want %q", receivedHeader, "acme")
	}
}
