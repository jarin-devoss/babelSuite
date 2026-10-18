package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

// One failed lease renewal must not stop a step: the agent retries, and gives
// the step up only once the lease has run out.
func TestWorkerRetriesAFailedLeaseRenewal(t *testing.T) {
	var renewals, failures atomic.Int32
	failFor := func(n int32) bool { return renewals.Add(1) <= n }
	var failing atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/lease") {
			if failFor(failing.Load()) {
				failures.Add(1)
				http.Error(w, "busy", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"running"}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	run := func(ttl, stepTime time.Duration) error {
		var stepErr error
		service := NewService(Info{AgentID: "worker-1"}, ExecutorFunc(func(ctx context.Context, _ StepRequest, _ func(logstream.Line)) error {
			select {
			case <-ctx.Done():
				stepErr = ctx.Err()
			case <-time.After(stepTime):
			}
			return stepErr
		}))
		worker := NewWorker("worker-1", time.Second, NewControlPlaneClient(server.URL, server.Client(), ""), service)
		worker.runAssignment(context.Background(), StepRequest{JobID: "run-1:step", ExecutionID: "run-1", LeaseTTL: ttl})
		return stepErr
	}

	// The first renewal fails, the next succeeds well inside the lease.
	failing.Store(1)
	if err := run(400*time.Millisecond, 700*time.Millisecond); err != nil {
		t.Fatalf("a single failed renewal stopped the step: %v", err)
	}
	if failures.Load() != 1 {
		t.Fatalf("failed renewals = %d, want 1", failures.Load())
	}

	// Renewals keep failing past the lease: the step is stopped.
	renewals.Store(0)
	failing.Store(1000)
	if err := run(200*time.Millisecond, 2*time.Second); err == nil {
		t.Fatal("the step outlived a lapsed lease")
	}
}

// A claimed step whose agent goes quiet fails as an expired lease, naming the
// agent, rather than as a bare deadline.
func TestCoordinatorReportsAnExpiredLease(t *testing.T) {
	coordinator := NewCoordinator(NewRegistry(nil), nil)
	defer coordinator.Close()

	submitted, err := coordinator.Submit(StepRequest{JobID: "run-1:db", ExecutionID: "run-1", BackendID: "vm-agent", LeaseTTL: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, ok := coordinator.Claim("vm-agent"); !ok {
		t.Fatal("claim failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = coordinator.Wait(ctx, submitted.JobID)
	if !errors.Is(err, ErrLeaseExpired) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "vm-agent") {
		t.Fatalf("err = %v, want ErrLeaseExpired naming vm-agent", err)
	}
}
