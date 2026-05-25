package runner

import (
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func intPtr(v int) *int { return &v }

// evalCase holds inputs and expected outputs for a single evaluation scenario.
type evalCase struct {
	name        string
	evaluation  *suites.StepEvaluation
	exitCode    int
	logs        []string
	wantErr     bool
	errContains string
}

func runEvalCase(t *testing.T, tc evalCase) {
	t.Helper()
	t.Run(tc.name, func(t *testing.T) {
		t.Parallel()
		step := StepSpec{
			Node:       StepNode{ID: "s", Name: "s", Kind: "test"},
			Evaluation: tc.evaluation,
		}
		var emitted []logstream.Line
		err := evaluateStepExpectations(step, tc.exitCode, tc.logs, func(l logstream.Line) {
			emitted = append(emitted, l)
		})
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error, got nil; emitted logs: %v", emitted)
			}
			if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
			}
		} else {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})
}

func TestEvaluateStepExpectationsNilEvaluationPasses(t *testing.T) {
	t.Parallel()
	step := StepSpec{Node: StepNode{ID: "s", Name: "s"}}
	err := evaluateStepExpectations(step, 0, nil, func(logstream.Line) {})
	if err != nil {
		t.Fatalf("nil evaluation should always pass, got %v", err)
	}
}

func TestEvaluateStepExpectationsTable(t *testing.T) {
	t.Parallel()

	cases := []evalCase{
		{
			name:     "exit code matches expectation",
			evaluation: &suites.StepEvaluation{ExpectExit: intPtr(0)},
			exitCode: 0,
		},
		{
			name:        "exit code mismatch",
			evaluation:  &suites.StepEvaluation{ExpectExit: intPtr(0)},
			exitCode:    1,
			wantErr:     true,
			errContains: "exit code",
		},
		{
			name:       "nonzero exit code expectation matches",
			evaluation: &suites.StepEvaluation{ExpectExit: intPtr(2)},
			exitCode:   2,
		},
		{
			name:       "expected log present in output",
			evaluation: &suites.StepEvaluation{ExpectLogs: []string{"healthy"}},
			logs:       []string{"service is healthy"},
		},
		{
			name:        "expected log absent from output",
			evaluation:  &suites.StepEvaluation{ExpectLogs: []string{"startup complete"}},
			logs:        []string{"some other output"},
			wantErr:     true,
			errContains: "startup complete",
		},
		{
			name:        "forbidden log found in output",
			evaluation:  &suites.StepEvaluation{FailOnLogs: []string{"FATAL"}},
			logs:        []string{"FATAL: out of memory"},
			wantErr:     true,
			errContains: "FATAL",
		},
		{
			name:       "forbidden log not present",
			evaluation: &suites.StepEvaluation{FailOnLogs: []string{"FATAL"}},
			logs:       []string{"everything is fine"},
		},
		{
			name: "all rules pass together",
			evaluation: &suites.StepEvaluation{
				ExpectExit: intPtr(0),
				ExpectLogs: []string{"ready"},
				FailOnLogs: []string{"ERROR"},
			},
			exitCode: 0,
			logs:     []string{"service ready"},
		},
		{
			name:       "empty expected log strings are skipped",
			evaluation: &suites.StepEvaluation{ExpectLogs: []string{"", "real"}},
			logs:       []string{"real output"},
		},
		{
			name:       "empty forbidden log strings are skipped",
			evaluation: &suites.StepEvaluation{FailOnLogs: []string{"", "bad-string"}},
			logs:       []string{"clean output"},
		},
		{
			name:       "nil logs slice with no expectations passes",
			evaluation: &suites.StepEvaluation{},
			logs:       nil,
		},
		{
			name:        "multiple expected logs one missing fails",
			evaluation:  &suites.StepEvaluation{ExpectLogs: []string{"alpha", "beta"}},
			logs:        []string{"alpha present"},
			wantErr:     true,
			errContains: "beta",
		},
	}

	for _, tc := range cases {
		runEvalCase(t, tc)
	}
}

func TestEvaluateStepExpectationsEmitsPassLogWhenRulesPresent(t *testing.T) {
	t.Parallel()
	step := StepSpec{
		Node:       StepNode{ID: "s", Name: "s", Kind: "test"},
		Evaluation: &suites.StepEvaluation{ExpectExit: intPtr(0)},
	}
	var emitted []logstream.Line
	if err := evaluateStepExpectations(step, 0, nil, func(l logstream.Line) {
		emitted = append(emitted, l)
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, l := range emitted {
		if strings.Contains(strings.ToLower(l.Text), "evaluation controls passed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected evaluation-passed log line to be emitted")
	}
}

func TestEvaluateStepExpectationsErrorLineIsEmitted(t *testing.T) {
	t.Parallel()
	step := StepSpec{
		Node:       StepNode{ID: "s", Name: "s"},
		Evaluation: &suites.StepEvaluation{ExpectExit: intPtr(0)},
	}
	var errorLines []logstream.Line
	err := evaluateStepExpectations(step, 1, nil, func(l logstream.Line) {
		if l.Level == "error" {
			errorLines = append(errorLines, l)
		}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(errorLines) == 0 {
		t.Fatal("expected an error-level log line to be emitted on evaluation failure")
	}
}
