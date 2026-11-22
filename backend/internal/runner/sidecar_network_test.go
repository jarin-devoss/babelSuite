package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
)

// The sidecar serves every run of a suite, so the address its clients were
// given must survive a run attaching it to that run's network. The run network
// is named babel-net-…, which sorts before the default bridge, and an engine
// then gives it the sidecar's default gateway; a host port Docker picked at
// random can move with it. The test names the run so that this is the case on
// every engine.
func TestSidecarKeepsItsAddressWhenItJoinsARunNetwork(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil || !pingDocker(context.Background()) {
		t.Skip("docker is not available")
	}
	const suiteID, profile, executionID = "sidecar-keeps-address", "local.yaml", "aaa-sidecar-keeps-address"
	remove := func() {
		_ = exec.Command("docker", "rm", "-f", SidecarContainerName(suiteID, profile)).Run()
		RemoveExecutionNetwork(executionID)
	}
	remove()
	t.Cleanup(remove)

	if err := EnsureSuiteSidecar(apisix.SuiteConfig{ID: suiteID}, profile, "apache/apisix:3.9.0-debian", ""); err != nil {
		t.Fatalf("start the sidecar: %v", err)
	}
	before := SuiteSidecarURL(suiteID, profile)
	if before == "" {
		t.Fatal("the sidecar is not published on a host port")
	}
	if err := waitForSidecarAnswer(before, 60*time.Second); err != nil {
		t.Fatalf("the sidecar never answered at %s: %v\n%s", before, err, sidecarDiagnostics(suiteID, profile))
	}

	if err := SetupExecutionNetwork(executionID); err != nil {
		t.Fatalf("create the run network: %v", err)
	}
	if err := AttachSidecarToExecution(context.Background(), suiteID, profile, executionID); err != nil {
		t.Fatalf("attach the sidecar: %v", err)
	}

	after := SuiteSidecarURL(suiteID, profile)
	if after != before {
		t.Fatalf("the sidecar's address moved from %s to %s when it joined the run network\n%s", before, after, sidecarDiagnostics(suiteID, profile))
	}
	if err := waitForSidecarAnswer(before, 10*time.Second); err != nil {
		t.Fatalf("the sidecar stopped answering at %s after joining the run network: %v\n%s", before, err, sidecarDiagnostics(suiteID, profile))
	}

	RemoveExecutionNetwork(executionID)
	if err := waitForSidecarAnswer(before, 10*time.Second); err != nil {
		t.Fatalf("the sidecar stopped answering at %s after leaving the run network: %v\n%s", before, err, sidecarDiagnostics(suiteID, profile))
	}
}

// waitForSidecarAnswer returns once the sidecar's scanner route answers at all.
func waitForSidecarAnswer(base string, within time.Duration) error {
	deadline := time.Now().Add(within)
	var last error
	for {
		resp, err := http.Post(base+apisix.AttackScannerTriggerRoute, "application/json", strings.NewReader("{}"))
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusBadRequest {
				return nil
			}
			last = fmt.Errorf("answered HTTP %d, want 400", resp.StatusCode)
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func sidecarDiagnostics(suiteID, profile string) string {
	name := SidecarContainerName(suiteID, profile)
	version, _ := exec.Command("docker", "version", "--format", "engine {{.Server.Version}} api {{.Server.APIVersion}}").CombinedOutput()
	state, _ := exec.Command("docker", "inspect", "--format", "{{.State.Status}} ports={{json .NetworkSettings.Ports}} networks={{json .NetworkSettings.Networks}}", name).CombinedOutput()
	logs, _ := exec.Command("docker", "logs", "--tail", "20", name).CombinedOutput()
	return fmt.Sprintf("%s%s\n%s", version, state, logs)
}
