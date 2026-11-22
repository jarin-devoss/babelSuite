//go:build integration

package runner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// These tests run the real cannon, in a real APISIX container, against real HTTP
// servers on this machine. Run them with:
//
//	go test -tags integration ./internal/runner -run Integration -count=1
//
// They need Docker and the apache/apisix:3.9.0-debian image.
const integrationImage = "apache/apisix:3.9.0-debian"

// countingTarget is a real HTTP server on this machine that counts what reaches
// it, so a run's reported figures can be compared with what was really received.
type countingTarget struct {
	listener net.Listener
	hits     atomic.Int64
	byPath   sync.Map
}

func newCountingTarget(t *testing.T) *countingTarget {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	target := &countingTarget{listener: listener}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		target.hits.Add(1)
		counter, _ := target.byPath.LoadOrStore(r.URL.Path, new(atomic.Int64))
		counter.(*atomic.Int64).Add(1)

		switch {
		case strings.HasPrefix(r.URL.Path, "/status/"):
			code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
			w.WriteHeader(code)
		case r.URL.Path == "/slow":
			ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
			time.Sleep(time.Duration(ms) * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return target
}

// url is how the sidecar's container reaches this machine.
func (c *countingTarget) url() string {
	return fmt.Sprintf("http://host.docker.internal:%d", c.listener.Addr().(*net.TCPAddr).Port)
}

var (
	sidecarOnce   sync.Once
	sidecarClient *cannonClient
	sidecarErr    error
)

const (
	integrationSuite   = "it-cannon"
	integrationProfile = "local.yaml"
)

// TestMain removes the sidecar the tests share once they are all done.
func TestMain(m *testing.M) {
	code := m.Run()
	removeIntegrationContainers()
	os.Exit(code)
}

// startSidecar starts one real APISIX sidecar for all the tests: a start takes
// several seconds, and nothing a test does to it needs a fresh one.
func startSidecar(t *testing.T) *cannonClient {
	t.Helper()
	sidecarOnce.Do(func() {
		_ = exec.Command("docker", "rm", "-f", SidecarContainerName(integrationSuite, integrationProfile)).Run()
		if err := EnsureSuiteSidecar(apisix.SuiteConfig{ID: integrationSuite}, integrationProfile, integrationImage, ""); err != nil {
			sidecarErr = fmt.Errorf("start the sidecar: %w", err)
			return
		}
		base := SuiteSidecarURL(integrationSuite, integrationProfile)
		if base == "" {
			sidecarErr = fmt.Errorf("the sidecar is not published on a host port")
			return
		}
		client := newCannonClient(base)
		deadline := time.Now().Add(45 * time.Second)
		for {
			var probe map[string]any
			err := client.call(context.Background(), http.MethodGet, "status/none", nil, &probe)
			if err != nil && strings.Contains(err.Error(), "HTTP 404") {
				sidecarClient = client // the route answers: the cannon loaded
				return
			}
			if time.Now().After(deadline) {
				sidecarErr = fmt.Errorf("the cannon never answered: %w", err)
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	})
	if sidecarErr != nil {
		t.Fatal(sidecarErr)
	}
	return sidecarClient
}

func baseConfig(runID, target string, stages ...apisixStageConfig) apisixTrafficConfig {
	return apisixTrafficConfig{
		RunID: runID, Target: target, Model: "closed", MaxInFlight: 256, TimeoutMs: 5000,
		Stages: stages,
		Users: []apisixUserConfig{{
			Name: "u", Weight: 1, Wait: apisixWaitConfig{Mode: "constant"},
			Tasks: []apisixTaskConfig{{Sampler: "root", Weight: 1, Method: "GET", Path: "/"}},
		}},
	}
}

func runToEnd(t *testing.T, client *cannonClient, cfg apisixTrafficConfig) *apisixTrafficResult {
	t.Helper()
	ctx := context.Background()
	if err := client.start(ctx, cfg); err != nil {
		t.Fatalf("start %s: %v", cfg.RunID, err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, err := client.status(ctx, cfg.RunID)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if status.State != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish", cfg.RunID)
		}
		time.Sleep(200 * time.Millisecond)
	}
	result, err := client.result(ctx, cfg.RunID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	return result
}

func total(result *apisixTrafficResult) (count, failures int) {
	for _, sampler := range result.Samplers {
		count += sampler.Count
		failures += sampler.Failures
	}
	return
}

func TestIntegrationCannonCountsMatchWhatTheTargetReceived(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)

	result := runToEnd(t, client, baseConfig("it-count", target.url(), apisixStageConfig{Users: 2, SpawnRate: 2, DurationS: 3}))

	count, failures := total(result)
	if count == 0 {
		t.Fatal("the cannon reported no requests")
	}
	if int64(count) != target.hits.Load() {
		t.Fatalf("the cannon reported %d requests but the target received %d", count, target.hits.Load())
	}
	if failures != 0 {
		t.Errorf("a target answering 200 must have no failures, got %d (%v)", failures, result.Samplers["root"].Errors)
	}
	if result.PeakUsers != 2 {
		t.Errorf("peak users = %d, want 2", result.PeakUsers)
	}
	var buckets int
	for _, n := range result.Samplers["root"].Buckets {
		buckets += n
	}
	if buckets != count {
		t.Errorf("the latency buckets hold %d requests, the counter says %d", buckets, count)
	}
}

func TestIntegrationA404IsAFailureUnlessExpected(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)

	cfg := baseConfig("it-404", target.url(), apisixStageConfig{Users: 1, SpawnRate: 1, DurationS: 2})
	cfg.Users[0].Tasks[0].Path = "/status/404"
	count, failures := total(runToEnd(t, client, cfg))
	if count == 0 || failures != count {
		t.Fatalf("every 404 must be a failure by default: %d requests, %d failures", count, failures)
	}

	expected := baseConfig("it-404-expected", target.url(), apisixStageConfig{Users: 1, SpawnRate: 1, DurationS: 2})
	expected.Users[0].Tasks[0].Path = "/status/404"
	expected.Users[0].Tasks[0].ExpectStatus = []int{404}
	count, failures = total(runToEnd(t, client, expected))
	if count == 0 || failures != 0 {
		t.Fatalf("a task that expects 404 must not fail on it: %d requests, %d failures", count, failures)
	}
}

func TestIntegrationUnreachableTargetFailsEveryRequest(t *testing.T) {
	client := startSidecar(t)
	// Nothing listens here: the connection is refused at once.
	result := runToEnd(t, client, baseConfig("it-dead", "http://host.docker.internal:1", apisixStageConfig{Users: 1, SpawnRate: 1, DurationS: 2}))
	count, failures := total(result)
	if count == 0 || failures != count {
		t.Fatalf("every request to a dead target must fail: %d requests, %d failures", count, failures)
	}
	if len(result.Samplers["root"].Errors) == 0 {
		t.Error("the failures must be categorised")
	}
}

func TestIntegrationConcurrentRunsDoNotSeeEachOther(t *testing.T) {
	client := startSidecar(t)
	first, second := newCountingTarget(t), newCountingTarget(t)

	var wg sync.WaitGroup
	results := make([]*apisixTrafficResult, 2)
	for index, target := range []*countingTarget{first, second} {
		wg.Add(1)
		go func(index int, target *countingTarget) {
			defer wg.Done()
			results[index] = runToEnd(t, client, baseConfig(fmt.Sprintf("it-iso-%d", index), target.url(), apisixStageConfig{Users: 2, SpawnRate: 2, DurationS: 3}))
		}(index, target)
	}
	wg.Wait()

	for index, target := range []*countingTarget{first, second} {
		count, _ := total(results[index])
		if count == 0 || int64(count) != target.hits.Load() {
			t.Errorf("run %d reported %d requests, its own target received %d", index, count, target.hits.Load())
		}
	}
}

func TestIntegrationStopEndsTheLoadPromptly(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)
	ctx := context.Background()

	cfg := baseConfig("it-stop", target.url(), apisixStageConfig{Users: 2, SpawnRate: 2, DurationS: 60})
	if err := client.start(ctx, cfg); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(2 * time.Second)
	if err := client.stop(ctx, cfg.RunID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	stoppedAt := time.Now()

	deadline := time.Now().Add(10 * time.Second)
	for {
		status, err := client.status(ctx, cfg.RunID)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if status.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run is still %q ten seconds after stop", status.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if took := time.Since(stoppedAt); took > 3*time.Second {
		t.Errorf("stop took %s, want under 3s", took)
	}

	settled := target.hits.Load()
	time.Sleep(2 * time.Second)
	if target.hits.Load() != settled {
		t.Errorf("the target kept receiving requests after the run stopped (%d -> %d)", settled, target.hits.Load())
	}
}

func TestIntegrationOpenModelKeepsItsRateWhateverTheLatency(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)

	cfg := baseConfig("it-open", target.url(), apisixStageConfig{DurationS: 5})
	cfg.Model, cfg.Rate = "open", 20
	cfg.Users[0].Tasks[0].Path = "/slow?ms=400"
	count, _ := total(runToEnd(t, client, cfg))

	// 20 arrivals a second for 5 seconds is 100, even though each takes 400 ms.
	if count < 85 || count > 115 {
		t.Fatalf("an open model at 20/s for 5s should send about 100 requests, sent %d", count)
	}
}

func TestIntegrationOpenModelCountsArrivalsItCannotStart(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)

	cfg := baseConfig("it-drop", target.url(), apisixStageConfig{DurationS: 4})
	cfg.Model, cfg.Rate, cfg.MaxInFlight = "open", 20, 2
	cfg.Users[0].Tasks[0].Path = "/slow?ms=1000"
	result := runToEnd(t, client, cfg)
	if result.Dropped == 0 {
		t.Fatal("with 2 in flight and 1 s responses at 20/s, arrivals must be dropped")
	}
}

func TestIntegrationWeightsAndSamplersAreHonoured(t *testing.T) {
	client := startSidecar(t)
	target := newCountingTarget(t)

	cfg := baseConfig("it-weights", target.url(), apisixStageConfig{Users: 4, SpawnRate: 4, DurationS: 4})
	cfg.Users[0].Tasks = []apisixTaskConfig{
		{Sampler: "common", Weight: 3, Method: "GET", Path: "/common"},
		{Sampler: "rare", Weight: 1, Method: "GET", Path: "/rare"},
	}
	result := runToEnd(t, client, cfg)

	common, rare := result.Samplers["common"], result.Samplers["rare"]
	if common == nil || rare == nil || common.Count+rare.Count < 40 {
		t.Fatalf("both samplers must have data: %+v", result.Samplers)
	}
	ratio := float64(common.Count) / float64(rare.Count)
	if ratio < 2.0 || ratio > 4.5 {
		t.Errorf("a 3:1 weighting gave %d:%d (%.2f)", common.Count, rare.Count, ratio)
	}
}

func TestIntegrationFullStepAgainstARealTarget(t *testing.T) {
	startSidecar(t)
	target := newCountingTarget(t)

	step := StepSpec{
		ExecutionID: "it-full", SuiteID: "it-cannon", Profile: "local.yaml",
		GatewayURL: SuiteSidecarURL(integrationSuite, integrationProfile),
		Node:       StepNode{ID: "load", Name: "load", Kind: "traffic", Variant: "traffic.baseline"},
		Load: &suites.LoadSpec{
			Variant: "traffic.baseline", Target: target.url(),
			Users: []suites.LoadUser{{Name: "u", Weight: 1, Wait: suites.LoadWait{Mode: "constant"},
				Tasks: []suites.LoadTask{{Name: "root", Weight: 1, Request: suites.LoadRequest{Method: "GET", Path: "/", Name: "root"}}}}},
			Stages: []suites.LoadStage{{Users: 2, SpawnRate: 2, Duration: 3 * time.Second}},
		},
	}
	var lines []string
	if err := executeLoadStep(context.Background(), step, func(l logstream.Line) { lines = append(lines, l.Text) }); err != nil {
		t.Fatalf("step: %v\n%s", err, joined(lines))
	}
	if target.hits.Load() == 0 {
		t.Fatal("the target received nothing")
	}
	if !strings.Contains(joined(lines), fmt.Sprintf("with %d requests", target.hits.Load())) {
		t.Errorf("the log should report the %d requests the target received:\n%s", target.hits.Load(), joined(lines))
	}
}
