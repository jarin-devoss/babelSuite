package execution

import (
	"fmt"
	"time"
)

// StatusInterrupted is the status of an execution that was running when the
// control plane that drove it stopped. Nothing resumes it, so it is over; it is
// reported as such instead of staying Booting for ever.
const StatusInterrupted = "Interrupted"

const (
	// leaseTTL is how long an execution stays claimed by the control plane that
	// runs it without that control plane saying so again.
	leaseTTL = 20 * time.Second
	// leaseRenewEvery is how often the owner says so. Several renewals fit in
	// one TTL, so one slow write does not look like a stopped control plane.
	leaseRenewEvery = 5 * time.Second
)

// IsTerminalStatus reports whether an execution with this status will not change
// again.
func IsTerminalStatus(status string) bool {
	switch status {
	case "Healthy", "Failed", StatusInterrupted:
		return true
	}
	return false
}

// IsFailureStatus reports whether a finished execution did not succeed.
func IsFailureStatus(status string) bool {
	return status == "Failed" || status == StatusInterrupted
}

// claimLocked marks the record as driven by this control plane until the lease
// runs out. Must be called with s.mu held.
func (s *Service) claimLocked(record *ExecutionRecord, now time.Time) {
	until := now.Add(leaseTTL)
	record.OwnerInstance = s.instanceID
	record.LeaseUntil = &until
}

// startLeaseLoop renews the lease of every execution this control plane runs,
// and interrupts any whose owner stopped renewing, until the service closes.
func (s *Service) startLeaseLoop() {
	go func() {
		ticker := time.NewTicker(leaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case now := <-ticker.C:
				s.renewLeases(now)
				s.interruptOrphans(now)
			}
		}
	}()
}

// renewLeases extends the lease of every unfinished execution this control
// plane owns.
func (s *Service) renewLeases(now time.Time) {
	s.mu.Lock()
	renewed := false
	for _, item := range s.executions {
		if item == nil || item.record.OwnerInstance != s.instanceID || IsTerminalStatus(item.record.Status) {
			continue
		}
		s.claimLocked(&item.record, now)
		renewed = true
	}
	s.mu.Unlock()
	if renewed {
		s.schedulePersist()
	}
}

// interruptOrphans ends every unfinished execution whose owner is gone: it
// belongs to another control plane, or to none, and its lease has run out. An
// execution this control plane runs is never touched here. It returns how many
// it ended.
func (s *Service) interruptOrphans(now time.Time) int {
	return s.interruptWhere(now, func(item *executionState) string {
		if item.record.OwnerInstance == s.instanceID {
			return ""
		}
		if item.record.LeaseUntil != nil && now.Before(*item.record.LeaseUntil) {
			return "" // its owner is alive
		}
		if item.record.OwnerInstance == "" {
			return "it was saved by a control plane that did not record its owner"
		}
		return "its control plane stopped"
	})
}

// interruptOwned ends every unfinished execution this control plane runs, for a
// control plane that is shutting down in an orderly way. The next one then finds
// them already over instead of waiting for their leases to run out.
func (s *Service) interruptOwned(now time.Time) int {
	return s.interruptWhere(now, func(item *executionState) string {
		if item.record.OwnerInstance != s.instanceID {
			return ""
		}
		return "the control plane was shut down"
	})
}

// interruptWhere ends every unfinished execution reasonFor gives a reason for.
func (s *Service) interruptWhere(now time.Time, reasonFor func(item *executionState) string) int {
	type interrupted struct {
		id, text string
		steps    map[string]string
	}
	var ended []interrupted

	s.mu.Lock()
	for id, item := range s.executions {
		if item == nil || IsTerminalStatus(item.record.Status) {
			continue
		}
		reason := reasonFor(item)
		if reason == "" {
			continue
		}
		item.record.Status = StatusInterrupted
		item.record.OwnerInstance = ""
		item.record.LeaseUntil = nil
		item.record.UpdatedAt = now.UTC()
		s.ensureStepStatusLocked(item)
		steps := make(map[string]string, len(item.stepStatus))
		for step, status := range item.stepStatus {
			if !isTerminalStepStatus(status) {
				steps[step] = status
			}
		}
		ended = append(ended, interrupted{
			id:    id,
			text:  fmt.Sprintf("This execution was interrupted: %s while it ran. Nothing resumes it.", reason),
			steps: steps,
		})
	}
	s.mu.Unlock()

	for _, run := range ended {
		s.endUnfinishedSteps(run.id, run.steps)
		s.appendEvent(run.id, ExecutionEvent{
			ID:        "interrupted",
			Source:    "boot",
			Timestamp: s.nextTimestamp(run.id),
			Text:      run.text,
			Status:    stepStatusFailed,
			Level:     "error",
		})
	}
	if len(ended) > 0 {
		s.persistExecutionRuntime()
	}
	return len(ended)
}

// endUnfinishedSteps records what became of the steps that had not finished: one
// that was running failed with the run, and one that had not started never will.
func (s *Service) endUnfinishedSteps(executionID string, steps map[string]string) {
	for step, status := range steps {
		if status == stepStatusRunning {
			s.appendEvent(executionID, ExecutionEvent{
				ID: step + "-interrupted", Source: step, Timestamp: s.nextTimestamp(executionID),
				Text: fmt.Sprintf("[%s] Stopped with the execution.", step), Status: stepStatusFailed, Level: "error",
			})
			continue
		}
		s.appendEvent(executionID, ExecutionEvent{
			ID: step + "-interrupted", Source: step, Timestamp: s.nextTimestamp(executionID),
			Text: fmt.Sprintf("[%s] Did not run: the execution was interrupted.", step), Status: stepStatusSkipped, Level: "warn",
		})
	}
}
