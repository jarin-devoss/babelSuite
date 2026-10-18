package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
)

// A service started under an execution's scope keeps running after its job,
// and stops only when the execution is released.
func TestExecutionScopesLastUntilRelease(t *testing.T) {
	scopes := newExecutionScopes()
	ctx := scopes.context("run-1")
	if scopes.context("run-1") != ctx {
		t.Fatal("a second step of the execution got a different scope")
	}
	other := scopes.context("run-2")

	scopes.release("run-1")
	if ctx.Err() == nil {
		t.Fatal("release left the execution's scope running")
	}
	if other.Err() != nil {
		t.Fatal("releasing run-1 ended run-2's scope")
	}
	if scopes.context("run-1") == ctx {
		t.Fatal("the released scope was handed out again")
	}
	scopes.release("unknown")
}

// An agent started while the control plane is down registers once it is up,
// and registers again after a heartbeat fails.
func TestRegisterAndHeartbeatRetries(t *testing.T) {
	var up atomic.Bool
	var registrations, heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/register"):
			registrations.Add(1)
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			if heartbeats.Add(1) == 2 {
				http.Error(w, "agent not found", http.StatusNotFound)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := agent.NewControlPlaneClient(server.URL, server.Client(), "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	register := func(ctx context.Context) error {
		return client.Register(ctx, agent.RegisterRequest{AgentID: "a", Capacity: 4})
	}
	var firstRegistrations atomic.Int32
	go registerAndHeartbeat(ctx, client, "a", 20*time.Millisecond, register, func() { firstRegistrations.Add(1) })

	time.Sleep(60 * time.Millisecond)
	if registrations.Load() != 0 {
		t.Fatal("registered while the control plane was down")
	}
	up.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for registrations.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if registrations.Load() < 2 {
		t.Fatalf("registrations = %d, want a retry once up and another after the failed heartbeat", registrations.Load())
	}
	// The claim loops start on the first registration only.
	if firstRegistrations.Load() != 1 {
		t.Fatalf("first-registration callbacks = %d, want 1", firstRegistrations.Load())
	}
}

// Shutdown destroys every execution the agent ran a step for, not only those
// with services.
func TestExecutionScopesReleaseAll(t *testing.T) {
	scopes := newExecutionScopes()
	scopes.track("run-task-only")
	service := scopes.context("run-with-service")

	if ids := scopes.releaseAll(); len(ids) != 2 {
		t.Fatalf("released %v, want both executions", ids)
	}
	if service.Err() == nil {
		t.Fatal("the service scope survived releaseAll")
	}
	if len(scopes.releaseAll()) != 0 {
		t.Fatal("a second releaseAll found executions again")
	}
}
