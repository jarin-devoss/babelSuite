package agent

import (
	"path/filepath"
	"testing"
)

func TestRegistryPersistsRegistrationAndHeartbeat(t *testing.T) {
	store := NewFileRuntimeStore(filepath.Join(t.TempDir(), "agents.yaml"))
	registry := NewRegistry(store)

	record := registry.Register(RegisterRequest{
		AgentID:      "worker-1",
		Name:         "Worker 1",
		HostURL:      "http://127.0.0.1:8091",
		Capabilities: []string{"service", "test"},
	})
	if record.AgentID != "worker-1" {
		t.Fatalf("expected agent id to round-trip, got %q", record.AgentID)
	}

	heartbeat, ok := registry.Heartbeat("worker-1")
	if !ok {
		t.Fatal("expected heartbeat to find the persisted worker")
	}
	if heartbeat.LastHeartbeatAt.IsZero() {
		t.Fatal("expected heartbeat timestamp to be recorded")
	}

	state, err := store.Load()
	if err != nil {
		t.Fatalf("load runtime state: %v", err)
	}
	if len(state.Agents) == 0 {
		t.Fatal("expected registered worker to be persisted into platform settings")
	}

	var worker Registration
	found := false
	for _, agent := range state.Agents {
		if agent.AgentID == "worker-1" {
			worker = agent
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find persisted worker agent")
	}
	if worker.LastHeartbeatAt.IsZero() {
		t.Fatal("expected last heartbeat to be persisted")
	}
	if len(worker.Capabilities) != 2 {
		t.Fatalf("expected runtime capabilities to be persisted, got %v", worker.Capabilities)
	}
}

// An agent declares how many steps it runs at once when it registers; one
// that predates capacity counts as 1.
func TestRegistryKeepsDeclaredCapacity(t *testing.T) {
	registry := NewRegistry(NewFileRuntimeStore(filepath.Join(t.TempDir(), "agents.yaml")))

	registry.Register(RegisterRequest{AgentID: "gpu-box", Capacity: 4})
	registry.Register(RegisterRequest{AgentID: "old-box"})

	got := map[string]int{}
	for _, reg := range registry.ListRegistrations() {
		got[reg.AgentID] = reg.SlotCount()
	}
	if got["gpu-box"] != 4 || got["old-box"] != 1 {
		t.Fatalf("slots = %v, want gpu-box 4 and old-box 1", got)
	}

	// Re-registering with a new capacity replaces the old one.
	registry.Register(RegisterRequest{AgentID: "gpu-box", Capacity: 2})
	for _, reg := range registry.ListRegistrations() {
		if reg.AgentID == "gpu-box" && reg.SlotCount() != 2 {
			t.Fatalf("re-registered capacity = %d, want 2", reg.SlotCount())
		}
	}
}
