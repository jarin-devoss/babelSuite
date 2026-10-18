package execution

import (
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// Only a run this process booted is owned. One restored from the runtime cache
// after a restart keeps its saved status — Running — but nothing resumes it,
// so its resources on a backend are a leak, and the Environments page must
// call them a zombie.
func TestRunOwned(t *testing.T) {
	service := NewServiceWithPlatform(suites.NewWorkspaceService(), stubPlatformSource{})
	defer service.Close()

	live := runner.NewLocal(runner.BackendConfig{ID: "local"})
	service.mu.Lock()
	service.executions["run-live"] = &executionState{record: ExecutionRecord{ID: "run-live", Status: "Running"}, backend: live}
	service.executions["run-restored"] = &executionState{record: ExecutionRecord{ID: "run-restored", Status: "Running"}}
	service.executions["run-just-done"] = &executionState{record: ExecutionRecord{ID: "run-just-done", Status: "Healthy", UpdatedAt: time.Now()}, backend: live}
	service.executions["run-long-done"] = &executionState{record: ExecutionRecord{ID: "run-long-done", Status: "Failed", UpdatedAt: time.Now().Add(-time.Hour)}, backend: live}
	service.mu.Unlock()

	for id, want := range map[string]bool{
		"run-live":      true,
		"run-restored":  false,
		"run-just-done": true,
		"run-long-done": false,
		"run-unknown":   false,
	} {
		if got := service.RunOwned(id); got != want {
			t.Errorf("RunOwned(%q) = %v, want %v", id, got, want)
		}
	}
}
