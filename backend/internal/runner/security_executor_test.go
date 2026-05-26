package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func apisixStep(variant, gatewayURL string) StepSpec {
	return StepSpec{
		Node:       StepNode{ID: "sec", Name: "sec", Kind: "security"},
		GatewayURL: gatewayURL,
		Security:   &suites.SecuritySpec{Variant: variant, Target: gatewayURL},
	}
}

func apisixResult(total, passed, failed int, findings []scannerFinding) []byte {
	r := scannerResult{Total: total, Passed: passed, Failed: failed, Findings: findings}
	b, _ := json.Marshal(r)
	return b
}

func TestExecuteSecurityStepRequiresGateway(t *testing.T) {
	t.Parallel()
	step := StepSpec{
		Node:     StepNode{ID: "sec", Name: "sec", Kind: "security"},
		Security: &suites.SecuritySpec{Variant: "security.probe"},
	}
	err := executeSecurityStep(context.Background(), step, func(logstream.Line) {}) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error when GatewayURL is empty")
	}
	if !strings.Contains(err.Error(), "APISIX sidecar") {
		t.Fatalf("error should mention APISIX sidecar, got: %v", err)
	}
}

func TestExecuteSecurityStepMissingSecuritySpec(t *testing.T) {
	t.Parallel()
	step := StepSpec{
		Node:       StepNode{ID: "sec", Name: "sec", Kind: "security"},
		GatewayURL: "http://localhost:9080",
	}
	err := executeSecurityStep(context.Background(), step, func(logstream.Line) {}) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error when Security spec is nil")
	}
}

func TestExecuteSecurityStepAllPassNoFindings(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(apisixResult(5, 5, 0, nil))
	}))
	defer srv.Close()

	step := apisixStep("security.probe", srv.URL)
	var lines []logstream.Line
	err := executeSecurityStep(context.Background(), step, func(l logstream.Line) { lines = append(lines, l) }) //nolint:staticcheck
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "total=5") && strings.Contains(l.Text, "passed=5") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected summary log with total=5 passed=5")
	}
}

func TestExecuteSecurityStepFindingsReturnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(apisixResult(3, 2, 1, []scannerFinding{
			{Label: "probe.admin", Method: "GET", Path: "/admin", Status: 200, Severity: "high", Detail: "exposed"},
		}))
	}))
	defer srv.Close()

	step := apisixStep("security.probe", srv.URL)
	var lines []logstream.Line
	err := executeSecurityStep(context.Background(), step, func(l logstream.Line) { lines = append(lines, l) }) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error when findings.failed > 0")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "Finding") && strings.Contains(l.Text, "probe.admin") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected Finding log line for the failed check")
	}
}

func TestExecuteSecurityStepBadJSONFromAPISIX(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	step := apisixStep("security.probe", srv.URL)
	err := executeSecurityStep(context.Background(), step, func(logstream.Line) {}) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error when APISIX returns malformed JSON")
	}
}

func TestExecuteSecurityStepAPISIXNonOKStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("sidecar error"))
	}))
	defer srv.Close()

	step := apisixStep("security.probe", srv.URL)
	err := executeSecurityStep(context.Background(), step, func(logstream.Line) {}) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error when APISIX returns non-200 status")
	}
}

func TestCanUseAPISIXSecurity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		gatewayURL string
		variant    string
		want       bool
	}{
		{"probe with gateway", "http://gw:9080", "security.probe", true},
		{"fuzz with gateway", "http://gw:9080", "security.fuzz", true},
		{"cors with gateway", "http://gw:9080", "security.cors", true},
		{"graphql with gateway", "http://gw:9080", "security.graphql", true},
		{"empty gateway", "", "security.probe", false},
		{"nil security spec handled by caller", "http://gw:9080", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sec *suites.SecuritySpec
			if tc.variant != "" {
				sec = &suites.SecuritySpec{Variant: tc.variant}
			}
			step := StepSpec{GatewayURL: tc.gatewayURL, Security: sec}
			if got := canUseAPISIXSecurity(step); got != tc.want {
				t.Fatalf("canUseAPISIXSecurity = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildScannerConfigFlood(t *testing.T) {
	t.Parallel()
	spec := &suites.SecuritySpec{
		Variant:       "security.flood",
		FloodPath:     "/api/v1/resource",
		FloodRate:     50,
		FloodDuration: 10,
		FloodThrottle: true,
	}
	cfg := buildScannerConfig(spec, "http://gw:9080")
	if cfg.Mode != "flood" {
		t.Fatalf("mode = %q, want flood", cfg.Mode)
	}
	if cfg.Path != "/api/v1/resource" {
		t.Fatalf("path = %q, want /api/v1/resource", cfg.Path)
	}
	if cfg.Rate != 50 {
		t.Fatalf("rate = %v, want 50", cfg.Rate)
	}
	if cfg.Duration != 10 {
		t.Fatalf("duration = %v, want 10", cfg.Duration)
	}
	if !cfg.ExpectThrottle {
		t.Fatal("expect_throttle should be true")
	}
}
