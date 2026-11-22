package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

func loadStep(execution, name string) StepSpec {
	return StepSpec{ExecutionID: execution, SuiteID: "lock-suite", Profile: "local.yaml", Node: StepNode{ID: name, Name: name}}
}

func TestTwoLoadStepsOnOneSidecarTakeTurns(t *testing.T) {
	var firstLog, secondLog []string
	emitTo := func(into *[]string) func(logstream.Line) {
		return func(l logstream.Line) { *into = append(*into, l.Text) }
	}

	releaseFirst, err := acquireSidecarLoad(context.Background(), loadStep("run-1", "cannon"), emitTo(&firstLog))
	if err != nil {
		t.Fatal(err)
	}

	got := make(chan func(), 1)
	go func() {
		release, err := acquireSidecarLoad(context.Background(), loadStep("run-2", "flood"), emitTo(&secondLog))
		if err != nil {
			t.Error(err)
			return
		}
		got <- release
	}()

	select {
	case <-got:
		t.Fatal("the second load step started while the first still held the sidecar")
	case <-time.After(150 * time.Millisecond):
	}

	releaseFirst()
	select {
	case release := <-got:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("the second load step never started after the first finished")
	}

	if len(firstLog) != 0 {
		t.Errorf("a step that did not wait must not log a wait: %v", firstLog)
	}
	joined := strings.Join(secondLog, "\n")
	if !strings.Contains(joined, "Waiting for run-1/cannon") || !strings.Contains(joined, "The sidecar is free after") {
		t.Errorf("the waiting step must say whom it waited for and for how long:\n%s", joined)
	}
}

func TestLoadStepsOnDifferentSidecarsDoNotWait(t *testing.T) {
	a := loadStep("run-1", "a")
	b := loadStep("run-2", "b")
	b.SuiteID = "another-suite"

	releaseA, err := acquireSidecarLoad(context.Background(), a, func(logstream.Line) {})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()

	done := make(chan struct{})
	go func() {
		release, err := acquireSidecarLoad(context.Background(), b, func(logstream.Line) {})
		if err == nil {
			release()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a load step waited for another suite's sidecar")
	}
}

func TestACancelledLoadStepStopsWaiting(t *testing.T) {
	release, err := acquireSidecarLoad(context.Background(), loadStep("run-1", "holder"), func(logstream.Line) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = acquireSidecarLoad(ctx, loadStep("run-2", "waiter"), func(logstream.Line) {})
	if err == nil || !strings.Contains(err.Error(), "run-1/holder") {
		t.Fatalf("a cancelled waiter must fail naming the holder, got %v", err)
	}
	release()

	again, err := acquireSidecarLoad(context.Background(), loadStep("run-3", "next"), func(logstream.Line) {})
	if err != nil {
		t.Fatalf("the slot was lost to a cancelled waiter: %v", err)
	}
	again()
}
