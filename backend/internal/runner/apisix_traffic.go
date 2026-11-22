package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// Variables, not constants, so tests can run a whole load step in milliseconds.
var (
	cannonPollInterval     = 2 * time.Second
	cannonProgressInterval = 10 * time.Second
	// A run whose every request failed after this long is not going to recover.
	cannonDeadTargetAfter = 3 * time.Second
	cannonDeadTargetMin   = 5
)

// apisixTrafficConfig is the run configuration sent to the cannon. It carries
// the plan, not any statistic: the cannon only counts.
type apisixTrafficConfig struct {
	RunID       string              `json:"run_id"`
	Target      string              `json:"target"`
	HostHeader  string              `json:"host_header,omitempty"`
	Model       string              `json:"model"`
	Rate        float64             `json:"rate,omitempty"`
	MaxInFlight int                 `json:"max_in_flight,omitempty"`
	TimeoutMs   int                 `json:"timeout_ms,omitempty"`
	Stages      []apisixStageConfig `json:"stages"`
	Users       []apisixUserConfig  `json:"users"`
}

type apisixStageConfig struct {
	// Users is always sent: a stop stage ramps down to zero users, and a missing
	// value would read as one.
	Users     int     `json:"users"`
	SpawnRate float64 `json:"spawn_rate,omitempty"`
	DurationS float64 `json:"duration_s"`
	Rate      float64 `json:"rate,omitempty"`
	Stop      bool    `json:"stop,omitempty"`
}

type apisixWaitConfig struct {
	Mode string  `json:"mode,omitempty"`
	S    float64 `json:"s,omitempty"`
	MinS float64 `json:"min_s,omitempty"`
	MaxS float64 `json:"max_s,omitempty"`
}

type apisixUserConfig struct {
	Name   string             `json:"name"`
	Weight int                `json:"weight"`
	Wait   apisixWaitConfig   `json:"wait"`
	Tasks  []apisixTaskConfig `json:"tasks"`
}

type apisixTaskConfig struct {
	Sampler      string            `json:"sampler"`
	Weight       int               `json:"weight"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Body         string            `json:"body,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	ExpectStatus []int             `json:"expect_status,omitempty"`
}

// apisixStatus is the cannon's progress report for a run.
type apisixStatus struct {
	State        string  `json:"state"`
	ElapsedS     float64 `json:"elapsed_s"`
	ActiveUsers  int     `json:"active_users"`
	Requests     int     `json:"requests"`
	Failures     int     `json:"failures"`
	Dropped      int     `json:"dropped"`
	DictFreeByte int64   `json:"dict_free_bytes"`
}

// apisixSamplerResult is everything the cannon counted for one sampler.
type apisixSamplerResult struct {
	Count    int            `json:"count"`
	Failures int            `json:"failures"`
	SumMs    float64        `json:"sum_ms"`
	MinMs    float64        `json:"min_ms"`
	MaxMs    float64        `json:"max_ms"`
	Buckets  map[string]int `json:"buckets"`
	Errors   map[string]int `json:"errors"`
}

type apisixStageResult struct {
	Index      int            `json:"index"`
	Count      int            `json:"count"`
	Failures   int            `json:"failures"`
	SumMs      float64        `json:"sum_ms"`
	StartedAt  float64        `json:"started_at"`
	FinishedAt float64        `json:"finished_at"`
	Buckets    map[string]int `json:"buckets"`
}

// apisixTrafficResult is the cannon's final report: exact counts and latency
// buckets, from which the control plane derives every statistic.
type apisixTrafficResult struct {
	State      string                          `json:"state"`
	Error      string                          `json:"error"`
	StartedAt  float64                         `json:"started_at"`
	FinishedAt float64                         `json:"finished_at"`
	PeakUsers  int                             `json:"peak_users"`
	Dropped    int                             `json:"dropped"`
	Samplers   map[string]*apisixSamplerResult `json:"samplers"`
	Stages     []apisixStageResult             `json:"stages"`
	PerSecond  map[string]int                  `json:"per_second"`
}

// canUseAPISIXTraffic reports whether the step has a sidecar to run the load in.
func canUseAPISIXTraffic(step StepSpec) bool {
	return strings.TrimSpace(step.GatewayURL) != ""
}

// cannonClient talks to the cannon inside a suite's sidecar.
type cannonClient struct {
	base string
	http *http.Client
}

func newCannonClient(gatewayURL string) *cannonClient {
	return &cannonClient{
		base: strings.TrimSuffix(strings.TrimSpace(gatewayURL), "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *cannonClient) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+apisix.TrafficCannonRoutePrefix+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("sidecar answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func (c *cannonClient) start(ctx context.Context, cfg apisixTrafficConfig) error {
	return c.call(ctx, http.MethodPost, "start", cfg, nil)
}

func (c *cannonClient) status(ctx context.Context, id string) (apisixStatus, error) {
	var out apisixStatus
	err := c.call(ctx, http.MethodGet, "status/"+id, nil, &out)
	return out, err
}

func (c *cannonClient) result(ctx context.Context, id string) (*apisixTrafficResult, error) {
	var out apisixTrafficResult
	if err := c.call(ctx, http.MethodGet, "result/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *cannonClient) stop(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodPost, "stop/"+id, nil, nil)
}

// runAPISIXTraffic runs one load run in the suite's sidecar and merges its
// counts into stats:
//  1. translate the plan into the cannon's configuration;
//  2. start the run, which answers at once;
//  3. poll its progress, logging it, until it ends;
//  4. fetch the counts and merge them.
//
// A cancelled step stops the run in the sidecar instead of leaving it sending.
func runAPISIXTraffic(ctx context.Context, step StepSpec, emit func(logstream.Line), stats *loadStats, target resolvedTarget, stages []suites.LoadStage, runID string) error {
	release, err := acquireSidecarLoad(ctx, step, emit)
	if err != nil {
		return err
	}
	defer release()

	client := newCannonClient(step.GatewayURL)
	cfg := buildAPISIXConfig(step.Load, runID, target, stages)

	emit(line(step, "info", fmt.Sprintf("[%s] Starting the traffic cannon in the sidecar: model=%s stages=%d target=%s.", step.Node.Name, cfg.Model, len(cfg.Stages), cfg.Target)))
	if err := client.start(ctx, cfg); err != nil {
		return fmt.Errorf("apisix traffic: start failed: %w", err)
	}

	stopRun := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.stop(stopCtx, runID)
	}

	ticker := time.NewTicker(cannonPollInterval)
	defer ticker.Stop()
	began := time.Now()
	lastProgress := began

	for {
		select {
		case <-ctx.Done():
			stopRun()
			return ctx.Err()
		case <-ticker.C:
		}

		status, err := client.status(ctx, runID)
		if err != nil {
			if ctx.Err() != nil {
				stopRun()
				return ctx.Err()
			}
			return fmt.Errorf("apisix traffic: status failed: %w", err)
		}

		if time.Since(lastProgress) >= cannonProgressInterval {
			lastProgress = time.Now()
			rps := 0.0
			if status.ElapsedS > 0 {
				rps = float64(status.Requests) / status.ElapsedS
			}
			emit(line(step, "info", fmt.Sprintf("[%s] t+%.0fs users=%d requests=%d failures=%d dropped=%d rps=%.1f.", step.Node.Name, status.ElapsedS, status.ActiveUsers, status.Requests, status.Failures, status.Dropped, rps)))
		}

		// A target that fails every request is not going to recover; running the
		// rest of the plan against it only wastes the time the plan was given.
		if status.State == "running" && time.Since(began) >= cannonDeadTargetAfter && status.Requests >= cannonDeadTargetMin && status.Failures == status.Requests {
			stopRun()
			final, err := waitForCannonResult(ctx, client, runID)
			if err != nil {
				return fmt.Errorf("apisix traffic: every request failed and the run could not be collected: %w", err)
			}
			applyAPISIXResult(final, stats, stages)
			return fmt.Errorf("target %s did not answer a single request successfully (%d of %d failed): %s", cfg.Target, status.Failures, status.Requests, topErrors(final, 3))
		}

		if status.State != "running" {
			break
		}
	}

	result, err := client.result(ctx, runID)
	if err != nil {
		return fmt.Errorf("apisix traffic: result failed: %w", err)
	}
	if result.State == "error" {
		return fmt.Errorf("apisix traffic: the cannon failed: %s", result.Error)
	}
	applyAPISIXResult(result, stats, stages)

	emit(line(step, "info", fmt.Sprintf("[%s] The cannon finished: state=%s requests=%d failures=%d dropped=%d.", step.Node.Name, result.State, stats.Requests, stats.Failures, stats.Dropped)))
	if errs := topErrors(result, 5); errs != "" {
		emit(line(step, "warn", fmt.Sprintf("[%s] Most frequent errors: %s.", step.Node.Name, errs)))
	}
	if stats.Dropped > 0 {
		emit(line(step, "warn", fmt.Sprintf("[%s] The target could not keep up: %d arrivals were dropped.", step.Node.Name, stats.Dropped)))
	}
	return nil
}

// waitForCannonResult polls until a stopped run's counts are available.
func waitForCannonResult(ctx context.Context, client *cannonClient, runID string) (*apisixTrafficResult, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		result, err := client.result(ctx, runID)
		if err == nil {
			return result, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// topErrors names the most frequent failure categories of a run.
func topErrors(result *apisixTrafficResult, limit int) string {
	totals := make(map[string]int)
	for _, sampler := range result.Samplers {
		for category, count := range sampler.Errors {
			totals[category] += count
		}
	}
	if len(totals) == 0 {
		return ""
	}
	type entry struct {
		category string
		count    int
	}
	ranked := make([]entry, 0, len(totals))
	for category, count := range totals {
		ranked = append(ranked, entry{category, count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].category < ranked[j].category
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	parts := make([]string, 0, len(ranked))
	for _, item := range ranked {
		parts = append(parts, fmt.Sprintf("%s x%d", item.category, item.count))
	}
	return strings.Join(parts, ", ")
}

// buildAPISIXConfig converts a plan into the cannon's configuration.
//
// The model is open (arrivals at a fixed rate whatever the response time) for
// the throughput variants, and for any variant given rps= or arrival_rate=,
// because a rate cannot be honoured by a count of virtual users. Otherwise it is
// closed.
func buildAPISIXConfig(spec *suites.LoadSpec, runID string, target resolvedTarget, stages []suites.LoadStage) apisixTrafficConfig {
	rate := spec.ArrivalRate
	if rate <= 0 {
		rate = spec.RequestsPerS
	}
	model := "closed"
	switch {
	case spec.Variant == "traffic.constant_throughput" || spec.Variant == "traffic.open_model" || rate > 0:
		model = "open"
	}

	cfg := apisixTrafficConfig{
		RunID:       runID,
		Target:      target.URL,
		HostHeader:  target.HostHeader,
		Model:       model,
		MaxInFlight: 256,
		TimeoutMs:   10000,
	}
	if model == "open" {
		cfg.Rate = rate
		if cfg.Rate <= 0 {
			cfg.Rate = defaultOpenRate
		}
	}

	for _, stage := range stages {
		cfg.Stages = append(cfg.Stages, apisixStageConfig{
			Users:     stage.Users,
			SpawnRate: stage.SpawnRate,
			DurationS: stage.Duration.Seconds(),
			Stop:      stage.Stop,
		})
	}

	for _, user := range spec.Users {
		tasks := make([]apisixTaskConfig, 0, len(user.Tasks))
		for _, task := range user.Tasks {
			tasks = append(tasks, apisixTaskConfig{
				Sampler:      taskSamplerName(task),
				Weight:       maxInt(1, task.Weight),
				Method:       firstNonEmptyStr(task.Request.Method, "GET"),
				Path:         firstNonEmptyStr(task.Request.Path, "/"),
				Body:         task.Request.Body,
				Headers:      task.Request.Headers,
				ExpectStatus: expectedStatuses(task),
			})
		}
		cfg.Users = append(cfg.Users, apisixUserConfig{
			Name:   user.Name,
			Weight: maxInt(1, user.Weight),
			Wait: apisixWaitConfig{
				Mode: user.Wait.Mode,
				S:    user.Wait.Seconds,
				MinS: user.Wait.MinSeconds,
				MaxS: user.Wait.MaxSeconds,
			},
			Tasks: tasks,
		})
	}
	return cfg
}

// defaultOpenRate is the arrival rate when an open-model plan names none.
const defaultOpenRate = 5

// expectedStatuses lists the statuses a task counts as success: the ones its
// `status == N` checks name. With none, the cannon's default applies (any 2xx or
// 3xx). A request answered with a 404 is therefore a failure unless the task
// said a 404 is what it expects.
func expectedStatuses(task suites.LoadTask) []int {
	var expected []int
	for _, check := range append(append([]suites.LoadThreshold{}, task.Checks...), task.Request.Checks...) {
		if check.Metric == "status" && check.Op == "==" {
			expected = append(expected, int(check.Value))
		}
	}
	return expected
}

// bucketsFromMap parses the cannon's bucket keys.
func bucketsFromMap(buckets map[string]int) map[int]int {
	out := make(map[int]int, len(buckets))
	for key, count := range buckets {
		ms, err := strconv.Atoi(key)
		if err != nil || count <= 0 {
			continue
		}
		out[ms] += count
	}
	return out
}

func latencyFromSampler(result *apisixSamplerResult) latencyBuckets {
	b := latencyBuckets{
		m:     bucketsFromMap(result.Buckets),
		count: result.Count,
		sumMs: int64(result.SumMs + 0.5),
		minMs: int(result.MinMs),
		maxMs: int(result.MaxMs),
	}
	return b
}

func (b *latencyBuckets) merge(other latencyBuckets) {
	if other.count == 0 {
		return
	}
	if b.m == nil {
		b.m = make(map[int]int, len(other.m))
	}
	for ms, count := range other.m {
		b.m[ms] += count
	}
	if b.count == 0 || other.minMs < b.minMs {
		b.minMs = other.minMs
	}
	if other.maxMs > b.maxMs {
		b.maxMs = other.maxMs
	}
	b.count += other.count
	b.sumMs += other.sumMs
}

// stageLatency builds a stage's latency from its buckets and exact sum. The
// cannon keeps min and max per sampler, not per stage, so a stage's come from
// its lowest and highest occupied bucket.
func stageLatency(stage apisixStageResult) latencyBuckets {
	b := latencyBuckets{m: bucketsFromMap(stage.Buckets), count: stage.Count, sumMs: int64(stage.SumMs + 0.5)}
	first := true
	for ms := range b.m {
		if first || ms < b.minMs {
			b.minMs = ms
		}
		if ms > b.maxMs {
			b.maxMs = ms
		}
		first = false
	}
	return b
}

func epochSeconds(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(value), int64((value-float64(int64(value)))*1e9))
}

// applyAPISIXResult replaces stats with what the cannon counted. Every figure
// is derived from the counts: the latency buckets are copied, not rebuilt from
// summary numbers, so averages, percentiles, histograms and the throughput
// timeline all describe the requests that were really sent.
func applyAPISIXResult(result *apisixTrafficResult, stats *loadStats, plan []suites.LoadStage) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.Requests, stats.Failures = 0, 0
	stats.lat = latencyBuckets{}
	stats.Samplers = make(map[string]*loadSamplerStats, len(result.Samplers))
	stats.Stages = make(map[int]*loadStageStats, len(result.Stages))
	stats.Errors = make(map[string]int)
	stats.ThroughputByS = make(map[int]int, len(result.PerSecond))

	for name, sampler := range result.Samplers {
		current := &loadSamplerStats{Count: sampler.Count, Failures: sampler.Failures, lat: latencyFromSampler(sampler)}
		stats.Samplers[name] = current
		stats.Requests += sampler.Count
		stats.Failures += sampler.Failures
		stats.lat.merge(current.lat)
		for category, count := range sampler.Errors {
			stats.Errors[category] += count
		}
	}

	stats.StartedAt = epochSeconds(result.StartedAt)
	stats.FinishedAt = epochSeconds(result.FinishedAt)
	stats.PeakUsers = result.PeakUsers
	stats.Dropped = result.Dropped
	for key, count := range result.PerSecond {
		if offset, err := strconv.Atoi(key); err == nil {
			stats.ThroughputByS[offset] = count
		}
	}

	for _, stage := range result.Stages {
		current := &loadStageStats{
			Index:         stage.Index,
			Requests:      stage.Count,
			Failures:      stage.Failures,
			StartedAt:     epochSeconds(stage.StartedAt),
			FinishedAt:    epochSeconds(stage.FinishedAt),
			ThroughputByS: make(map[int]int),
			lat: stageLatency(stage),
		}
		if stage.Index >= 0 && stage.Index < len(plan) {
			current.Users = plan[stage.Index].Users
			current.SpawnRate = plan[stage.Index].SpawnRate
			current.Planned = plan[stage.Index].Duration
			current.Stop = plan[stage.Index].Stop
		}
		// The stage's own timeline: the run's per-second counts inside its window.
		first := int(stage.StartedAt - result.StartedAt)
		last := int(stage.FinishedAt - result.StartedAt)
		for second := first; second <= last; second++ {
			if count, ok := stats.ThroughputByS[second]; ok {
				current.ThroughputByS[second-first] = count
			}
		}
		stats.Stages[stage.Index] = current
	}
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
