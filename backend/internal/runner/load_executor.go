package runner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// latencyBuckets is a coarsened histogram keyed by bucketed milliseconds.
// Bucket granularity: exact below 100 ms, 10 ms steps to 1 s,
// 100 ms steps to 10 s, 1 s steps above — ~320 keys max regardless of volume.
type latencyBuckets struct {
	m     map[int]int
	count int
	sumMs int64
	minMs int
	maxMs int
}

func bucketLatencyMs(ms int) int {
	if ms < 0 {
		ms = 0
	}
	if ms < 100 {
		return ms
	}
	if ms < 1000 {
		return (ms / 10) * 10
	}
	if ms < 10000 {
		return (ms / 100) * 100
	}
	return (ms / 1000) * 1000
}

func (b *latencyBuckets) addMs(ms int) {
	if b.m == nil {
		b.m = make(map[int]int, 32)
	}
	key := bucketLatencyMs(ms)
	b.m[key]++
	b.count++
	b.sumMs += int64(ms)
	if b.count == 1 || ms < b.minMs {
		b.minMs = ms
	}
	if ms > b.maxMs {
		b.maxMs = ms
	}
}

func (b latencyBuckets) percentile(p float64) float64 {
	if b.count == 0 || len(b.m) == 0 {
		return 0
	}
	keys := make([]int, 0, len(b.m))
	for k := range b.m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	target := int(math.Ceil(p / 100 * float64(b.count)))
	if target < 1 {
		target = 1
	}
	accumulated := 0
	for _, k := range keys {
		accumulated += b.m[k]
		if accumulated >= target {
			return float64(k)
		}
	}
	return float64(keys[len(keys)-1])
}

type loadSamplerStats struct {
	Count    int
	Failures int
	lat      latencyBuckets
}

type loadStageStats struct {
	Index         int
	Users         int
	SpawnRate     float64
	Planned       time.Duration
	Stop          bool
	StartedAt     time.Time
	FinishedAt    time.Time
	Requests      int
	Failures      int
	lat           latencyBuckets
	ThroughputByS map[int]int
}

type loadStats struct {
	mu            sync.Mutex
	StartedAt     time.Time
	FinishedAt    time.Time
	Requests      int
	Failures      int
	PeakUsers     int
	lat           latencyBuckets
	ThroughputByS map[int]int
	Samplers      map[string]*loadSamplerStats
	Stages        map[int]*loadStageStats
	// Dropped counts open-model arrivals the generator refused to start because
	// too many requests were already in flight: the target could not keep up.
	Dropped int
	// Errors counts failed requests by category (connect, timeout, http_404, ...).
	Errors map[string]int
}

func newLoadStats() *loadStats {
	return &loadStats{
		StartedAt:     time.Now(),
		ThroughputByS: make(map[int]int),
		Samplers:      make(map[string]*loadSamplerStats),
		Stages:        make(map[int]*loadStageStats),
		Errors:        make(map[string]int),
	}
}

type loadSummary struct {
	Dropped        int
	Requests       int
	Failures       int
	ErrorRate      float64
	PeakUsers      int
	ActualDuration time.Duration
	AverageRPS     float64
	PeakRPS        float64
	Latency        loadLatencySummary
	Throughput     []loadThroughputPoint
	Samplers       []loadSamplerSummary
	Stages         []loadStageSummary
}

type loadSamplerSummary struct {
	Name      string
	Count     int
	Failures  int
	ErrorRate float64
	Latency   loadLatencySummary
}

type loadStageSummary struct {
	Index           int
	Users           int
	SpawnRate       float64
	PlannedDuration time.Duration
	ActualDuration  time.Duration
	Requests        int
	Failures        int
	ErrorRate       float64
	AverageRPS      float64
	PeakRPS         float64
	Latency         loadLatencySummary
}

type loadThroughputPoint struct {
	OffsetSeconds int
	Requests      int
}

type loadLatencySummary struct {
	Count     int
	MinMillis float64
	MaxMillis float64
	AvgMillis float64
	P50Millis float64
	P90Millis float64
	P95Millis float64
	P99Millis float64
	Histogram []loadHistogramBucket
}

type loadHistogramBucket struct {
	Label string
	Count int
}

func (s *loadStats) beginStage(index int, stage suites.LoadStage, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Stages[index]
	if current == nil {
		current = &loadStageStats{
			Index:         index,
			ThroughputByS: make(map[int]int),
		}
		s.Stages[index] = current
	}
	current.Users = stage.Users
	current.SpawnRate = stage.SpawnRate
	current.Planned = stage.Duration
	current.Stop = stage.Stop
	current.StartedAt = startedAt
	if current.FinishedAt.Before(startedAt) {
		current.FinishedAt = time.Time{}
	}
}

func (s *loadStats) endStage(index int, finishedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.Stages[index]
	if current == nil {
		return
	}
	current.FinishedAt = finishedAt
	if finishedAt.After(s.FinishedAt) {
		s.FinishedAt = finishedAt
	}
}

func (s *loadStats) recordResult(sampler string, latencyMs int, failed bool, stageIndex int, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.StartedAt.IsZero() || observedAt.Before(s.StartedAt) {
		s.StartedAt = observedAt
	}
	if observedAt.After(s.FinishedAt) {
		s.FinishedAt = observedAt
	}
	s.Requests++
	if failed {
		s.Failures++
	}
	s.lat.addMs(latencyMs)
	s.ThroughputByS[throughputBucketIndex(s.StartedAt, observedAt)]++

	current := s.Samplers[sampler]
	if current == nil {
		current = &loadSamplerStats{}
		s.Samplers[sampler] = current
	}
	current.Count++
	if failed {
		current.Failures++
	}
	current.lat.addMs(latencyMs)

	stage := s.Stages[stageIndex]
	if stage == nil {
		stage = &loadStageStats{
			Index:         stageIndex,
			ThroughputByS: make(map[int]int),
		}
		s.Stages[stageIndex] = stage
	}
	if stage.StartedAt.IsZero() || observedAt.Before(stage.StartedAt) {
		stage.StartedAt = observedAt
	}
	if observedAt.After(stage.FinishedAt) {
		stage.FinishedAt = observedAt
	}
	stage.Requests++
	if failed {
		stage.Failures++
	}
	stage.lat.addMs(latencyMs)
	stage.ThroughputByS[throughputBucketIndex(stage.StartedAt, observedAt)]++
}

func (s *loadStats) recordActiveUsers(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count > s.PeakUsers {
		s.PeakUsers = count
	}
}

func (s *loadStats) summary() loadSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	summary := loadSummary{
		Requests:   s.Requests,
		Failures:   s.Failures,
		PeakUsers:  s.PeakUsers,
		Dropped:    s.Dropped,
		Latency:    summarizeLatencies(s.lat),
		Throughput: summarizeThroughput(s.ThroughputByS),
		Samplers:   make([]loadSamplerSummary, 0, len(s.Samplers)),
		Stages:     make([]loadStageSummary, 0, len(s.Stages)),
	}
	if s.Requests > 0 {
		summary.ErrorRate = float64(s.Failures) / float64(s.Requests)
	}
	if !s.StartedAt.IsZero() && !s.FinishedAt.IsZero() && s.FinishedAt.After(s.StartedAt) {
		summary.ActualDuration = s.FinishedAt.Sub(s.StartedAt)
	}
	summary.AverageRPS, summary.PeakRPS = summarizeRPS(summary.Requests, summary.ActualDuration, summary.Throughput)
	for name, sampler := range s.Samplers {
		errorRate := 0.0
		if sampler.Count > 0 {
			errorRate = float64(sampler.Failures) / float64(sampler.Count)
		}
		summary.Samplers = append(summary.Samplers, loadSamplerSummary{
			Name:      name,
			Count:     sampler.Count,
			Failures:  sampler.Failures,
			ErrorRate: errorRate,
			Latency:   summarizeLatencies(sampler.lat),
		})
	}
	sort.Slice(summary.Samplers, func(i, j int) bool {
		return summary.Samplers[i].Name < summary.Samplers[j].Name
	})
	for index, stage := range s.Stages {
		actualDuration := stage.Planned
		if !stage.StartedAt.IsZero() && !stage.FinishedAt.IsZero() && stage.FinishedAt.After(stage.StartedAt) {
			actualDuration = stage.FinishedAt.Sub(stage.StartedAt)
		}
		errorRate := 0.0
		if stage.Requests > 0 {
			errorRate = float64(stage.Failures) / float64(stage.Requests)
		}
		points := summarizeThroughput(stage.ThroughputByS)
		avgRPS, peakRPS := summarizeRPS(stage.Requests, actualDuration, points)
		summary.Stages = append(summary.Stages, loadStageSummary{
			Index:           index,
			Users:           stage.Users,
			SpawnRate:       stage.SpawnRate,
			PlannedDuration: stage.Planned,
			ActualDuration:  actualDuration,
			Requests:        stage.Requests,
			Failures:        stage.Failures,
			ErrorRate:       errorRate,
			AverageRPS:      avgRPS,
			PeakRPS:         peakRPS,
			Latency:         summarizeLatencies(stage.lat),
		})
	}
	sort.Slice(summary.Stages, func(i, j int) bool {
		return summary.Stages[i].Index < summary.Stages[j].Index
	})
	return summary
}

func snapshotMetrics(stats *loadStats) TrafficMetricSnapshot {
	s := stats.summary()
	return TrafficMetricSnapshot{
		Requests:  s.Requests,
		Failures:  s.Failures,
		ErrorRate: s.ErrorRate,
		RPS:       s.AverageRPS,
		Users:     s.PeakUsers,
		MinMS:     s.Latency.MinMillis,
		AvgMS:     s.Latency.AvgMillis,
		P50MS:     s.Latency.P50Millis,
		P95MS:     s.Latency.P95Millis,
		P99MS:     s.Latency.P99Millis,
		MaxMS:     s.Latency.MaxMillis,
	}
}

func (s loadSummary) metricValue(metric string, sampler string) (float64, bool) {
	if metric == "http.error_rate" {
		return s.ErrorRate, true
	}
	if metric == "throughput.avg_rps" || metric == "traffic.avg_rps" {
		return s.AverageRPS, true
	}
	if metric == "throughput.peak_rps" || metric == "traffic.peak_rps" {
		return s.PeakRPS, true
	}

	latency := s.Latency
	if sampler != "" {
		found := false
		for _, item := range s.Samplers {
			if item.Name == sampler {
				latency = item.Latency
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}

	switch metric {
	case "http.min_ms", "latency.min_ms":
		return latency.MinMillis, latency.Count > 0
	case "http.avg_ms", "latency.avg_ms":
		return latency.AvgMillis, latency.Count > 0
	case "http.max_ms", "latency.max_ms":
		return latency.MaxMillis, latency.Count > 0
	case "http.p50_ms", "latency.p50_ms":
		return latency.P50Millis, latency.Count > 0
	case "http.p90_ms", "latency.p90_ms":
		return latency.P90Millis, latency.Count > 0
	case "http.p95_ms", "latency.p95_ms":
		return latency.P95Millis, latency.Count > 0
	case "http.p99_ms", "latency.p99_ms":
		return latency.P99Millis, latency.Count > 0
	default:
		return 0, false
	}
}

func summarizeLatencies(b latencyBuckets) loadLatencySummary {
	if b.count == 0 {
		return loadLatencySummary{}
	}
	avgMs := float64(b.sumMs) / float64(b.count)
	return loadLatencySummary{
		Count:     b.count,
		MinMillis: float64(b.minMs),
		MaxMillis: float64(b.maxMs),
		AvgMillis: avgMs,
		P50Millis: b.percentile(50),
		P90Millis: b.percentile(90),
		P95Millis: b.percentile(95),
		P99Millis: b.percentile(99),
		Histogram: buildLatencyHistogram(b),
	}
}

func summarizeThroughput(buckets map[int]int) []loadThroughputPoint {
	if len(buckets) == 0 {
		return nil
	}
	offsets := make([]int, 0, len(buckets))
	for offset := range buckets {
		offsets = append(offsets, offset)
	}
	sort.Ints(offsets)
	points := make([]loadThroughputPoint, 0, len(offsets))
	for _, offset := range offsets {
		points = append(points, loadThroughputPoint{
			OffsetSeconds: offset,
			Requests:      buckets[offset],
		})
	}
	return points
}

func summarizeRPS(requests int, duration time.Duration, throughput []loadThroughputPoint) (float64, float64) {
	peak := 0.0
	for _, point := range throughput {
		if float64(point.Requests) > peak {
			peak = float64(point.Requests)
		}
	}
	if duration <= 0 {
		return peak, peak
	}
	return float64(requests) / duration.Seconds(), peak
}

func throughputBucketIndex(startedAt time.Time, observedAt time.Time) int {
	if startedAt.IsZero() || observedAt.Before(startedAt) {
		return 0
	}
	return int(observedAt.Sub(startedAt) / time.Second)
}

func buildLatencyHistogram(b latencyBuckets) []loadHistogramBucket {
	thresholds := []struct {
		label string
		limit int
	}{
		{label: "<=10ms", limit: 10},
		{label: "<=25ms", limit: 25},
		{label: "<=50ms", limit: 50},
		{label: "<=100ms", limit: 100},
		{label: "<=250ms", limit: 250},
		{label: "<=500ms", limit: 500},
		{label: "<=1000ms", limit: 1000},
	}
	buckets := make([]loadHistogramBucket, len(thresholds)+1)
	for i, t := range thresholds {
		buckets[i].Label = t.label
	}
	buckets[len(thresholds)].Label = ">1000ms"
	for key, count := range b.m {
		placed := false
		for i, t := range thresholds {
			if key <= t.limit {
				buckets[i].Count += count
				placed = true
				break
			}
		}
		if !placed {
			buckets[len(thresholds)].Count += count
		}
	}
	return buckets
}

func formatThroughputTimeline(points []loadThroughputPoint) string {
	if len(points) == 0 {
		return "none"
	}
	parts := make([]string, 0, minInt(len(points), 12))
	appendPoint := func(point loadThroughputPoint) {
		parts = append(parts, fmt.Sprintf("t+%ds=%drps", point.OffsetSeconds, point.Requests))
	}
	if len(points) <= 12 {
		for _, point := range points {
			appendPoint(point)
		}
		return strings.Join(parts, ", ")
	}
	for _, point := range points[:8] {
		appendPoint(point)
	}
	parts = append(parts, "...")
	for _, point := range points[len(points)-3:] {
		appendPoint(point)
	}
	return strings.Join(parts, ", ")
}

func formatHistogram(buckets []loadHistogramBucket) string {
	if len(buckets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		parts = append(parts, fmt.Sprintf("%s=%d", bucket.Label, bucket.Count))
	}
	return strings.Join(parts, ", ")
}

func taskSamplerName(task suites.LoadTask) string {
	return strutil.FirstNonEmpty(task.Request.Name, task.Name, task.Request.Path)
}

func compareLoadValue(actual float64, op string, expected float64) bool {
	switch op {
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	case "<":
		return actual < expected
	case "<=":
		return actual <= expected
	case ">":
		return actual > expected
	case ">=":
		return actual >= expected
	default:
		return false
	}
}

func containsLoadMetric(thresholds []suites.LoadThreshold, metric string) bool {
	for _, threshold := range thresholds {
		if threshold.Metric == metric {
			return true
		}
	}
	return false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// executeLoadStep runs a traffic step: real requests, from the suite's sidecar,
// to the target the suite names. It never substitutes made-up traffic: when the
// run cannot happen (no sidecar, a target nothing provides, an unreachable host)
// the step fails and says why, and a run that sent nothing is a failure too.
func executeLoadStep(ctx context.Context, step StepSpec, emit func(logstream.Line)) error {
	if step.Load == nil {
		return fmt.Errorf("traffic step %q has no load spec", step.Node.Name)
	}
	if !canUseAPISIXTraffic(step) {
		return fmt.Errorf("traffic step %q: the suite's APISIX sidecar is not running, so no load can be generated", step.Node.Name)
	}

	plan := "the default " + trafficProfileLabel(step.Node.Variant) + " shape"
	if step.Load.PlanPath != "" {
		plan = "traffic plan " + step.Load.PlanPath
	}
	emit(line(step, "info", fmt.Sprintf("[%s] Running %s against %s.", step.Node.Name, plan, step.Load.Target)))

	target, err := resolveTrafficTarget(ctx, step)
	if err != nil {
		return err
	}
	emit(line(step, "info", fmt.Sprintf("[%s] Target %s resolved to %s: %s.", step.Node.Name, step.Load.Target, target.URL, target.Note)))

	runID := trafficRunID(step)
	if step.Load.Variant == "traffic.scalability" {
		return runScalabilityModel(ctx, step, emit, target, runID)
	}

	stats := newLoadStats()
	if err := runAPISIXTraffic(ctx, step, emit, stats, target, step.Load.Stages, runID); err != nil {
		return err
	}
	return finalizeLoadStep(step, emit, stats)
}

// trafficRunID names a load run in the sidecar. The sidecar serves every
// execution of a suite and profile, and keeps each run's counts under its id, so
// the id has to be unique across all of them.
func trafficRunID(step StepSpec) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, step.ExecutionID+"-"+step.Node.ID)
	return clean + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func finalizeLoadStep(step StepSpec, emit func(logstream.Line), stats *loadStats) error {
	summary := stats.summary()
	emit(line(step, "info", fmt.Sprintf("[%s] Traffic run completed with %d requests, %d failures, and peak concurrency %d.", step.Node.Name, summary.Requests, summary.Failures, summary.PeakUsers)))
	emit(line(step, "info", fmt.Sprintf("[%s] Latency avg=%.1fms min=%.1fms max=%.1fms p50=%.1fms p90=%.1fms p95=%.1fms p99=%.1fms.", step.Node.Name, summary.Latency.AvgMillis, summary.Latency.MinMillis, summary.Latency.MaxMillis, summary.Latency.P50Millis, summary.Latency.P90Millis, summary.Latency.P95Millis, summary.Latency.P99Millis)))
	emit(line(step, "info", fmt.Sprintf("[%s] Throughput avg=%.1frps peak=%.1frps timeline=%s.", step.Node.Name, summary.AverageRPS, summary.PeakRPS, formatThroughputTimeline(summary.Throughput))))
	if histogram := formatHistogram(summary.Latency.Histogram); histogram != "" {
		emit(line(step, "info", fmt.Sprintf("[%s] Latency histogram %s.", step.Node.Name, histogram)))
	}
	for _, sampler := range summary.Samplers {
		emit(line(step, "info", fmt.Sprintf("[%s] Sampler %s avg=%.1fms min=%.1fms max=%.1fms p50=%.1fms p90=%.1fms p95=%.1fms p99=%.1fms count=%d failures=%d.", step.Node.Name, sampler.Name, sampler.Latency.AvgMillis, sampler.Latency.MinMillis, sampler.Latency.MaxMillis, sampler.Latency.P50Millis, sampler.Latency.P90Millis, sampler.Latency.P95Millis, sampler.Latency.P99Millis, sampler.Count, sampler.Failures)))
	}
	for _, stage := range summary.Stages {
		emit(line(step, "info", fmt.Sprintf("[%s] Stage %d summary users=%d spawn_rate=%.1f duration=%s requests=%d failures=%d avg_rps=%.1f peak_rps=%.1f avg=%.1fms p95=%.1fms p99=%.1fms.", step.Node.Name, stage.Index+1, stage.Users, stage.SpawnRate, stage.ActualDuration, stage.Requests, stage.Failures, stage.AverageRPS, stage.PeakRPS, stage.Latency.AvgMillis, stage.Latency.P95Millis, stage.Latency.P99Millis)))
	}
	return evaluateLoadThresholds(step, stats)
}

// runScalabilityModel finds how many users a target sustains by running real
// traffic, one probe per stage, until a probe breaks a threshold. It fails when
// nothing is sustainable (the first probe already breaks), and when the
// sustained count is below min_users= if the step names one.
func runScalabilityModel(ctx context.Context, step StepSpec, emit func(logstream.Line), target resolvedTarget, baseRunID string) error {
	stages := step.Load.Stages
	if len(stages) == 0 {
		return fmt.Errorf("traffic step %q: a scalability run needs at least one probe stage", step.Node.Name)
	}

	lastPassing := 0
	for index, stage := range stages {
		probe := newLoadStats()
		emit(line(step, "info", fmt.Sprintf("[%s] Scalability probe %d: users=%d spawn_rate=%.1f duration=%s.", step.Node.Name, index+1, stage.Users, stage.SpawnRate, stage.Duration)))
		if err := runAPISIXTraffic(ctx, step, emit, probe, target, []suites.LoadStage{stage}, fmt.Sprintf("%s-probe-%d", baseRunID, index+1)); err != nil {
			return err
		}

		summary := probe.summary()
		emit(line(step, "info", fmt.Sprintf("[%s] Probe %d result: users=%d requests=%d failures=%d error_rate=%.4f avg=%.1fms p95=%.1fms p99=%.1fms.", step.Node.Name, index+1, stage.Users, summary.Requests, summary.Failures, summary.ErrorRate, summary.Latency.AvgMillis, summary.Latency.P95Millis, summary.Latency.P99Millis)))
		if summary.Requests == 0 {
			// Not a breaking point: nothing was measured, so nothing can be concluded.
			return fmt.Errorf("scalability probe %d sent no requests to %s", index+1, step.Load.Target)
		}

		if err := evaluateLoadThresholds(step, probe); err != nil {
			emit(line(step, "warn", fmt.Sprintf("[%s] Breaking point found at %d users: %v.", step.Node.Name, stage.Users, err)))
			if lastPassing == 0 {
				return fmt.Errorf("nothing is sustainable: the first probe (%d users) already broke: %w", stage.Users, err)
			}
			emit(line(step, "info", fmt.Sprintf("[%s] Max sustainable load: %d users (last passing probe before the breaking point at %d).", step.Node.Name, lastPassing, stage.Users)))
			return checkMinUsers(step, lastPassing)
		}
		lastPassing = stage.Users
	}

	emit(line(step, "info", fmt.Sprintf("[%s] All %d scalability probes passed. Max sustainable load: %d users.", step.Node.Name, len(stages), lastPassing)))
	return checkMinUsers(step, lastPassing)
}

func checkMinUsers(step StepSpec, sustained int) error {
	if step.Load.MinUsers > 0 && sustained < step.Load.MinUsers {
		return fmt.Errorf("sustained %d users, below the min_users=%d the step requires", sustained, step.Load.MinUsers)
	}
	return nil
}

// evaluateLoadThresholds checks a run against its thresholds. A run that
// measured nothing fails before any threshold is read: with no requests every
// rate is zero, and zero meets "error rate <= 0", which is how a run that sent
// no traffic used to pass.
func evaluateLoadThresholds(step StepSpec, stats *loadStats) error {
	summary := stats.summary()
	if summary.Requests == 0 {
		return fmt.Errorf("no requests were sent to %s", step.Load.Target)
	}

	failures := make([]string, 0)
	for _, stage := range summary.Stages {
		if stage.Requests == 0 {
			failures = append(failures, fmt.Sprintf("stage %d sent no requests", stage.Index+1))
		}
	}

	thresholds := make([]suites.LoadThreshold, 0, len(step.Load.Thresholds)+4)
	thresholds = append(thresholds, step.Load.Thresholds...)
	if !containsLoadMetric(thresholds, "http.error_rate") {
		thresholds = append(thresholds, suites.LoadThreshold{Metric: "http.error_rate", Op: "<=", Value: 0})
	}
	for _, user := range step.Load.Users {
		for _, task := range user.Tasks {
			for _, check := range task.Checks {
				if check.Metric == "status" {
					continue
				}
				normalized := check
				if normalized.Sampler == "" {
					normalized.Sampler = taskSamplerName(task)
				}
				thresholds = append(thresholds, normalized)
			}
		}
	}

	for _, threshold := range thresholds {
		switch threshold.Metric {
		case "http.error_rate":
			if !compareLoadValue(summary.ErrorRate, threshold.Op, threshold.Value) {
				failures = append(failures, fmt.Sprintf("%s %s %.2f failed (got %.4f%s)", threshold.Metric, threshold.Op, threshold.Value, summary.ErrorRate, topErrorSuffix(stats)))
			}
		case "traffic.dropped":
			if !compareLoadValue(float64(summary.Dropped), threshold.Op, threshold.Value) {
				failures = append(failures, fmt.Sprintf("%s %s %.0f failed (got %d: the target could not keep up)", threshold.Metric, threshold.Op, threshold.Value, summary.Dropped))
			}
		case "http.min_ms", "latency.min_ms", "http.avg_ms", "latency.avg_ms", "http.max_ms", "latency.max_ms", "http.p50_ms", "latency.p50_ms", "http.p90_ms", "latency.p90_ms", "http.p95_ms", "latency.p95_ms", "http.p99_ms", "latency.p99_ms", "throughput.avg_rps", "traffic.avg_rps", "throughput.peak_rps", "traffic.peak_rps":
			value, ok := summary.metricValue(threshold.Metric, threshold.Sampler)
			if !ok {
				failures = append(failures, fmt.Sprintf("%s %s %.2f cannot be checked: sampler %s sent no requests", threshold.Metric, threshold.Op, threshold.Value, strutil.FirstNonEmpty(threshold.Sampler, "all")))
			} else if !compareLoadValue(value, threshold.Op, threshold.Value) {
				failures = append(failures, fmt.Sprintf("%s %s %.2f failed for %s (got %.2f)", threshold.Metric, threshold.Op, threshold.Value, strutil.FirstNonEmpty(threshold.Sampler, "all"), value))
			}
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New(strings.Join(failures, "; "))
}

// topErrorSuffix names the most frequent failure categories, so a failed error
// rate says what the failures were.
func topErrorSuffix(stats *loadStats) string {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	if len(stats.Errors) == 0 {
		return ""
	}
	type entry struct {
		category string
		count    int
	}
	ranked := make([]entry, 0, len(stats.Errors))
	for category, count := range stats.Errors {
		ranked = append(ranked, entry{category, count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].category < ranked[j].category
	})
	if len(ranked) > 3 {
		ranked = ranked[:3]
	}
	parts := make([]string, 0, len(ranked))
	for _, item := range ranked {
		parts = append(parts, fmt.Sprintf("%s x%d", item.category, item.count))
	}
	return "; " + strings.Join(parts, ", ")
}
