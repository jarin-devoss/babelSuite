package environments

import (
	"context"
	"strings"
	"testing"
	"time"
)

const leakedNetworkInspect = `[
  {
    "Id": "network-a",
    "Name": "babel-net-run-dead",
    "Driver": "bridge",
    "Scope": "local",
    "Labels": {
      "babelsuite.managed": "true",
      "babelsuite.run_id": "run-dead",
      "babelsuite.suite": "Payment Suite",
      "babelsuite.orchestrator_pid": "5012"
    },
    "Containers": {
      "sidecar-id": { "Name": "babel-sidecar-payment-suite-local-yaml" }
    }
  }
]`

func leakedNetworkRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{
		cmd("docker", "ps", "-aq", "--filter", "label=babelsuite.managed=true"):           "",
		cmd("docker", "network", "ls", "-q", "--filter", "label=babelsuite.managed=true"): "network-a\n",
		cmd("docker", "network", "inspect", "network-a"):                                  leakedNetworkInspect,
		cmd("docker", "volume", "ls", "-q", "--filter", "label=babelsuite.managed=true"):  "",
		cmd("docker", "network", "disconnect", "-f", "network-a", "sidecar-id"):           "",
		cmd("docker", "network", "rm", "network-a"):                                       "",
	}}
}

// A run that was killed left its network behind, and with no running
// container in it the network was only ever "Residual" (FINDINGS 6.2).
func TestANetworkLeftByAKilledRunIsAZombie(t *testing.T) {
	t.Parallel()
	service := newServiceForTest(leakedNetworkRunner(), fakeChecker{}, func() time.Time { return time.Now().UTC() })

	inventory, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sandboxes) != 1 || !inventory.Sandboxes[0].IsZombie || inventory.Sandboxes[0].Status != "Zombie" {
		t.Fatalf("a dead run's leftover network must be a zombie: %+v", inventory.Sandboxes)
	}
}

func TestANetworkOfARunWhoseOrchestratorIsAliveIsNotAZombie(t *testing.T) {
	t.Parallel()
	service := newServiceForTest(leakedNetworkRunner(), fakeChecker{alive: map[int]bool{5012: true}}, func() time.Time { return time.Now().UTC() })

	inventory, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sandboxes) != 1 || inventory.Sandboxes[0].IsZombie {
		t.Fatalf("a run another control plane still drives was called a zombie: %+v", inventory.Sandboxes)
	}
}

// The suite's sidecar joins every run's network, and Docker will not remove a
// network that still has an endpoint on it.
func TestReapingZombiesDetachesTheSharedSidecarBeforeRemovingTheNetwork(t *testing.T) {
	t.Parallel()
	runner := leakedNetworkRunner()
	service := newServiceForTest(runner, fakeChecker{}, func() time.Time { return time.Now().UTC() })

	result, err := service.ReapZombies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedNetworks != 1 {
		t.Fatalf("removed %d networks, want 1: %+v", result.RemovedNetworks, result)
	}

	calls := strings.Join(runner.calls, "\n")
	disconnect := strings.Index(calls, cmd("docker", "network", "disconnect", "-f", "network-a", "sidecar-id"))
	remove := strings.Index(calls, cmd("docker", "network", "rm", "network-a"))
	if disconnect < 0 || remove < 0 || disconnect > remove {
		t.Fatalf("the sidecar must be detached before the network is removed:\n%s", calls)
	}
	if strings.Contains(calls, "rm -f sidecar-id") {
		t.Fatalf("the shared sidecar must never be removed with a run:\n%s", calls)
	}
}

func TestReapingZombiesLeavesLiveRunsAlone(t *testing.T) {
	t.Parallel()
	runner := leakedNetworkRunner()
	service := newServiceForTest(runner, fakeChecker{alive: map[int]bool{5012: true}}, func() time.Time { return time.Now().UTC() })

	if _, err := service.ReapZombies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(runner.calls, "\n"), "network rm") {
		t.Fatalf("a live run's network was removed:\n%s", strings.Join(runner.calls, "\n"))
	}
}
