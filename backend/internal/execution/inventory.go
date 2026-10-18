package execution

import (
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
)

// InventorySource is an enabled backend whose resources live outside the
// control plane's Docker — a Kubernetes namespace or a remote agent's host —
// for the Environments page to list and reap.
type InventorySource struct {
	ID       string
	Label    string
	Kind     string
	Reporter runner.InventoryReporter
}

// runOwnedGrace is how long a finished run still counts as owned: its release
// job and pod deletions take a few seconds, and until then its resources are
// expected, not leaked.
const runOwnedGrace = 2 * time.Minute

// ConfigureAgentSecret sets the shared secret the remote backends present to
// their agents' own HTTP API, which serves the inventory.
func (s *Service) ConfigureAgentSecret(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSecret = secret
}

// InventorySources returns every enabled Kubernetes and remote-agent backend,
// those the deployment mode excludes included: a run left behind before the
// mode changed is a leak all the same.
func (s *Service) InventorySources() []InventorySource {
	allowed, excluded, _ := s.backendCatalog()
	sources := make([]InventorySource, 0, len(allowed)+len(excluded))
	for _, binding := range append(allowed, excluded...) {
		reporter, ok := binding.backend.(runner.InventoryReporter)
		if !ok || binding.option.Kind == "local" {
			continue
		}
		sources = append(sources, InventorySource{
			ID:       binding.option.ID,
			Label:    binding.option.Label,
			Kind:     binding.option.Kind,
			Reporter: reporter,
		})
	}
	return sources
}

// RunOwned reports whether this control plane is running the execution, or
// finished it so recently that its cleanup may still be under way. Resources
// of a run it does not own were left by one that crashed or lost track of it.
//
// An execution restored from the runtime store or cache after a restart is
// not owned, whatever status it was saved with: no process resumes it, so a
// run saved as Running is exactly the leak this exists to find. Only one this
// process booted holds the backend its steps run on.
func (s *Service) RunOwned(executionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.executions[executionID]
	if item == nil || item.backend == nil {
		return false
	}
	switch item.record.Status {
	case "Healthy", "Failed":
		return time.Since(item.record.UpdatedAt) < runOwnedGrace
	}
	return true
}
