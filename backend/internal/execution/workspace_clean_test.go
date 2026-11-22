package execution

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/suites"
)

func TestCleanStaleWorkspacesRemovesOnlyWhatNothingNeeds(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	t.Setenv("TMPDIR", temp)

	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.mu.Lock()
	service.executions["run-finished"] = &executionState{record: ExecutionRecord{ID: "run-finished", Status: "Healthy"}}
	service.executions["run-interrupted"] = &executionState{record: ExecutionRecord{ID: "run-interrupted", Status: StatusInterrupted}}
	service.executions["run-active"] = &executionState{record: ExecutionRecord{ID: "run-active", Status: "Booting"}}
	service.mu.Unlock()

	base := filepath.Join(temp, "babel-workspace")
	for _, name := range []string{"run-finished", "run-interrupted", "run-active", "run-unknown-old", "run-unknown-new"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(base, "run-unknown-old"), old, old); err != nil {
		t.Fatal(err)
	}

	if removed := service.CleanStaleWorkspaces(); removed != 3 {
		t.Fatalf("removed %d workspaces, want 3", removed)
	}
	for name, wantKept := range map[string]bool{
		"run-finished": false, "run-interrupted": false, "run-active": true, "run-unknown-old": false, "run-unknown-new": true,
	} {
		_, err := os.Stat(filepath.Join(base, name))
		if kept := err == nil; kept != wantKept {
			t.Errorf("%s: kept=%v, want %v", name, kept, wantKept)
		}
	}
}
