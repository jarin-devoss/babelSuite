package execution

import (
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func persistedRun(id, status, workspace, owner string, lease *time.Time) PersistedExecution {
	return PersistedExecution{
		Record: ExecutionRecord{
			ID:            id,
			Suite:         ExecutionSuite{ID: "payment-suite", Title: "Payment Suite", Topology: []suites.TopologyNode{{ID: "db", Name: "db", Kind: "service"}, {ID: "api", Name: "api", Kind: "service"}}},
			Profile:       "local.yaml",
			Status:        status,
			StartedAt:     time.Now().UTC().Add(-time.Minute),
			UpdatedAt:     time.Now().UTC().Add(-time.Second),
			WorkspaceID:   workspace,
			OwnerInstance: owner,
			LeaseUntil:    lease,
			Events: []ExecutionEvent{
				{ID: "db-start", Source: "db", Text: "[db] up", Status: "running"},
			},
		},
		Total: 2,
		Logs:  []logstream.Line{{Source: "db", Level: "info", Text: "restored"}},
	}
}

func at(offset time.Duration) *time.Time {
	t := time.Now().UTC().Add(offset)
	return &t
}

// A run that was saved before a restart came back with no workspace, and every
// signed-in read filters on the workspace, so the run answered 404 and the list
// was empty (FINDINGS 6.2).
func TestARestoredExecutionStaysVisibleToItsWorkspace(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.ConfigureRuntimeStore(&stubRuntimeStore{executions: []PersistedExecution{
		persistedRun("run-done", "Healthy", "ws-1", "", nil),
	}})

	if _, err := service.GetExecution("run-done", "ws-1"); err != nil {
		t.Fatalf("the workspace that started the run cannot read it after a restart: %v", err)
	}
	if _, err := service.GetExecution("run-done", "ws-2"); err == nil {
		t.Fatal("another workspace can read the run")
	}
	if list, _ := service.ListExecutions("ws-1", 0, 10); len(list) != 1 {
		t.Fatalf("the workspace's list has %d runs after a restart, want 1", len(list))
	}
	if list, _ := service.ListExecutions("ws-2", 0, 10); len(list) != 0 {
		t.Fatalf("another workspace's list has %d runs, want 0", len(list))
	}

	record, _ := service.GetExecution("run-done", "ws-1")
	if record.WorkspaceID != "" || record.OwnerInstance != "" || record.LeaseUntil != nil {
		t.Fatalf("ownership bookkeeping must not be shown to clients: %+v", record)
	}
}

func TestAnUnfinishedRunOfAStoppedControlPlaneIsInterrupted(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	store := &stubRuntimeStore{executions: []PersistedExecution{
		persistedRun("run-dead", "Booting", "ws-1", "cp-gone", at(-time.Minute)),
	}}
	service.ConfigureRuntimeStore(store)

	record, err := service.GetExecution("run-dead", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusInterrupted {
		t.Fatalf("status = %q, want %q", record.Status, StatusInterrupted)
	}
	var said bool
	for _, event := range record.Events {
		if event.ID == "interrupted" && strings.Contains(event.Text, "its control plane stopped") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the run must say why it was interrupted: %+v", record.Events)
	}

	// The step that was running failed, the one that never started did not run.
	statuses := map[string]string{}
	for _, event := range record.Events {
		statuses[event.Source] = event.Status
	}
	if statuses["db"] != "failed" || statuses["api"] != "skipped" {
		t.Fatalf("steps after the interruption: db=%q api=%q, want failed and skipped", statuses["db"], statuses["api"])
	}

	// The next control plane reads the same answer from the store.
	var saved string
	for _, item := range store.executions {
		if item.Record.ID == "run-dead" {
			saved = item.Record.Status
		}
	}
	if saved != StatusInterrupted {
		t.Fatalf("the store still has the run as %q", saved)
	}
}

func TestARunWhoseOwnerIsStillRenewingItsLeaseIsLeftAlone(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.ConfigureRuntimeStore(&stubRuntimeStore{executions: []PersistedExecution{
		persistedRun("run-live", "Booting", "ws-1", "cp-other", at(time.Minute)),
	}})

	record, err := service.GetExecution("run-live", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "Booting" {
		t.Fatalf("a run another control plane is still driving became %q", record.Status)
	}

	// Once its lease runs out with nobody renewing it, it is over.
	if ended := service.interruptOrphans(time.Now().Add(2 * time.Minute)); ended != 1 {
		t.Fatalf("interrupted %d runs after the lease ran out, want 1", ended)
	}
}

func TestARunSavedWithNoOwnerIsInterrupted(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.ConfigureRuntimeStore(&stubRuntimeStore{executions: []PersistedExecution{
		persistedRun("run-old", "Booting", "ws-1", "", nil),
	}})
	record, err := service.GetExecution("run-old", "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusInterrupted {
		t.Fatalf("status = %q, want %q", record.Status, StatusInterrupted)
	}
}

func TestAControlPlaneNeverInterruptsItsOwnRuns(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.mu.Lock()
	state := &executionState{record: persistedRun("run-mine", "Booting", "ws-1", "", nil).Record, stepStatus: map[string]string{}}
	service.claimLocked(&state.record, time.Now())
	service.executions["run-mine"] = state
	service.order = append(service.order, "run-mine")
	service.mu.Unlock()

	// Even a lease that has run out: the owner renews it, so it is only late.
	service.mu.Lock()
	past := time.Now().Add(-time.Hour)
	state.record.LeaseUntil = &past
	service.mu.Unlock()
	if ended := service.interruptOrphans(time.Now()); ended != 0 {
		t.Fatalf("a control plane interrupted %d of its own runs", ended)
	}

	service.renewLeases(time.Now())
	service.mu.Lock()
	renewed := state.record.LeaseUntil != nil && state.record.LeaseUntil.After(time.Now())
	service.mu.Unlock()
	if !renewed {
		t.Fatal("renewLeases did not extend the lease of a run this control plane owns")
	}

	// An orderly shutdown ends them, and says so.
	if ended := service.interruptOwned(time.Now()); ended != 1 {
		t.Fatalf("shutdown interrupted %d runs, want 1", ended)
	}
	record, _ := service.GetExecution("run-mine", "")
	if record.Status != StatusInterrupted {
		t.Fatalf("status after shutdown = %q", record.Status)
	}
}

func TestRenewingLeasesLeavesOtherControlPlanesRunsAlone(t *testing.T) {
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()
	service.ConfigureRuntimeStore(&stubRuntimeStore{executions: []PersistedExecution{
		persistedRun("run-other", "Booting", "ws-1", "cp-other", at(5*time.Second)),
	}})
	service.renewLeases(time.Now())
	service.mu.Lock()
	lease := service.executions["run-other"].record.LeaseUntil
	service.mu.Unlock()
	if lease == nil || lease.After(time.Now().Add(10*time.Second)) {
		t.Fatalf("this control plane extended another one's lease: %v", lease)
	}
}

func TestStatusHelpers(t *testing.T) {
	for status, terminal := range map[string]bool{"Booting": false, "Healthy": true, "Failed": true, "Interrupted": true, "": false} {
		if IsTerminalStatus(status) != terminal {
			t.Errorf("IsTerminalStatus(%q) = %v", status, !terminal)
		}
	}
	if !IsFailureStatus("Interrupted") || !IsFailureStatus("Failed") || IsFailureStatus("Healthy") {
		t.Error("IsFailureStatus is wrong")
	}
}
