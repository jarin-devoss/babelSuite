package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpan runs fn with a recording span in context and returns its attributes.
func recordSpan(t *testing.T, fn func(ctx context.Context)) []attribute.KeyValue {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "step")
	fn(ctx)
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected one span, got %d", len(ended))
	}
	return ended[0].Attributes()
}

func attrValue(attrs []attribute.KeyValue, key string) (string, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}
	return "", false
}

func TestPluginAttributesLandOnTheStepSpan(t *testing.T) {
	attrs := recordSpan(t, func(ctx context.Context) {
		recordPluginSpanAttributes(ctx, map[string]string{
			"tool.name":   "search",
			"judge.score": "0.75",
		})
	})

	if got, ok := attrValue(attrs, "runner.plugin.tool.name"); !ok || got != "search" {
		t.Fatalf("tool.name missing or wrong: %q (%v)", got, ok)
	}
	if got, ok := attrValue(attrs, "runner.plugin.judge.score"); !ok || got != "0.75" {
		t.Fatalf("judge.score missing or wrong: %q (%v)", got, ok)
	}
}

func TestPluginAttributeValuesAreTruncated(t *testing.T) {
	// A plugin echoing a whole prompt back would otherwise bloat every span.
	long := strings.Repeat("x", maxPluginSpanValueLength*3)
	attrs := recordSpan(t, func(ctx context.Context) {
		recordPluginSpanAttributes(ctx, map[string]string{"prompt": long})
	})

	got, ok := attrValue(attrs, "runner.plugin.prompt")
	if !ok {
		t.Fatal("attribute was dropped entirely")
	}
	if len(got) != maxPluginSpanValueLength {
		t.Fatalf("expected truncation to %d, got %d", maxPluginSpanValueLength, len(got))
	}
}

func TestPluginAttributeCountIsCapped(t *testing.T) {
	// Guards trace-backend cardinality against a plugin returning per-call keys.
	supplied := make(map[string]string, maxPluginSpanAttributes*2)
	for i := 0; i < maxPluginSpanAttributes*2; i++ {
		supplied[string(rune('a'+i%26))+string(rune('a'+i/26))] = "v"
	}

	attrs := recordSpan(t, func(ctx context.Context) {
		recordPluginSpanAttributes(ctx, supplied)
	})

	plugin := 0
	for _, a := range attrs {
		if strings.HasPrefix(string(a.Key), pluginSpanAttributePrefix) {
			plugin++
		}
	}
	if plugin > maxPluginSpanAttributes {
		t.Fatalf("expected at most %d attributes, got %d", maxPluginSpanAttributes, plugin)
	}
}

func TestPluginAttributesIgnoreEmptyAndBlankKeys(t *testing.T) {
	attrs := recordSpan(t, func(ctx context.Context) {
		recordPluginSpanAttributes(ctx, map[string]string{"  ": "ignored", "kept": "yes"})
	})

	if _, ok := attrValue(attrs, pluginSpanAttributePrefix+"  "); ok {
		t.Error("a blank key should not be recorded")
	}
	if got, _ := attrValue(attrs, pluginSpanAttributePrefix+"kept"); got != "yes" {
		t.Errorf("valid key was dropped, got %q", got)
	}
}

func TestNoAttributesIsANoOp(t *testing.T) {
	attrs := recordSpan(t, func(ctx context.Context) {
		recordPluginSpanAttributes(ctx, nil)
		recordPluginSpanAttributes(ctx, map[string]string{})
	})

	for _, a := range attrs {
		if strings.HasPrefix(string(a.Key), pluginSpanAttributePrefix) {
			t.Fatalf("unexpected attribute %s", a.Key)
		}
	}
}

// The earlier version of this change attached attributes to a span this test
// created itself, which hid the fact that the span actually current during a
// plugin step is the one startStepSpan opens. Go through that helper instead.
func TestAttributesLandOnTheRunnerStepSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := runnerMetrics.tracer
	runnerMetrics.tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	defer func() { runnerMetrics.tracer = previous }()

	step := StepSpec{ExecutionID: "run-1", SuiteID: "suite-1"}
	step.Node.ID = "judge"
	step.Node.Kind = "plugin"

	spanCtx, span := startStepSpan(context.Background(), step, "local")
	recordPluginSpanAttributes(spanCtx, map[string]string{"judge.score": "0.80"})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected one runner span, got %d", len(ended))
	}
	attrs := ended[0].Attributes()
	if got, ok := attrValue(attrs, "runner.plugin.judge.score"); !ok || got != "0.80" {
		t.Fatalf("attribute did not reach the runner.run span: %q (%v)", got, ok)
	}
	// It must sit alongside the runner's own attributes, not replace them.
	if _, ok := attrValue(attrs, "runner.node_kind"); !ok {
		t.Error("runner attributes were lost")
	}
}

// The attributes travel as JSON from Lua, where every value is produced by
// tostring(). Decode a representative payload to keep the Lua side and the Go
// struct from drifting.
func TestPluginResponseAttributesDecode(t *testing.T) {
	const body = `{
	  "passed": false,
	  "findings": [{"label":"shadow.diff $.total","severity":"critical","detail":"primary=1 shadow=2"}],
	  "attributes": {"shadow.diffs":"1","shadow.threshold":"0","shadow.status":"200/200"}
	}`

	var result pluginResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Attributes) != 3 {
		t.Fatalf("expected 3 attributes, got %d", len(result.Attributes))
	}
	if result.Attributes["shadow.status"] != "200/200" {
		t.Fatalf("unexpected value: %q", result.Attributes["shadow.status"])
	}
	if result.Passed {
		t.Error("passed should have decoded as false")
	}
}

// A plugin that reports no attributes must still decode cleanly — every
// shipped plugin predates the field.
func TestPluginResponseWithoutAttributesDecodes(t *testing.T) {
	var result pluginResult
	if err := json.Unmarshal([]byte(`{"passed":true,"findings":[]}`), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Attributes != nil {
		t.Fatalf("expected no attributes, got %v", result.Attributes)
	}
}
