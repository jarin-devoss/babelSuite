package environments

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
)

// dockerDownRunner answers every docker command as a stopped daemon would.
type dockerDownRunner struct{}

func (dockerDownRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte("Cannot connect to the Docker daemon"), errors.New("exit status 1")
}

type fakeReporter struct {
	runs   []runner.ExecutionInventory
	err    error
	reaped []string
}

func (f *fakeReporter) Inventory(context.Context) ([]runner.ExecutionInventory, error) {
	return f.runs, f.err
}

func (f *fakeReporter) Reap(_ context.Context, runID string) error {
	f.reaped = append(f.reaped, runID)
	return nil
}

func sourceService(sources []Source, owned map[string]bool) *Service {
	service := newServiceForTest(dockerDownRunner{}, fakeChecker{}, func() time.Time { return time.Now().UTC() })
	service.sources = func() []Source { return sources }
	service.runOwned = func(runID string) bool { return owned[runID] }
	return service
}

// Runs on a Kubernetes backend and a remote agent are listed beside the host's
// Docker — here down, which must not hide them — and one the control plane no
// longer runs is a zombie.
func TestSnapshotListsRemoteBackends(t *testing.T) {
	cluster := &fakeReporter{runs: []runner.ExecutionInventory{{
		RunID: "run-live", Suite: "payments", Profile: "ci.yaml",
		Containers: []runner.InventoryItem{{ID: "p1", Name: "babel-run-live-db", Kind: "pod", State: "running"}},
		Networks:   []runner.InventoryItem{{ID: "s1", Name: "babel-run-live", Kind: "headless-service"}},
		Volumes:    []runner.InventoryItem{{ID: "v1", Name: "babel-run-live-workspace", Kind: "persistentvolumeclaim"}},
	}}}
	vm := &fakeReporter{runs: []runner.ExecutionInventory{{
		RunID:      "run-leaked",
		Containers: []runner.InventoryItem{{ID: "c1", Name: "babel-run-leaked-kafka", Kind: "container", State: "running"}},
	}}}
	down := &fakeReporter{err: errors.New("connection refused")}
	service := sourceService([]Source{
		{ID: "k8s", Label: "Kubernetes Pool", Kind: "kubernetes", Reporter: cluster},
		{ID: "vm", Label: "VM Agent", Kind: "remote", Reporter: vm},
		{ID: "gone", Label: "Old Agent", Kind: "remote", Reporter: down},
	}, map[string]bool{"run-live": true})

	inventory, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if inventory.DockerAvailable {
		t.Error("the host's Docker was reported available")
	}
	byID := map[string]Sandbox{}
	for _, sandbox := range inventory.Sandboxes {
		byID[sandbox.SandboxID] = sandbox
	}

	live, ok := byID["k8s:run-live"]
	if !ok {
		t.Fatalf("the cluster's run is missing: %v", inventory.Sandboxes)
	}
	if live.Status != "Running" || live.IsZombie || live.BackendLabel != "Kubernetes Pool" || live.Suite != "payments" {
		t.Errorf("cluster run = %+v", live)
	}
	if len(live.Containers) != 1 || len(live.Networks) != 1 || len(live.Volumes) != 1 {
		t.Errorf("cluster run resources = %d/%d/%d", len(live.Containers), len(live.Networks), len(live.Volumes))
	}

	leaked, ok := byID["vm:run-leaked"]
	if !ok {
		t.Fatalf("the agent's run is missing: %v", inventory.Sandboxes)
	}
	if leaked.Status != "Zombie" || leaked.OrchestratorState != "gone" || !strings.Contains(strings.Join(leaked.Warnings, " "), "VM Agent") {
		t.Errorf("leaked run = %+v", leaked)
	}
	if inventory.Summary.ActiveSandboxes != 2 || inventory.Summary.ZombieSandboxes != 1 {
		t.Errorf("summary = %+v", inventory.Summary)
	}
	if !strings.Contains(strings.Join(inventory.Warnings, " "), "Old Agent") {
		t.Errorf("the unreachable agent was not reported: %v", inventory.Warnings)
	}
}

// Reaping a remote environment goes to its backend, and the global clean
// reaches every backend even with the host's Docker down.
func TestReapRoutesToTheBackend(t *testing.T) {
	cluster := &fakeReporter{runs: []runner.ExecutionInventory{
		{RunID: "run-a", Containers: []runner.InventoryItem{{ID: "p1", Name: "a", State: "running"}}},
		{RunID: "run-b", Containers: []runner.InventoryItem{{ID: "p2", Name: "b", State: "running"}}},
	}}
	service := sourceService([]Source{{ID: "k8s", Label: "Kubernetes Pool", Kind: "kubernetes", Reporter: cluster}}, nil)

	result, err := service.ReapSandbox(context.Background(), "k8s:run-a")
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if strings.Join(cluster.reaped, ",") != "run-a" || result.RemovedContainers != 1 {
		t.Errorf("reaped %v, result %+v", cluster.reaped, result)
	}
	if _, err := service.ReapSandbox(context.Background(), "k8s:run-missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown run: err = %v, want ErrNotFound", err)
	}

	cluster.reaped = nil
	if _, err := service.ReapAll(context.Background()); err != nil {
		t.Fatalf("reap all: %v", err)
	}
	if strings.Join(cluster.reaped, ",") != "run-a,run-b" {
		t.Errorf("reap all reaped %v", cluster.reaped)
	}
}
