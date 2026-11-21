package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func TestLocalRunnerEmitsLogs(t *testing.T) {
	r := NewLocal()
	lines := make([]logstream.Line, 0)

	err := r.Run(context.Background(), StepSpec{
		ExecutionID: "run-1",
		SuiteID:     "payment-suite",
		SuiteTitle:  "Payment Suite",
		Profile:     "year.yaml",
		Node: StepNode{
			ID:   "db",
			Name: "db",
			Kind: "service",
		},
	}, func(line logstream.Line) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(lines) < 3 {
		t.Fatalf("expected runner logs, got %d lines", len(lines))
	}
}

func TestLocalRunnerHonorsCancellation(t *testing.T) {
	r := NewLocal()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Run(ctx, StepSpec{
		ExecutionID: "run-1",
		SuiteID:     "payment-suite",
		SuiteTitle:  "Payment Suite",
		Profile:     "year.yaml",
		Node: StepNode{
			ID:   "checkout-smoke",
			Name: "checkout-smoke",
			Kind: "test",
		},
	}, func(logstream.Line) {})
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestLocalRunnerCompletesWithinExpectedWindow(t *testing.T) {
	r := NewLocal()
	start := time.Now()

	if err := r.Run(context.Background(), StepSpec{
		ExecutionID: "run-1",
		SuiteID:     "payment-suite",
		SuiteTitle:  "Payment Suite",
		Profile:     "year.yaml",
		Node: StepNode{
			ID:   "bootstrap-topics",
			Name: "bootstrap-topics",
			Kind: "task",
		},
	}, func(logstream.Line) {}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if time.Since(start) < 400*time.Millisecond {
		t.Fatal("expected task step to simulate a real runner delay")
	}
}

func TestLocalRunnerPassesStepEvaluationControls(t *testing.T) {
	runner := NewLocal()
	expectExit := 0
	lines := make([]logstream.Line, 0)

	err := runner.Run(context.Background(), StepSpec{
		ExecutionID: "run-1",
		SuiteID:     "payment-suite",
		SuiteTitle:  "Payment Suite",
		Profile:     "local.yaml",
		Evaluation: &suites.StepEvaluation{
			ExpectExit: &expectExit,
			ExpectLogs: []string{"successfully"},
			FailOnLogs: []string{"FATAL ERROR"},
		},
		Node: StepNode{
			ID:      "seed-data",
			Name:    "seed-data",
			Kind:    "task",
			Variant: "task.run",
		},
	}, func(line logstream.Line) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	foundEvaluationLog := false
	for _, line := range lines {
		if contains(line.Text, "evaluation controls passed") {
			foundEvaluationLog = true
			break
		}
	}
	if !foundEvaluationLog {
		t.Fatal("expected evaluation success log")
	}
}

func TestLocalRunnerFailsWhenExpectedLogsAreMissing(t *testing.T) {
	runner := NewLocal()
	lines := make([]logstream.Line, 0)

	err := runner.Run(context.Background(), StepSpec{
		ExecutionID: "run-1",
		SuiteID:     "payment-suite",
		SuiteTitle:  "Payment Suite",
		Profile:     "local.yaml",
		Evaluation: &suites.StepEvaluation{
			ExpectLogs: []string{"THIS STRING DOES NOT EXIST"},
		},
		Node: StepNode{
			ID:      "smoke",
			Name:    "smoke",
			Kind:    "test",
			Variant: "test.run",
		},
	}, func(line logstream.Line) {
		lines = append(lines, line)
	})
	if err == nil {
		t.Fatal("expected evaluation failure")
	}
	foundFailureLog := false
	for _, line := range lines {
		if contains(line.Text, "expected logs containing") {
			foundFailureLog = true
			break
		}
	}
	if !foundFailureLog {
		t.Fatal("expected evaluation failure log")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
