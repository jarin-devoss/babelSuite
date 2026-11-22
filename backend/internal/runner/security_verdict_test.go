package runner

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// scannerStub answers the scanner's trigger route with a canned report and keeps
// the config the runner sent.
type scannerStub struct {
	*httptest.Server
	mu     sync.Mutex
	config scannerConfig
	calls  int
}

func newScannerStub(t *testing.T, result scannerResult) *scannerStub {
	t.Helper()
	stub := &scannerStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(body)) == "{}" {
			// The runner's readiness probe: the real scanner rejects an empty
			// configuration and runs nothing.
			http.Error(w, "target is required", http.StatusBadRequest)
			return
		}
		stub.mu.Lock()
		stub.calls++
		_ = json.Unmarshal(body, &stub.config)
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *scannerStub) sent() scannerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func securityStep(stub *scannerStub, spec suites.SecuritySpec) StepSpec {
	return StepSpec{
		ExecutionID: "run-security-test",
		Node:        StepNode{ID: "scan", Name: "scan", Kind: "security"},
		GatewayURL:  stub.URL,
		Security:    &spec,
	}
}

func runSecurity(step StepSpec) (*capturedLines, error) {
	log := &capturedLines{}
	err := executeSecurityStep(context.Background(), step, log.emit)
	return log, err
}

func finding(label, severity string) scannerFinding {
	return scannerFinding{Label: label, Method: "GET", Path: "/x", Status: 200, Severity: severity, Detail: "d"}
}

// ---- the verdict ------------------------------------------------------------

func TestSecurityStepFailsWhenNoCheckReachedTheTarget(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 9, Unknown: 9, FirstError: "GET /: dns server error: 3 name error"})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.cors", Target: "https://api.example.test"}))
	if err == nil || !strings.Contains(err.Error(), "no check reached https://api.example.test") || !strings.Contains(err.Error(), "dns server error") {
		t.Fatalf("expected a failure naming the target and the first error, got %v", err)
	}
}

func TestSecurityStepFailsWhenNothingWasExecuted(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.verbs", Target: "https://api.example.test"}))
	if err == nil || !strings.Contains(err.Error(), "no check was executed") {
		t.Fatalf("a run of total=0 must not pass, got %v", err)
	}
}

func TestSecurityStepFailsWhenOnlySkippedPayloadsRan(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Skipped: 2})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.fuzz", Technique: "header-injection", Target: "https://api.example.test"}))
	if err == nil || !strings.Contains(err.Error(), "could not be sent") {
		t.Fatalf("expected the skipped payloads to be named, got %v", err)
	}
}

func TestSecurityStepFailsWhenSomeChecksCouldNotBeDetermined(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 15, Passed: 14, Unknown: 1, FirstError: "GET /admin: timeout"})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test"}))
	if err == nil || !strings.Contains(err.Error(), "1 of 15 checks could not be determined: GET /admin: timeout") {
		t.Fatalf("an unknown check is not a pass, got %v", err)
	}
}

func TestSecurityStepNamesBothFindingsAndUnknownChecks(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 5, Passed: 3, Failed: 1, Unknown: 1, FirstError: "boom", Findings: []scannerFinding{finding("a", "high")}})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test"}))
	if err == nil || !strings.Contains(err.Error(), "1 finding(s)") || !strings.Contains(err.Error(), "could not be determined") {
		t.Fatalf("expected both problems, got %v", err)
	}
}

func TestSecurityStepPassesWhenEveryCheckPassed(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 6, Passed: 6})
	if _, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.headers", Target: "https://api.example.test"})); err != nil {
		t.Fatalf("expected a pass, got %v", err)
	}
}

func TestSecurityFailOnSetsTheLowestSeverityThatFails(t *testing.T) {
	t.Parallel()
	report := scannerResult{Total: 4, Passed: 2, Failed: 2, Findings: []scannerFinding{finding("swagger", "low"), finding("debug", "medium")}}
	cases := []struct {
		failOn   string
		wantFail bool
		wantText string
	}{
		{"", true, "2 finding(s) at or above low"},
		{"low", true, "2 finding(s) at or above low"},
		{"medium", true, "1 finding(s) at or above medium"},
		{"high", false, ""},
		{"critical", false, ""},
	}
	for _, tc := range cases {
		stub := newScannerStub(t, report)
		log, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test", FailOn: tc.failOn}))
		if tc.wantFail {
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("fail_on=%q: expected %q, got %v", tc.failOn, tc.wantText, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("fail_on=%q: findings below the floor must not fail the step, got %v", tc.failOn, err)
		}
		if !log.has("warn", "Finding [swagger]") {
			t.Fatalf("fail_on=%q: a finding below the floor must still be shown as a warning: %v", tc.failOn, log.texts())
		}
	}
}

func TestSecurityFindingsAtOrAboveTheFloorAreLoggedAsErrors(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 2, Failed: 2, Passed: 0, Findings: []scannerFinding{finding("env-file", "critical"), finding("swagger", "low")}})
	log, _ := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test", FailOn: "high"}))
	if !log.has("error", "Finding [env-file]") || !log.has("warn", "Finding [swagger]") {
		t.Fatalf("expected the critical finding at error level and the low one at warn: %v", log.texts())
	}
}

// ---- the target -------------------------------------------------------------

func TestSecurityStepWithoutATargetScansTheSuitesMockByItsHost(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.headers"})
	step.MockHosts = []string{"shop.mock.internal", "other.mock.internal"}
	log, err := runSecurity(step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := stub.sent()
	if cfg.Target != "http://127.0.0.1:9080" || cfg.HostHeader != "shop.mock.internal" {
		t.Fatalf("the scan must go to the sidecar's listener with the mock host as Host, got target=%q host=%q", cfg.Target, cfg.HostHeader)
	}
	if !log.has("warn", "no target= given: scanning the suite's own mock at https://shop.mock.internal") {
		t.Fatalf("scanning the mock by default must be said once: %v", log.texts())
	}
	if log.has("warn", "harden=True") {
		t.Fatalf("an unhardened suite must not get the hardening warning: %v", log.texts())
	}
}

func TestSecurityStepOnAHardenedMockSaysTheGatewayDidIt(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.headers"})
	step.MockHosts = []string{"shop.mock.internal"}
	step.Hardened = true
	log, err := runSecurity(step)
	if err != nil {
		t.Fatal(err)
	}
	if !log.has("warn", "harden=True: the gateway adds security headers and a rate-limited route") {
		t.Fatalf("expected the hardening warning: %v", log.texts())
	}
}

func TestSecurityStepWithAMockHostTargetIsScannedByHostToo(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.cors", Target: "https://shop.mock.internal/api"})
	step.MockHosts = []string{"shop.mock.internal"}
	log, err := runSecurity(step)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := stub.sent(); cfg.Target != "http://127.0.0.1:9080/api" || cfg.HostHeader != "shop.mock.internal" {
		t.Fatalf("got target=%q host=%q", cfg.Target, cfg.HostHeader)
	}
	if log.has("warn", "no target= given") {
		t.Fatalf("an explicit target must not claim none was given: %v", log.texts())
	}
	if !log.has("warn", "scanning the suite's own mock") {
		t.Fatalf("scanning the mock explicitly must still say what it measures: %v", log.texts())
	}
}

func TestSecurityStepWithoutATargetOrAMockFails(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe"}))
	if err == nil || !strings.Contains(err.Error(), "no target= and the suite has no mock host") {
		t.Fatalf("expected a named failure, got %v", err)
	}
	if stub.calls != 0 {
		t.Fatal("nothing must be sent when there is nothing to scan")
	}
}

func TestSecurityTargetOnTheHostMachineIsReachedFromTheSidecar(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	if _, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.auth", Target: "http://localhost:8080"})); err != nil {
		t.Fatal(err)
	}
	if cfg := stub.sent(); cfg.Target != "http://host.docker.internal:8080" || cfg.HostHeader != "" {
		t.Fatalf("localhost inside the sidecar is the sidecar itself, got %q host=%q", cfg.Target, cfg.HostHeader)
	}
}

func TestSecurityTargetAFullyQualifiedHostIsUsedAsWritten(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	log, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.auth", Target: "https://api.staging.example.test"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg := stub.sent(); cfg.Target != "https://api.staging.example.test" || cfg.HostHeader != "" {
		t.Fatalf("got target=%q host=%q", cfg.Target, cfg.HostHeader)
	}
	if log.has("warn", "own mock") {
		t.Fatalf("an external target is not the mock: %v", log.texts())
	}
}

func TestSecurityTargetThatIsNoServiceOfTheSuiteIsRefused(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	_, err := runSecurity(securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "http://ghost:8000"}))
	if err == nil || !strings.Contains(err.Error(), `security step "scan"`) || !strings.Contains(err.Error(), "not a service of this suite") {
		t.Fatalf("expected the ghost host to be refused by a security-worded error, got %v", err)
	}
}

func TestSecurityTargetThatIsAServiceIsRefusedOnKubernetes(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "http://vuln:8000"})
	step.ServiceHosts = map[string]string{"vuln": "vuln"}
	step.BackendKind = "kubernetes"
	_, err := runSecurity(step)
	if err == nil || !strings.Contains(err.Error(), "a security scan of a service is supported only on the local Docker backend") {
		t.Fatalf("expected the Kubernetes refusal, got %v", err)
	}
}

// ---- the report -------------------------------------------------------------

func reportStep(stub *scannerStub, kind string, results *[]ArtifactResult) StepSpec {
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test"})
	step.ExecutionID = "run-security-report-" + strings.ReplaceAll(kind, "/", "-")
	step.BackendKind = kind
	step.ArtifactExports = []ArtifactExport{{Path: "/findings/probe.json", On: "always"}}
	step.OnArtifact = func(result ArtifactResult) { *results = append(*results, result) }
	return step
}

func TestSecurityStepExportsItsReportEvenWhenItFails(t *testing.T) {
	stub := newScannerStub(t, scannerResult{Total: 2, Passed: 1, Failed: 1, Findings: []scannerFinding{finding("admin-panel", "high")}})
	var results []ArtifactResult
	step := reportStep(stub, "local", &results)
	t.Cleanup(func() { _ = os.RemoveAll(ExecutionWorkspaceDir(step.ExecutionID)) })

	if _, err := runSecurity(step); err == nil {
		t.Fatal("the finding must fail the step")
	}
	if len(results) != 1 || len(results[0].Files) != 1 {
		t.Fatalf("the export must be delivered once with one file, got %+v", results)
	}
	var report securityReport
	if err := json.Unmarshal(results[0].Files[0].Content, &report); err != nil {
		t.Fatalf("the report must be JSON: %v", err)
	}
	if report.Target != "https://api.example.test" || report.Mode != "probe" || len(report.Findings) != 1 || report.Findings[0].Label != "admin-panel" {
		t.Fatalf("unexpected report %+v", report)
	}
}

func TestSecurityStepWritesItsReportIntoTheWorkspaceOnDocker(t *testing.T) {
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	var results []ArtifactResult
	step := reportStep(stub, "local", &results)
	t.Cleanup(func() { _ = os.RemoveAll(ExecutionWorkspaceDir(step.ExecutionID)) })

	if _, err := runSecurity(step); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ExecutionWorkspaceDir(step.ExecutionID) + "/findings/probe.json")
	if err != nil {
		t.Fatalf("the report must be in the workspace for a later step to read: %v", err)
	}
	if !strings.Contains(string(data), `"passed": 1`) {
		t.Fatalf("unexpected report file: %s", data)
	}
}

func TestSecurityStepDoesNotWriteTheWorkspaceOnKubernetesAndSaysSo(t *testing.T) {
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	var results []ArtifactResult
	step := reportStep(stub, "kubernetes", &results)
	t.Cleanup(func() { _ = os.RemoveAll(ExecutionWorkspaceDir(step.ExecutionID)) })

	log, err := runSecurity(step)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatal("the report is still an artifact there")
	}
	if _, statErr := os.Stat(ExecutionWorkspaceDir(step.ExecutionID) + "/findings/probe.json"); statErr == nil {
		t.Fatal("the control plane cannot write into a Kubernetes volume and must not pretend to")
	}
	if !log.has("warn", "not written to the execution workspace") {
		t.Fatalf("expected the warning: %v", log.texts())
	}
}

func TestWorkspaceFileRefusesAPathOutsideTheWorkspace(t *testing.T) {
	t.Parallel()
	// A path that climbs out is cleaned to stay inside, so it lands under the
	// workspace instead of beside it.
	id := "run-security-traversal"
	t.Cleanup(func() { _ = os.RemoveAll(ExecutionWorkspaceDir(id)) })
	if err := writeWorkspaceFile(id, "../../../escaped.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ExecutionWorkspaceDir(id) + "/escaped.json"); err != nil {
		t.Fatalf("expected the file inside the workspace: %v", err)
	}
	if err := writeWorkspaceFile(id, "/", []byte("{}")); err == nil {
		t.Fatal("the workspace root itself is not a file")
	}
}

func TestSecurityStepWithNoExportsDeliversNothing(t *testing.T) {
	t.Parallel()
	stub := newScannerStub(t, scannerResult{Total: 1, Passed: 1})
	called := false
	step := securityStep(stub, suites.SecuritySpec{Variant: "security.probe", Target: "https://api.example.test"})
	step.OnArtifact = func(ArtifactResult) { called = true }
	if _, err := runSecurity(step); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("no export is declared, so none is delivered")
	}
}

func TestSecurityFindingRanks(t *testing.T) {
	t.Parallel()
	for severity, want := range map[string]int{"info": 0, "low": 1, "MEDIUM": 2, "high": 3, "Critical": 4, "bogus": 2, "": 2} {
		if got := securityFindingRank(severity); got != want {
			t.Fatalf("%q ranks %d, want %d", severity, got, want)
		}
	}
	if securityFailFloor(&suites.SecuritySpec{}) != 1 || securityFailFloor(nil) != 1 {
		t.Fatal("no fail_on means any finding fails")
	}
}

// A sidecar that was just started refuses connections until its gateway is
// listening. The step must wait for it instead of failing the first time the
// port is closed (seen on a slow CI runner).
func TestSecurityStepWaitsForASidecarThatIsStillStarting(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close() // the port now refuses connections

	var scans atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(body)) == "{}" {
			http.Error(w, "target is required", http.StatusBadRequest)
			return
		}
		scans.Add(1)
		_ = json.NewEncoder(w).Encode(scannerResult{Total: 1, Passed: 1})
	})
	started := make(chan *http.Server, 1)
	go func() {
		time.Sleep(700 * time.Millisecond)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			started <- nil
			return
		}
		server := &http.Server{Handler: handler}
		go func() { _ = server.Serve(listener) }()
		started <- server
	}()

	step := StepSpec{
		ExecutionID: "run-security-test",
		Node:        StepNode{ID: "scan", Name: "scan", Kind: "security"},
		GatewayURL:  "http://" + address,
		Security:    &suites.SecuritySpec{Variant: "security.headers", Target: "https://example.com"},
	}
	_, runErr := runSecurity(step)
	if server := <-started; server != nil {
		defer server.Close()
	} else {
		t.Skip("the port was taken before the scanner could listen on it")
	}
	if runErr != nil {
		t.Fatalf("the step must wait for the scanner to start: %v", runErr)
	}
	if scans.Load() != 1 {
		t.Fatalf("the scan must be sent once, got %d", scans.Load())
	}
}

func TestWaitForScannerGivesUpWhenTheSidecarNeverServes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if err := waitForScanner(ctx, "http://"+address); err == nil {
		t.Fatal("a sidecar that never serves must be an error")
	}
}
