package execution

import (
	"os"
	"path/filepath"
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
)

// forgottenWorkspaceAge is how old a workspace with no record at all must be
// before it is taken for a leftover. A run another control plane drives has a
// record here only if it was restored, so the age is the guard for those.
const forgottenWorkspaceAge = 24 * time.Hour

// CleanStaleWorkspaces removes the workspace directories nothing needs:
// those of executions that have finished (the end of a run removes its own, so
// one still here was left by a crash), and those of executions this control
// plane has never heard of that have not been touched for a day. The workspace
// of an execution still running is never removed. It returns how many it
// removed.
func (s *Service) CleanStaleWorkspaces() int {
	base := filepath.Dir(runner.ExecutionWorkspaceDir("x"))
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		s.mu.Lock()
		item := s.executions[entry.Name()]
		active := item != nil && !IsTerminalStatus(item.record.Status)
		known := item != nil
		s.mu.Unlock()
		if active {
			continue
		}
		if !known {
			info, err := entry.Info()
			if err != nil || time.Since(info.ModTime()) < forgottenWorkspaceAge {
				continue
			}
		}
		if os.RemoveAll(filepath.Join(base, entry.Name())) == nil {
			removed++
		}
	}
	return removed
}
