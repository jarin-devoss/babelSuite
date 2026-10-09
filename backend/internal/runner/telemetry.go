package runner

import (
	"context"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const runnerScope = "github.com/babelsuite/babelsuite/internal/runner"

type runnerSignals struct {
	tracer   trace.Tracer
	runs     metric.Int64Counter
	failures metric.Int64Counter
}

func newRunnerSignals() *runnerSignals {
	meter := otel.Meter(runnerScope)
	runs, _ := meter.Int64Counter("babelsuite.runner.runs",
		metric.WithDescription("Step executions dispatched by the local runner"))
	failures, _ := meter.Int64Counter("babelsuite.runner.failures",
		metric.WithDescription("Step executions that completed with an error"))
	return &runnerSignals{
		tracer:   otel.Tracer(runnerScope),
		runs:     runs,
		failures: failures,
	}
}

var runnerMetrics = newRunnerSignals()

func startStepSpan(ctx context.Context, step StepSpec, backend string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attribute.String("runner.execution_id", step.ExecutionID),
		attribute.String("runner.suite_id", step.SuiteID),
		attribute.String("runner.node_id", step.Node.ID),
		attribute.String("runner.node_kind", step.Node.Kind),
		attribute.String("runner.backend", backend),
	}
	spanCtx, span := runnerMetrics.tracer.Start(ctx, "runner.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	runnerMetrics.runs.Add(ctx, 1, metric.WithAttributes(attrs...))
	return spanCtx, span
}

const (
	// Span attributes are indexed by the trace backend, so a plugin returning
	// something per-call unique would multiply its cardinality. Cap the count
	// and the value length rather than trusting every plugin to behave.
	maxPluginSpanAttributes   = 24
	maxPluginSpanValueLength  = 256
	pluginSpanAttributePrefix = "runner.plugin."
)

// recordPluginSpanAttributes adds a plugin's reported attributes to the
// runner.run span for this step. The span otherwise carries only what the
// runner knows — execution, suite, node and backend — so this is how a plugin
// reports what it actually did, such as the tool it called or the score it gave.
func recordPluginSpanAttributes(ctx context.Context, attributes map[string]string) {
	if len(attributes) == 0 {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	// Sort so a plugin exceeding the cap loses the same keys on every run
	// instead of an arbitrary subset.
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	recorded := make([]attribute.KeyValue, 0, len(keys))
	for _, key := range keys {
		if len(recorded) >= maxPluginSpanAttributes {
			break
		}
		value := attributes[key]
		if len(value) > maxPluginSpanValueLength {
			value = value[:maxPluginSpanValueLength]
		}
		recorded = append(recorded, attribute.String(pluginSpanAttributePrefix+key, value))
	}

	span.SetAttributes(recorded...)
}

func finishStepSpan(ctx context.Context, span trace.Span, step StepSpec, err error) {
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		attrs := []attribute.KeyValue{
			attribute.String("runner.node_kind", step.Node.Kind),
		}
		runnerMetrics.failures.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	span.End()
}
