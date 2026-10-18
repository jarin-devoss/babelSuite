package execution

import (
	"path/filepath"
	"testing"

	"github.com/babelsuite/babelsuite/internal/agent"
)

// A remote backend's capacity is what its online agents declared, so the
// queue starts as many of its steps as the agents will claim.
func TestAgentCapacitySumsDeclaredSlots(t *testing.T) {
	t.Parallel()

	registry := agent.NewRegistry(agent.NewFileRuntimeStore(filepath.Join(t.TempDir(), "agents.yaml")))
	if got := agentCapacity(registry, "remote-agent"); got != 1 {
		t.Errorf("with no agents online capacity = %d, want 1", got)
	}

	registry.Register(agent.RegisterRequest{AgentID: "remote-agent", Capacity: 6})
	registry.Register(agent.RegisterRequest{AgentID: "other-agent", Capacity: 9})
	if got := agentCapacity(registry, "remote-agent"); got != 6 {
		t.Errorf("capacity = %d, want the 6 its own agent declared", got)
	}
	if got := agentCapacity(nil, "remote-agent"); got != 1 {
		t.Errorf("without a registry capacity = %d, want 1", got)
	}
}
