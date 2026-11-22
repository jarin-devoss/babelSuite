//go:build integration

package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// These tests run the attack scanner, as shipped, in a real APISIX sidecar,
// against two stub services on this machine:
//
//   - careful: what a careful service does. Security headers everywhere, 401 or
//     404 for what must not be public, 405 for dangerous methods, no GraphQL,
//     CORS for one trusted origin, nothing reflected, a rate limit.
//   - weak: everything wrong at once.
//
// and against a port nothing listens on. Every mode must pass careful, fail weak
// and fail the closed port, where "fail" for the closed port means the step says
// no check reached it.
//
//	go test -tags integration ./internal/runner -run IntegrationSecurity -count=1

// carefulService is a service that does what the scanner checks for.
func carefulService() http.Handler {
	var mu sync.Mutex
	var recent []time.Time
	allow := func() bool { // 20 requests per second on /api/v1/resource
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		kept := recent[:0]
		for _, at := range recent {
			if now.Sub(at) < time.Second {
				kept = append(kept, at)
			}
		}
		recent = kept
		if len(recent) >= 20 {
			return false
		}
		recent = append(recent, now)
		return true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=()")
		if origin := r.Header.Get("Origin"); origin == "https://app.example.test" {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		switch r.Method {
		case "PUT", "DELETE", "TRACE", "TRACK":
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/", "/api", "/api/v1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/v1/resource":
			if !allow() {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// weakService is everything the scanner looks for, and keeps the paths it was
// asked for so a test can see which payloads arrived.
type weakService struct {
	mu       sync.Mutex
	requests []string
	resource atomic.Int64
}

func (s *weakService) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
		s.mu.Unlock()

		// No security headers. CORS reflects whatever asks.
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		switch {
		case r.Method == "TRACE" || r.Method == "TRACK" || r.Method == "PUT" || r.Method == "DELETE":
			w.WriteHeader(http.StatusOK) // dangerous methods accepted
		case r.URL.Path == "/graphql" && r.Method == "POST":
			_, _ = w.Write([]byte(`{"data":{"__schema":{"queryType":{"name":"Query"}}}}`))
		case r.URL.Path == "/api/v1/resource":
			if s.resource.Add(1) > 40 {
				w.WriteHeader(http.StatusServiceUnavailable) // falls over under load, never throttles
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/admin" || r.URL.Path == "/.env" || r.URL.Path == "/internal":
			_, _ = w.Write([]byte("secret=1"))
		case strings.Contains(r.URL.Query().Get("id"), "'"):
			_, _ = w.Write([]byte("You have an error in your SQL syntax"))
		case r.URL.Query().Get("q") != "":
			_, _ = w.Write([]byte(r.URL.Query().Get("q"))) // reflected, unescaped
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	})
}

func (s *weakService) saw(fragment string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, request := range s.requests {
		if strings.Contains(request, fragment) {
			return true
		}
	}
	return false
}

// scan runs one security step through the sidecar, the way the control plane does.
func scan(t *testing.T, variant, target string, adjust func(*suites.SecuritySpec)) (*capturedLines, error) {
	t.Helper()
	spec := suites.SecuritySpec{Variant: variant, Target: target}
	if adjust != nil {
		adjust(&spec)
	}
	step := StepSpec{
		ExecutionID: "run-it-security",
		GatewayURL:  startPluginSidecar(t),
		Node:        StepNode{ID: "scan", Name: "scan", Kind: "security"},
		Security:    &spec,
	}
	log := &capturedLines{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return log, executeSecurityStep(ctx, step, log.emit)
}

func hasFinding(log *capturedLines, label string) bool {
	return log.has("error", "Finding ["+label) || log.has("warn", "Finding ["+label)
}

func shortFlood(spec *suites.SecuritySpec) {
	spec.FloodPath = "/api/v1/resource"
	spec.FloodRate = 50
	spec.FloodDuration = 3
}

// ---- careful ----------------------------------------------------------------

func TestIntegrationSecurityEveryModePassesACarefulService(t *testing.T) {
	careful := stub(t, carefulService())
	cases := map[string]func(*suites.SecuritySpec){
		"security.probe": nil, "security.auth": nil, "security.headers": nil, "security.verbs": nil,
		"security.graphql": nil, "security.cors": nil, "security.fuzz": nil,
		"security.flood": func(spec *suites.SecuritySpec) { shortFlood(spec); spec.FloodThrottle = true },
	}
	for variant, adjust := range cases {
		log, err := scan(t, variant, careful, adjust)
		if err != nil {
			t.Errorf("%s: a careful service must pass: %v\n%v", variant, err, log.texts())
		}
	}
}

func TestIntegrationSecurityFloodAvailabilityPassesACarefulService(t *testing.T) {
	careful := stub(t, carefulService())
	log, err := scan(t, "security.flood", careful, shortFlood)
	if err != nil {
		t.Fatalf("a service that throttles stays available: %v\n%v", err, log.texts())
	}
}

// ---- weak -------------------------------------------------------------------

func TestIntegrationSecurityEveryModeFailsAWeakService(t *testing.T) {
	cases := []struct {
		variant string
		label   string
		adjust  func(*suites.SecuritySpec)
	}{
		{"security.probe", "admin-panel", nil},
		{"security.auth", "admin-no-auth", nil},
		{"security.headers", "Strict-Transport-Security", nil},
		{"security.verbs", "PUT-bypass", nil},
		{"security.graphql", "graphql-introspection", nil},
		{"security.cors", "cors-reflect-origin", nil},
		{"security.fuzz", "sqli-query", nil},
		{"security.flood", "rate-limit", func(spec *suites.SecuritySpec) { shortFlood(spec); spec.FloodThrottle = true }},
		{"security.flood", "availability", shortFlood},
	}
	for _, tc := range cases {
		weak := &weakService{}
		target := stub(t, weak.handler())
		log, err := scan(t, tc.variant, target, tc.adjust)
		if err == nil {
			t.Errorf("%s: a weak service must fail. Log: %v", tc.variant, log.texts())
			continue
		}
		if !strings.Contains(err.Error(), "finding(s) at or above") {
			t.Errorf("%s: the failure must be about findings, got %v", tc.variant, err)
		}
		if !hasFinding(log, tc.label) && !hasFinding(log, "sqli/"+tc.label) {
			t.Errorf("%s: expected a %q finding, got %v", tc.variant, tc.label, log.texts())
		}
	}
}

func TestIntegrationSecurityTechniqueLimitsThePayloadsThatAreSent(t *testing.T) {
	weak := &weakService{}
	target := stub(t, weak.handler())
	log, err := scan(t, "security.fuzz", target, func(spec *suites.SecuritySpec) { spec.Technique = "sqli" })
	if err == nil {
		t.Fatalf("a weak service must fail the sqli family: %v", log.texts())
	}
	if !weak.saw("id=") {
		t.Fatal("expected SQL injection payloads to be sent")
	}
	if weak.saw("script") || weak.saw("etc%2Fpasswd") || weak.saw("etc/passwd") {
		t.Fatalf("technique=sqli must send no XSS or traversal payload, but the service saw %v", weak.requests)
	}
	for _, text := range log.texts() {
		if strings.Contains(text, "xss") || strings.Contains(text, "traversal") {
			t.Fatalf("a finding from another family appeared: %s", text)
		}
	}
}

func TestIntegrationSecurityFailOnLetsLowerFindingsThrough(t *testing.T) {
	weak := &weakService{}
	target := stub(t, weak.handler())
	// A weak service's header findings are medium or lower for three of the six
	// headers, and high for HSTS and CSP: with fail_on=critical none of them fails.
	log, err := scan(t, "security.headers", target, func(spec *suites.SecuritySpec) { spec.FailOn = "critical" })
	if err != nil {
		t.Fatalf("findings below fail_on must not fail the step: %v", err)
	}
	if !log.has("warn", "Finding [Strict-Transport-Security]") {
		t.Fatalf("they must still be shown as warnings: %v", log.texts())
	}
}

// ---- closed port ------------------------------------------------------------

func TestIntegrationSecurityEveryModeFailsWhenNothingAnswers(t *testing.T) {
	dead := deadURL(t)
	for _, variant := range []string{
		"security.probe", "security.auth", "security.headers", "security.verbs",
		"security.graphql", "security.cors", "security.fuzz", "security.flood",
	} {
		_, err := scan(t, variant, dead, func(spec *suites.SecuritySpec) {
			if variant == "security.flood" {
				shortFlood(spec)
			}
		})
		if err == nil || !strings.Contains(err.Error(), "no check reached "+dead) {
			t.Errorf("%s: a target nothing answers must fail as unreachable, got %v", variant, err)
		}
	}
}

func TestIntegrationSecurityCorsAndVerbsAgainstANameThatDoesNotResolve(t *testing.T) {
	// FIX-PLAN #21: these two used to pass with total=0.
	for _, variant := range []string{"security.cors", "security.verbs"} {
		_, err := scan(t, variant, "http://nothing.invalid:8000", nil)
		if err == nil || !strings.Contains(err.Error(), "no check reached") {
			t.Fatalf("%s: expected a failure, got %v", variant, err)
		}
		if !strings.Contains(err.Error(), "name error") && !strings.Contains(err.Error(), "dns") && !strings.Contains(err.Error(), "resolve") {
			t.Fatalf("%s: the failure must name the name lookup as the cause, got %v", variant, err)
		}
	}
}

// ---- the Host header and concurrency ---------------------------------------

func TestIntegrationSecurityScannerSendsTheHostHeaderItIsGiven(t *testing.T) {
	var host atomic.Value
	target := stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.Store(r.Host)
		w.WriteHeader(http.StatusNotFound)
	}))
	base := startPluginSidecar(t)
	body, _ := json.Marshal(scannerConfig{Target: target, HostHeader: "shop.mock.internal", Mode: "probe", TimeoutMS: 5000})
	resp, err := http.Post(base+"/_babelsuite/attack/start", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if got, _ := host.Load().(string); got != "shop.mock.internal" {
		t.Fatalf("the mock surface is selected by Host, and the scanner sent %q", got)
	}
}

func TestIntegrationSecurityTwoScansAtOnceKeepTheirOwnLabels(t *testing.T) {
	weak := &weakService{}
	target := stub(t, weak.handler())

	var wg sync.WaitGroup
	logs := make([]*capturedLines, 2)
	for index, technique := range []string{"sqli", "xss"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logs[index], _ = scan(t, "security.fuzz", target, func(spec *suites.SecuritySpec) { spec.Technique = technique })
		}()
	}
	wg.Wait()

	for index, prefix := range []string{"sqli/", "xss/"} {
		other := map[string]string{"sqli/": "xss/", "xss/": "sqli/"}[prefix]
		for _, text := range logs[index].texts() {
			if strings.Contains(text, "Finding [") && !strings.Contains(text, "Finding ["+prefix) {
				t.Errorf("a %s scan reported a finding labelled by the other: %s", prefix, text)
			}
			if strings.Contains(text, "Finding ["+other) {
				t.Errorf("a %s scan carries a %s label: %s", prefix, other, text)
			}
		}
	}
}

func TestIntegrationSecurityScannerRefusesAnUnknownModeAndTechnique(t *testing.T) {
	base := startPluginSidecar(t)
	for _, payload := range []string{
		`{"target":"http://x","mode":"nope"}`,
		`{"target":"http://x","mode":"fuzz","technique":"nope"}`,
		`{"mode":"probe"}`,
	} {
		resp, err := http.Post(base+"/_babelsuite/attack/start", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", payload, resp.StatusCode)
		}
	}
}
