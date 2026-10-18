package runner

import (
	"os"
	"strconv"
)

// The labels the Environments page (internal/environments) inventories Docker
// by. It lists only resources marked managed, groups them into an environment
// by run id, and calls a group a zombie when the process that created it is
// gone while its containers still run — the leak a crashed control plane
// leaves. Nothing set these before, so the page showed no environment at all.
const (
	labelManaged         = "babelsuite.managed"
	labelRunID           = "babelsuite.run_id"
	labelOrchestratorPID = "babelsuite.orchestrator_pid"
	labelSuite           = "babelsuite.suite"
	labelProfile         = "babelsuite.profile"
)

// managedLabels marks a resource as belonging to runID, created by this
// process, and merges in extra.
func managedLabels(runID string, extra map[string]string) map[string]string {
	labels := map[string]string{
		labelManaged:         "true",
		labelRunID:           runID,
		labelOrchestratorPID: strconv.Itoa(os.Getpid()),
	}
	for key, value := range extra {
		if value != "" {
			labels[key] = value
		}
	}
	return labels
}

// sidecarLabels marks a suite's shared APISIX sidecar. It carries no
// orchestrator pid: the sidecar is meant to outlive the control plane — a
// restarted one finds it running and reuses it — so the pid of whichever
// process started it says nothing about whether it is abandoned, and with it
// every restart reported each sidecar as a zombie.
func sidecarLabels(suiteID, profile string) map[string]string {
	labels := managedLabels(sidecarRunID(suiteID, profile), map[string]string{
		labelSuite:        suiteID,
		labelProfile:      profile,
		"babelsuite.kind": "apisix-sidecar",
	})
	delete(labels, labelOrchestratorPID)
	return labels
}

// sidecarRunID groups a suite's APISIX sidecar on its own. The sidecar serves
// every run of the suite and profile, so it must not sit in any one run's
// environment, where reaping that run would take it from the others.
func sidecarRunID(suiteID, profile string) string {
	return "sidecar-" + sanitizeID(suiteID) + "-" + sanitizeID(profile)
}
