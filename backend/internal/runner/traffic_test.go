package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// fakeCannon speaks the traffic cannon's protocol the way the sidecar does, with
// the counts a test chooses, so the control plane's side can be checked exactly.
type fakeCannon struct {
	mu      sync.Mutex
	started []apisixTrafficConfig
	stopped []string
	polls   int

	// resultFor returns the counts of a run; statusFor the progress at a poll.
	resultFor func(runID string) *apisixTrafficResult
	statusFor func(poll int) apisixStatus
}

func (f *fakeCannon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, apisix.TrafficCannonRoutePrefix)
	reply := func(code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case path == "start" && r.Method == http.MethodPost:
		var cfg apisixTrafficConfig
		_ = json.NewDecoder(r.Body).Decode(&cfg)
		f.started = append(f.started, cfg)
		reply(http.StatusAccepted, map[string]string{"run_id": cfg.RunID})
	case strings.HasPrefix(path, "status/"):
		f.polls++
		status := apisixStatus{State: "done"}
		if f.statusFor != nil {
			status = f.statusFor(f.polls)
		}
		reply(http.StatusOK, status)
	case strings.HasPrefix(path, "result/"):
		result := f.resultFor(strings.TrimPrefix(path, "result/"))
		reply(http.StatusOK, result)
	case strings.HasPrefix(path, "stop/"):
		f.stopped = append(f.stopped, strings.TrimPrefix(path, "stop/"))
		reply(http.StatusOK, map[string]bool{"stopping": true})
	default:
		reply(http.StatusNotFound, map[string]string{"error": "unknown"})
	}
}

func fastPolling(t *testing.T) {
	t.Helper()
	poll, progress, after := cannonPollInterval, cannonProgressInterval, cannonDeadTargetAfter
	cannonPollInterval, cannonProgressInterval, cannonDeadTargetAfter = 5*time.Millisecond, time.Hour, time.Millisecond
	t.Cleanup(func() { cannonPollInterval, cannonProgressInterval, cannonDeadTargetAfter = poll, progress, after })
}

// A hundred requests: 40 at 10 ms, 40 at 20 ms, 15 at 50 ms and 5 at 120 ms.
func healthResult(failures int) *apisixTrafficResult {
	return &apisixTrafficResult{
		State: "done", StartedAt: 1000, FinishedAt: 1010, PeakUsers: 3,
		Samplers: map[string]*apisixSamplerResult{
			"health": {
				Count: 100, Failures: failures, SumMs: 5000, MinMs: 10, MaxMs: 120,
				Buckets: map[string]int{"10": 40, "20": 40, "50": 15, "120": 5},
				Errors:  map[string]int{},
			},
		},
		Stages: []apisixStageResult{{
			Index: 0, Count: 100, Failures: failures, SumMs: 5000, StartedAt: 1000, FinishedAt: 1010,
			Buckets: map[string]int{"10": 40, "20": 40, "50": 15, "120": 5},
		}},
		PerSecond: map[string]int{"0": 10, "1": 10},
	}
}

func trafficSpec(variant string, stages ...suites.LoadStage) *suites.LoadSpec {
	if len(stages) == 0 {
		stages = []suites.LoadStage{{Users: 3, SpawnRate: 3, Duration: 10 * time.Second}}
	}
	return &suites.LoadSpec{
		Variant: variant,
		Target:  "http://api.example.com",
		Users: []suites.LoadUser{{
			Name: "probe", Weight: 1, Wait: suites.LoadWait{Mode: "constant"},
			Tasks: []suites.LoadTask{{
				Name: "health", Weight: 1,
				Request: suites.LoadRequest{Method: "GET", Path: "/health", Name: "health"},
			}},
		}},
		Stages: stages,
	}
}

func trafficStep(gateway string, spec *suites.LoadSpec) StepSpec {
	return StepSpec{
		ExecutionID: "run-1", SuiteID: "suite", Profile: "local.yaml", GatewayURL: gateway, Load: spec,
		Node: StepNode{ID: "load", Name: "load", Kind: "traffic", Variant: spec.Variant},
	}
}

func runTraffic(t *testing.T, step StepSpec) ([]string, error) {
	t.Helper()
	var lines []string
	err := executeLoadStep(context.Background(), step, func(l logstream.Line) { lines = append(lines, l.Text) })
	return lines, err
}

func joined(lines []string) string { return strings.Join(lines, "\n") }

func TestTrafficStepWithoutLoadSpecFails(t *testing.T) {
	step := StepSpec{GatewayURL: "http://sidecar", Node: StepNode{Name: "promo", Kind: "traffic"}}
	_, err := runTraffic(t, step)
	if err == nil || !strings.Contains(err.Error(), "no load spec") {
		t.Fatalf("want an error about the missing load spec, got %v", err)
	}
}

func TestTrafficStepWithoutSidecarFails(t *testing.T) {
	_, err := runTraffic(t, trafficStep("", trafficSpec("traffic.baseline")))
	if err == nil || !strings.Contains(err.Error(), "sidecar is not running") {
		t.Fatalf("want an error that the sidecar is not running, got %v", err)
	}
}

// Every figure in the log is derived from the cannon's counts, not rebuilt from
// five summary numbers: the percentiles, the average and the histogram of a
// known hundred-request run come out exactly.
func TestTrafficRunReportsTheCannonsCountsExactly(t *testing.T) {
	fastPolling(t)
	cannon := &fakeCannon{resultFor: func(string) *apisixTrafficResult { return healthResult(0) }}
	server := httptest.NewServer(cannon)
	defer server.Close()

	lines, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.baseline")))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	log := joined(lines)
	for _, want := range []string{
		"completed with 100 requests, 0 failures, and peak concurrency 3",
		"avg=50.0ms min=10.0ms max=120.0ms p50=20.0ms p90=50.0ms p95=50.0ms p99=120.0ms",
		"<=10ms=40",
		"<=25ms=40",
		"<=50ms=15",
		"<=250ms=5",
		"Sampler health",
		"count=100 failures=0",
		"Stage 1 summary users=3",
		"requests=100",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if len(cannon.started) != 1 {
		t.Fatalf("want one start, got %d", len(cannon.started))
	}
	started := cannon.started[0]
	if started.Target != "http://api.example.com" || started.Model != "closed" || len(started.Stages) != 1 {
		t.Errorf("unexpected configuration sent: %+v", started)
	}
}

func TestTrafficRunThatSentNothingFails(t *testing.T) {
	fastPolling(t)
	empty := &apisixTrafficResult{State: "done", Samplers: map[string]*apisixSamplerResult{}, Stages: []apisixStageResult{}}
	server := httptest.NewServer(&fakeCannon{resultFor: func(string) *apisixTrafficResult { return empty }})
	defer server.Close()

	_, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.baseline")))
	if err == nil || !strings.Contains(err.Error(), "no requests were sent") {
		t.Fatalf("a run that sent nothing must fail, got %v", err)
	}
}

func TestTrafficFailedRequestsFailTheStepAndNameTheErrors(t *testing.T) {
	fastPolling(t)
	result := healthResult(10)
	result.Samplers["health"].Errors = map[string]int{"http_4xx:404": 10}
	server := httptest.NewServer(&fakeCannon{resultFor: func(string) *apisixTrafficResult { return result }})
	defer server.Close()

	_, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.baseline")))
	if err == nil {
		t.Fatal("a run with failed requests must fail the default error-rate threshold")
	}
	for _, want := range []string{"http.error_rate", "http_4xx:404 x10"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestTrafficPerTaskThresholdIsCheckedAgainstItsOwnSampler(t *testing.T) {
	fastPolling(t)
	server := httptest.NewServer(&fakeCannon{resultFor: func(string) *apisixTrafficResult { return healthResult(0) }})
	defer server.Close()

	passing := trafficSpec("traffic.baseline")
	passing.Users[0].Tasks[0].Checks = []suites.LoadThreshold{{Metric: "latency.p95_ms", Op: "<", Value: 100, Sampler: "health"}}
	if _, err := runTraffic(t, trafficStep(server.URL, passing)); err != nil {
		t.Fatalf("p95 of 50ms is under 100ms, got %v", err)
	}

	failing := trafficSpec("traffic.baseline")
	failing.Users[0].Tasks[0].Checks = []suites.LoadThreshold{{Metric: "latency.p95_ms", Op: "<", Value: 40, Sampler: "health"}}
	_, err := runTraffic(t, trafficStep(server.URL, failing))
	if err == nil || !strings.Contains(err.Error(), "latency.p95_ms") || !strings.Contains(err.Error(), "got 50.00") {
		t.Fatalf("p95 of 50ms is over 40ms and the error should say so, got %v", err)
	}

	missing := trafficSpec("traffic.baseline")
	missing.Users[0].Tasks[0].Checks = []suites.LoadThreshold{{Metric: "latency.p95_ms", Op: "<", Value: 100, Sampler: "no-such-sampler"}}
	_, err = runTraffic(t, trafficStep(server.URL, missing))
	if err == nil || !strings.Contains(err.Error(), "no-such-sampler sent no requests") {
		t.Fatalf("a check on a sampler that sent nothing must say so, got %v", err)
	}
}

func TestTrafficCancelStopsTheRunInTheSidecar(t *testing.T) {
	fastPolling(t)
	cannon := &fakeCannon{
		resultFor: func(string) *apisixTrafficResult { return healthResult(0) },
		statusFor: func(int) apisixStatus { return apisixStatus{State: "running", Requests: 1} },
	}
	server := httptest.NewServer(cannon)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := executeLoadStep(ctx, trafficStep(server.URL, trafficSpec("traffic.baseline")), func(logstream.Line) {})
	if err != context.Canceled {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	cannon.mu.Lock()
	defer cannon.mu.Unlock()
	if len(cannon.stopped) != 1 || len(cannon.started) != 1 || cannon.stopped[0] != cannon.started[0].RunID {
		t.Fatalf("the run must be stopped in the sidecar: started %v, stopped %v", cannon.started, cannon.stopped)
	}
}

func TestTrafficTargetThatFailsEveryRequestStopsEarly(t *testing.T) {
	fastPolling(t)
	dead := healthResult(100)
	dead.Samplers["health"].Errors = map[string]int{"connect": 100}
	cannon := &fakeCannon{
		resultFor: func(string) *apisixTrafficResult { return dead },
		statusFor: func(int) apisixStatus { return apisixStatus{State: "running", Requests: 20, Failures: 20} },
	}
	server := httptest.NewServer(cannon)
	defer server.Close()
	time.Sleep(5 * time.Millisecond)

	_, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.baseline")))
	if err == nil || !strings.Contains(err.Error(), "did not answer a single request successfully") || !strings.Contains(err.Error(), "connect x100") {
		t.Fatalf("want the dead-target error naming the failure, got %v", err)
	}
	if len(cannon.stopped) != 1 {
		t.Fatalf("the doomed run should have been stopped, stopped=%v", cannon.stopped)
	}
}

// Every probe is a real run. Probes 1 and 2 pass, probe 3 breaks the error-rate
// threshold, so the step reports the last passing user count and still passes.
func scalabilityCannon(brokenFrom int) *fakeCannon {
	return &fakeCannon{resultFor: func(runID string) *apisixTrafficResult {
		probe := 0
		if index := strings.LastIndex(runID, "-probe-"); index >= 0 {
			probe, _ = strconv.Atoi(runID[index+len("-probe-"):])
		}
		failures := 0
		if brokenFrom > 0 && probe >= brokenFrom {
			failures = 50
		}
		return healthResult(failures)
	}}
}

func scalabilityStages() []suites.LoadStage {
	return []suites.LoadStage{
		{Users: 2, SpawnRate: 2, Duration: time.Second},
		{Users: 4, SpawnRate: 4, Duration: time.Second},
		{Users: 8, SpawnRate: 8, Duration: time.Second},
	}
}

func TestScalabilityReportsTheLastPassingProbeAsTheMaximum(t *testing.T) {
	fastPolling(t)
	cannon := scalabilityCannon(3)
	server := httptest.NewServer(cannon)
	defer server.Close()

	lines, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.scalability", scalabilityStages()...)))
	if err != nil {
		t.Fatalf("a breaking point after two passing probes is a result, not a failure: %v", err)
	}
	log := joined(lines)
	if !strings.Contains(log, "Max sustainable load: 4 users") || !strings.Contains(log, "Breaking point found at 8 users") {
		t.Errorf("log lacks the maximum and the breaking point:\n%s", log)
	}
	if len(cannon.started) != 3 {
		t.Errorf("want three real probes, got %d", len(cannon.started))
	}
}

func TestScalabilityFailsWhenNothingIsSustainable(t *testing.T) {
	fastPolling(t)
	server := httptest.NewServer(scalabilityCannon(1))
	defer server.Close()

	_, err := runTraffic(t, trafficStep(server.URL, trafficSpec("traffic.scalability", scalabilityStages()...)))
	if err == nil || !strings.Contains(err.Error(), "nothing is sustainable") {
		t.Fatalf("a first probe that already breaks must fail the step, got %v", err)
	}
}

func TestScalabilityFailsBelowMinUsers(t *testing.T) {
	fastPolling(t)
	server := httptest.NewServer(scalabilityCannon(3))
	defer server.Close()

	spec := trafficSpec("traffic.scalability", scalabilityStages()...)
	spec.MinUsers = 8
	_, err := runTraffic(t, trafficStep(server.URL, spec))
	if err == nil || !strings.Contains(err.Error(), "below the min_users=8") {
		t.Fatalf("sustaining 4 users against min_users=8 must fail, got %v", err)
	}
}

func TestBuildConfigWeightsWaitsAndExpectedStatuses(t *testing.T) {
	spec := trafficSpec("traffic.stress")
	spec.Users = []suites.LoadUser{{
		Name: "shopper", Weight: 3, Wait: suites.LoadWait{Mode: "between", MinSeconds: 1, MaxSeconds: 2},
		Tasks: []suites.LoadTask{
			{Name: "browse", Weight: 4, Request: suites.LoadRequest{Method: "GET", Path: "/items", Name: "browse"}},
			{Name: "gone", Weight: 1, Request: suites.LoadRequest{Method: "GET", Path: "/old", Name: "gone"},
				Checks: []suites.LoadThreshold{{Metric: "status", Op: "==", Value: 404}}},
		},
	}}
	cfg := buildAPISIXConfig(spec, "id", resolvedTarget{URL: "http://t", HostHeader: "h"}, spec.Stages)

	if cfg.Model != "closed" || cfg.HostHeader != "h" || cfg.Users[0].Weight != 3 || cfg.Users[0].Wait.Mode != "between" || cfg.Users[0].Wait.MaxS != 2 {
		t.Errorf("unexpected user configuration: %+v", cfg)
	}
	browse, gone := cfg.Users[0].Tasks[0], cfg.Users[0].Tasks[1]
	if browse.Weight != 4 || browse.Sampler != "browse" || len(browse.ExpectStatus) != 0 {
		t.Errorf("browse task: %+v", browse)
	}
	if len(gone.ExpectStatus) != 1 || gone.ExpectStatus[0] != 404 {
		t.Errorf("a status == 404 check must become the expected status, got %+v", gone)
	}
}

func TestBuildConfigRunsARateAsAnOpenModel(t *testing.T) {
	spec := trafficSpec("traffic.stress")
	spec.RequestsPerS = 40
	if cfg := buildAPISIXConfig(spec, "id", resolvedTarget{URL: "http://t"}, spec.Stages); cfg.Model != "open" || cfg.Rate != 40 {
		t.Errorf("rps= on a closed variant must run as an open model at that rate, got %+v", cfg)
	}

	open := trafficSpec("traffic.open_model")
	open.ArrivalRate = 12
	if cfg := buildAPISIXConfig(open, "id", resolvedTarget{URL: "http://t"}, open.Stages); cfg.Model != "open" || cfg.Rate != 12 {
		t.Errorf("open_model must use its arrival rate, got %+v", cfg)
	}
}

func TestResolveTrafficTarget(t *testing.T) {
	step := func(target string) StepSpec {
		return StepSpec{
			Load:      &suites.LoadSpec{Target: target},
			Node:      StepNode{Name: "load"},
			MockHosts: []string{"payment-suite.mock.internal"},
		}
	}

	cases := []struct {
		name, target       string
		wantURL, wantHost  string
		wantErrContaining  string
		backendKind, svc   string
		serviceHostsToNode map[string]string
	}{
		{name: "mock host is served by the sidecar", target: "https://payment-suite.mock.internal/v1", wantURL: "http://127.0.0.1:9080/v1", wantHost: "payment-suite.mock.internal"},
		{name: "localhost becomes the host", target: "http://localhost:8080", wantURL: "http://host.docker.internal:8080"},
		{name: "loopback address becomes the host", target: "http://127.0.0.1:9000/x", wantURL: "http://host.docker.internal:9000/x"},
		{name: "a fully-qualified host is used as written", target: "https://api.staging.example.com:8443/base", wantURL: "https://api.staging.example.com:8443/base"},
		{name: "an address is used as written", target: "http://10.1.2.3:80", wantURL: "http://10.1.2.3:80"},
		{name: "an unknown bare name is refused", target: "http://ghost:8080", wantErrContaining: `"ghost" is not a service of this suite`},
		{name: "a service on kubernetes is refused, not guessed", target: "http://api:8080", backendKind: "kubernetes",
			serviceHostsToNode: map[string]string{"api": "api-node"}, wantErrContaining: "only on the local Docker backend"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := step(tc.target)
			spec.BackendKind = tc.backendKind
			spec.ServiceHosts = tc.serviceHostsToNode
			got, err := resolveTrafficTarget(context.Background(), spec)
			if tc.wantErrContaining != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrContaining) {
					t.Fatalf("want an error containing %q, got %v", tc.wantErrContaining, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got.URL != tc.wantURL || got.HostHeader != tc.wantHost {
				t.Errorf("got %q (host %q), want %q (host %q)", got.URL, got.HostHeader, tc.wantURL, tc.wantHost)
			}
		})
	}
}

func TestSidecarConfigHashChangesWithAnyInput(t *testing.T) {
	base := sidecarConfigHash("img", "routes", map[string]string{"a.lua": "one"})
	if base != sidecarConfigHash("img", "routes", map[string]string{"a.lua": "one"}) {
		t.Fatal("the hash must be stable")
	}
	for name, other := range map[string]string{
		"image":   sidecarConfigHash("img2", "routes", map[string]string{"a.lua": "one"}),
		"routes":  sidecarConfigHash("img", "routes2", map[string]string{"a.lua": "one"}),
		"plugin":  sidecarConfigHash("img", "routes", map[string]string{"a.lua": "two"}),
		"added":   sidecarConfigHash("img", "routes", map[string]string{"a.lua": "one", "b.lua": ""}),
		"renamed": sidecarConfigHash("img", "routes", map[string]string{"b.lua": "one"}),
	} {
		if other == base {
			t.Errorf("changing the %s must change the hash", name)
		}
	}
}
