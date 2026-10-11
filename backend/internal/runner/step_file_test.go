package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

// A step naming a file the package does not contain used to start no container
// and be reported healthy by the simulated path, so a suite stayed green while
// its seeding or verification never ran.
func TestMissingStepFileIsReported(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		kind   string
		file   string
		body   string
		fails  bool
		expect string
	}{
		{name: "task with a file that resolved nothing", kind: "task", file: "seed.sh", fails: true, expect: "tasks/seed.sh"},
		{name: "test with a file that resolved nothing", kind: "test", file: "smoke.py", fails: true, expect: "tests/smoke.py"},
		{name: "task whose file resolved", kind: "task", file: "seed.sh", body: "echo hi"},
		{name: "test whose file resolved", kind: "test", file: "smoke.py", body: "print(1)"},
		{name: "task with no file at all uses commands", kind: "task", file: ""},
		{name: "service carrying no file", kind: "service", file: ""},
		// A service runs file= too, and a missing one is worse there: the
		// container starts and silently runs the image's default command.
		{name: "service with a file that resolved nothing", kind: "service", file: "stub.py", fails: true, expect: "stub.py"},
		{name: "service whose file resolved", kind: "service", file: "stub.py", body: "print(1)"},
		{name: "whitespace-only content counts as missing", kind: "task", file: "seed.sh", body: "   \n\t ", fails: true},
		// These kinds never execute file=, so an unresolved one is not what is
		// wrong with the step and the guard must stay out of the way. A mock is
		// served from its own cue/ folder and a traffic plan travels in
		// Node.Load, neither of which goes through FileContent.
		{name: "mock is not a file-executing kind", kind: "mock", file: "responses.json"},
		{name: "traffic carries its plan in Load, not file", kind: "traffic", file: "plan.js"},
		{name: "plugin answers in the sidecar", kind: "plugin", file: "plugin.lua"},
		{name: "security runs a scanner", kind: "security", file: "scan.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			step := StepSpec{Node: StepNode{
				Kind: tc.kind, Name: "step-" + tc.kind,
				File: tc.file, FileContent: tc.body,
			}}
			err := requireResolvedStepFile(step)
			if tc.fails && err == nil {
				t.Fatalf("expected an error for %s file=%q content=%q", tc.kind, tc.file, tc.body)
			}
			if !tc.fails && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.fails && tc.expect != "" && !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("error should name the searched path %q, got: %v", tc.expect, err)
			}
			if tc.fails && !strings.Contains(err.Error(), tc.file) {
				t.Fatalf("error should name the file, got: %v", err)
			}
		})
	}
}

// The tests above cover the predicate. This one covers the fix: Run itself has
// to fail the step. Without it, deleting the call from Run while leaving the
// function in place would keep every test above green and quietly restore the
// silent pass this change exists to stop.
func TestRunFailsTheStepWhenItsFileIsMissing(t *testing.T) {
	t.Parallel()

	var emitted []logstream.Line
	step := StepSpec{Node: StepNode{
		ID: "seed", Kind: "task", Name: "seed", File: "seed.sh",
	}}

	err := NewLocal().Run(context.Background(), step, func(l logstream.Line) {
		emitted = append(emitted, l)
	})
	if err == nil {
		t.Fatal("Run should fail a step whose file= resolved to nothing")
	}
	if !strings.Contains(err.Error(), "seed.sh") {
		t.Fatalf("error should name the file, got: %v", err)
	}

	// The reason has to be visible in the run's logs, not just the returned
	// error, since that is where someone looks first.
	var sawError bool
	for _, l := range emitted {
		if l.Level == "error" && strings.Contains(l.Text, "seed.sh") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected an error log naming the file, got: %+v", emitted)
	}
}

// Placement matters as much as the guard: a log step carries no file to run and
// returns before the check, so it must stay unaffected.
func TestRunStillAllowsALogStepThatNamesAFile(t *testing.T) {
	t.Parallel()

	step := StepSpec{Node: StepNode{
		ID: "note", Kind: "log", Name: "note", Message: "hello", File: "absent.sh",
	}}
	if err := NewLocal().Run(context.Background(), step, func(logstream.Line) {}); err != nil {
		t.Fatalf("a log step should not be subject to the file check: %v", err)
	}
}
