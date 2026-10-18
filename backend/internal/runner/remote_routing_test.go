package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

type countingDispatcher struct {
	kinds    []string
	requests []agent.StepRequest
}

func (d *countingDispatcher) IsAvailable(context.Context) bool { return true }

func (d *countingDispatcher) Dispatch(_ context.Context, request agent.StepRequest, _ func(logstream.Line)) error {
	d.kinds = append(d.kinds, request.Node.Kind)
	d.requests = append(d.requests, request)
	return nil
}

// Only container steps go to the agent. A plugin step sent there arrived
// without its plugin call and crashed the agent with every job it held.
func TestRemoteSendsOnlyContainerStepsToTheAgent(t *testing.T) {
	t.Parallel()

	dispatcher := &countingDispatcher{}
	remote := NewRemote(RemoteConfig{BackendConfig: BackendConfig{ID: "remote-agent"}, Dispatcher: dispatcher})
	emit := func(logstream.Line) {}

	for _, node := range []StepNode{
		{ID: "say", Name: "say", Kind: "log", Message: "hi"},
		{ID: "api", Name: "api", Kind: "mock"},
		{ID: "migrate", Name: "migrate", Kind: "task", Image: "alpine", Commands: []string{"true"}},
	} {
		if err := remote.Run(context.Background(), StepSpec{ExecutionID: "run-1", Node: node}, emit); err != nil {
			t.Errorf("%s step: %v", node.Kind, err)
		}
	}
	if len(dispatcher.kinds) != 1 || dispatcher.kinds[0] != "task" {
		t.Errorf("dispatched kinds = %v, want only the task", dispatcher.kinds)
	}

	// A plugin step with no sidecar fails with a reason instead of panicking.
	err := remote.Run(context.Background(), StepSpec{ExecutionID: "run-1",
		Node:   StepNode{ID: "judge", Name: "judge", Kind: "plugin"},
		Plugin: &suites.PluginSpec{Name: "llm-judge", Op: "judge"}}, emit)
	if err == nil || !strings.Contains(err.Error(), "sidecar is not running") {
		t.Errorf("plugin without sidecar: err = %v", err)
	}
	err = remote.Run(context.Background(), StepSpec{ExecutionID: "run-1",
		Node: StepNode{ID: "judge", Name: "judge", Kind: "plugin"}}, emit)
	if err == nil || !strings.Contains(err.Error(), "carries no plugin call") {
		t.Errorf("plugin without a call: err = %v", err)
	}
	if len(dispatcher.kinds) != 1 {
		t.Errorf("plugin steps reached the agent: %v", dispatcher.kinds)
	}
}

// The agent names a step's container on the execution network by the aliases
// the control plane resolved. Without them both composed suites' "db" took the
// bare name on the agent, and a seed wrote to one database and read the other.
func TestRemoteForwardsNetworkAliases(t *testing.T) {
	t.Parallel()

	dispatcher := &countingDispatcher{}
	remote := NewRemote(RemoteConfig{BackendConfig: BackendConfig{ID: "remote-agent"}, Dispatcher: dispatcher})
	err := remote.Run(context.Background(), StepSpec{
		ExecutionID:    "run-1",
		NetworkAliases: []string{"identity-broker-db"},
		Node:           StepNode{ID: "identity-broker/db", Name: "identity-broker/db", Kind: "service", Image: "postgres:16"},
	}, func(logstream.Line) {})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(dispatcher.requests) != 1 {
		t.Fatalf("dispatched %d requests, want 1", len(dispatcher.requests))
	}
	if got := dispatcher.requests[0].NetworkAliases; len(got) != 1 || got[0] != "identity-broker-db" {
		t.Errorf("aliases = %v, want [identity-broker-db]", got)
	}
	if dispatcher.requests[0].LeaseTTL < remoteLeaseTTL {
		t.Errorf("lease = %v, want at least %v", dispatcher.requests[0].LeaseTTL, remoteLeaseTTL)
	}
}

func TestRewriteLoopbackURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ raw, host, want string }{
		{"http://127.0.0.1:51536", "10.0.2.2", "http://10.0.2.2:51536"},
		{"http://localhost:9080/mocks", "10.0.2.2", "http://10.0.2.2:9080/mocks"},
		{"http://agent-model:9080", "10.0.2.2", "http://agent-model:9080"},
		{"http://127.0.0.1:51536", "", "http://127.0.0.1:51536"},
	} {
		if got := RewriteLoopbackURL(tc.raw, tc.host); got != tc.want {
			t.Errorf("RewriteLoopbackURL(%q, %q) = %q, want %q", tc.raw, tc.host, got, tc.want)
		}
	}
}
