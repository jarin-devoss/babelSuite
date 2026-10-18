package runner

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The Environments page lists resources labelled babelsuite.managed=true and
// groups them by babelsuite.run_id; a run's containers and network must carry
// both, and the orchestrator's pid, or the page shows nothing.
func TestManagedLabels(t *testing.T) {
	labels := managedLabels("run-1", map[string]string{labelSuite: "payments", labelProfile: ""})
	if labels[labelManaged] != "true" || labels[labelRunID] != "run-1" {
		t.Fatalf("labels = %v, want managed run-1", labels)
	}
	if labels[labelOrchestratorPID] != strconv.Itoa(os.Getpid()) {
		t.Errorf("orchestrator pid = %q, want this process", labels[labelOrchestratorPID])
	}
	if labels[labelSuite] != "payments" {
		t.Errorf("suite label = %q", labels[labelSuite])
	}
	if _, ok := labels[labelProfile]; ok {
		t.Error("an empty profile was written as a label")
	}
}

// A shared sidecar is its own environment, never one run's, and outlives the
// control plane, so it is not tied to the pid that started it.
func TestSidecarLabels(t *testing.T) {
	labels := sidecarLabels("returns-control-plane", "local.yaml")
	id := labels[labelRunID]
	if !strings.HasPrefix(id, "sidecar-") || strings.HasPrefix(id, "run-") {
		t.Fatalf("sidecar run id = %q", id)
	}
	if labels[labelManaged] != "true" {
		t.Error("sidecar is not marked managed")
	}
	if _, ok := labels[labelOrchestratorPID]; ok {
		t.Error("sidecar carries an orchestrator pid; a restarted control plane would call it a zombie")
	}
}
