package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func securityStep(variant, target string) StepSpec {
	return StepSpec{
		Node:     StepNode{ID: "s", Name: "s", Kind: "security"},
		Security: &suites.SecuritySpec{Variant: variant, Target: target},
	}
}

func collectSecurityLogs(step StepSpec, fn func(context.Context, StepSpec, func(logstream.Line)) error) ([]logstream.Line, error) {
	var lines []logstream.Line
	err := fn(context.Background(), step, func(l logstream.Line) { lines = append(lines, l) })
	return lines, err
}

func TestNativeProbeReachableTarget(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := collectSecurityLogs(securityStep("security.probe", srv.URL), executeNativeSecurityStep)
	if err != nil {
		t.Fatalf("unexpected error for reachable target: %v", err)
	}
}

func TestNativeProbeUnreachableTarget(t *testing.T) {
	t.Parallel()
	lines, err := collectSecurityLogs(securityStep("security.probe", "http://127.0.0.1:19999"), executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error for unreachable target")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "Finding") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a Finding log line for unreachable target")
	}
}

func TestNativeHeadersMissingSecurityHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	lines, err := collectSecurityLogs(securityStep("security.headers", srv.URL), executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error when required security headers are absent")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "Finding") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected header findings when security headers are absent")
	}
}

func TestNativeHeadersPresent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := collectSecurityLogs(securityStep("security.headers", srv.URL), executeNativeSecurityStep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNativeVerbsBlocksDangerousMethods(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	lines, err := collectSecurityLogs(securityStep("security.verbs", srv.URL), executeNativeSecurityStep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, l := range lines {
		if strings.Contains(l.Text, "Finding") && strings.Contains(l.Text, "verb.") {
			t.Fatalf("no verb findings expected when server rejects all unsafe methods; got: %s", l.Text)
		}
	}
}

func TestNativeVerbsAcceptedMethod(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "TRACE" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	lines, err := collectSecurityLogs(securityStep("security.verbs", srv.URL), executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error when an unsafe verb is accepted")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "verb.trace") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected finding for accepted TRACE method")
	}
}

func TestNativeCORSWildcardOrigin(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	lines, err := collectSecurityLogs(securityStep("security.cors", srv.URL), executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error for wildcard CORS misconfiguration")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "cors.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected CORS finding for wildcard origin")
	}
}

func TestNativeFuzzNoDisclosure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := collectSecurityLogs(securityStep("security.fuzz", srv.URL), executeNativeSecurityStep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNativeFuzzSQLErrorDisclosure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("you have an error in your sql syntax near '1'"))
	}))
	defer srv.Close()

	lines, err := collectSecurityLogs(securityStep("security.fuzz", srv.URL), executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error when SQL error disclosure is detected")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "sqli") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected SQL injection finding on error disclosure")
	}
}

func TestNativeUnsupportedVariantSkips(t *testing.T) {
	t.Parallel()
	_, err := collectSecurityLogs(securityStep("security.graphql", "http://localhost"), executeNativeSecurityStep)
	if err != nil {
		t.Fatalf("unsupported variant should skip cleanly, got error: %v", err)
	}
}

func TestNativeSecurityStepMissingTarget(t *testing.T) {
	t.Parallel()
	step := StepSpec{
		Node:     StepNode{ID: "s", Name: "s", Kind: "security"},
		Security: &suites.SecuritySpec{Variant: "security.probe", Target: ""},
	}
	_, err := collectSecurityLogs(step, executeNativeSecurityStep)
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}
