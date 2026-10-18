package environments

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
)

// Source is an execution backend whose resources live outside the control
// plane's Docker — a Kubernetes namespace or a remote agent's host — listed
// and reaped through the backend itself.
type Source struct {
	ID       string
	Label    string
	Kind     string
	Reporter runner.InventoryReporter
}

// Option configures a Service.
type Option func(*Service)

// WithSources adds the backends listed beside the control plane's Docker. The
// function is asked on every snapshot, so enabling a backend shows it at once.
func WithSources(list func() []Source) Option {
	return func(s *Service) { s.sources = list }
}

// WithRunOwner tells the service whether the control plane is still running
// an execution. A remote environment whose run it does not own is a zombie:
// the pid check the host's Docker uses means nothing on another machine.
func WithRunOwner(owned func(runID string) bool) Option {
	return func(s *Service) { s.runOwned = owned }
}

// The host's own Docker, as the backend local environments are shown on.
const (
	hostBackendID    = "host-docker"
	hostBackendLabel = "Control plane Docker"
)

// sourceTimeout bounds one backend's listing, inside the 5s inventory poll:
// an unreachable cluster or agent costs a warning, not the whole page.
const sourceTimeout = 4 * time.Second

func (s *Service) listSources() []Source {
	if s.sources == nil {
		return nil
	}
	return s.sources()
}

// collectRemote lists every source in parallel, returning their environments
// and a warning for each source that could not be reached.
func (s *Service) collectRemote(ctx context.Context) ([]Sandbox, []string) {
	sources := s.listSources()
	type result struct {
		sandboxes []Sandbox
		warning   string
	}
	results := make([]result, len(sources))
	var wg sync.WaitGroup
	for index, source := range sources {
		wg.Go(func() {
			sandboxes, err := s.sourceSandboxes(ctx, source)
			if err != nil {
				results[index].warning = fmt.Sprintf("%s (%s) could not be inventoried: %v", source.Label, source.Kind, err)
				return
			}
			results[index].sandboxes = sandboxes
		})
	}
	wg.Wait()

	var sandboxes []Sandbox
	var warnings []string
	for _, r := range results {
		sandboxes = append(sandboxes, r.sandboxes...)
		if r.warning != "" {
			warnings = append(warnings, r.warning)
		}
	}
	return sandboxes, warnings
}

func (s *Service) sourceSandboxes(ctx context.Context, source Source) ([]Sandbox, error) {
	listCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
	defer cancel()
	runs, err := source.Reporter.Inventory(listCtx)
	if err != nil {
		return nil, err
	}
	sandboxes := make([]Sandbox, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" {
			continue
		}
		sandboxes = append(sandboxes, s.remoteSandbox(ctx, source, run))
	}
	return sandboxes, nil
}

// remoteSandbox turns one execution's resources on a source into an
// environment, named <source>:<run> so a reap finds its way back.
func (s *Service) remoteSandbox(ctx context.Context, source Source, run runner.ExecutionInventory) Sandbox {
	sandbox := Sandbox{
		SandboxID:    source.ID + ":" + run.RunID,
		RunID:        run.RunID,
		Suite:        run.Suite,
		Profile:      run.Profile,
		Backend:      source.ID,
		BackendLabel: source.Label,
		BackendKind:  source.Kind,
		StartedAt:    run.StartedAt,
		Containers:   []Container{},
		Networks:     []Network{},
		Volumes:      []Volume{},
		Warnings:     []string{},
	}
	for _, item := range run.Containers {
		sandbox.Containers = append(sandbox.Containers, Container{
			ID: item.ID, Name: item.Name, Image: item.Image, State: item.State, Status: item.State,
			Ports: []string{}, StartedAt: item.StartedAt,
		})
	}
	for _, item := range run.Networks {
		sandbox.Networks = append(sandbox.Networks, Network{ID: item.ID, Name: item.Name, Driver: item.Kind})
	}
	for _, item := range run.Volumes {
		sandbox.Volumes = append(sandbox.Volumes, Volume{Name: item.Name, Driver: item.Kind})
	}

	owned := s.runOwned != nil && s.runOwned(run.RunID)
	sandbox.IsZombie = !owned
	finalizeSandbox(ctx, s.checker, s.now(), &sandbox)

	// finalizeSandbox speaks of a dead orchestrator pid, which a remote
	// environment has none of: its owner is the control plane's run.
	warnings := sandbox.Warnings[:0]
	for _, warning := range sandbox.Warnings {
		if !strings.HasPrefix(warning, "The orchestrator appears to be gone") {
			warnings = append(warnings, warning)
		}
	}
	sandbox.Warnings = warnings
	if owned {
		sandbox.OrchestratorState = "alive"
	} else {
		sandbox.OrchestratorState = "gone"
		sandbox.Warnings = append(sandbox.Warnings, fmt.Sprintf("The control plane is not running this execution, but it still has resources on %s.", source.Label))
	}
	return sandbox
}

// sourceFor resolves a "<source>:<run>" environment id.
func (s *Service) sourceFor(sandboxID string) (Source, string, bool) {
	sourceID, runID, found := strings.Cut(sandboxID, ":")
	if !found || runID == "" {
		return Source{}, "", false
	}
	for _, source := range s.listSources() {
		if source.ID == sourceID {
			return source, runID, true
		}
	}
	return Source{}, "", false
}

// reapRemote removes one execution from its source.
func (s *Service) reapRemote(ctx context.Context, source Source, runID, sandboxID string) (*ReapResult, error) {
	sandboxes, err := s.sourceSandboxes(ctx, source)
	if err != nil {
		return nil, err
	}
	for _, sandbox := range sandboxes {
		if sandbox.RunID != runID {
			continue
		}
		if err := source.Reporter.Reap(ctx, runID); err != nil {
			return nil, fmt.Errorf("%s could not reap %s: %w", source.Label, runID, err)
		}
		return &ReapResult{
			Scope:             "sandbox",
			Target:            sandboxID,
			RemovedContainers: len(sandbox.Containers),
			RemovedNetworks:   len(sandbox.Networks),
			RemovedVolumes:    len(sandbox.Volumes),
			Warnings:          []string{},
		}, nil
	}
	return nil, ErrNotFound
}

// reapAllRemote removes every execution every source holds, adding to result.
func (s *Service) reapAllRemote(ctx context.Context, result *ReapResult) {
	for _, source := range s.listSources() {
		sandboxes, err := s.sourceSandboxes(ctx, source)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s could not be inventoried: %v", source.Label, err))
			continue
		}
		for _, sandbox := range sandboxes {
			if err := source.Reporter.Reap(ctx, sandbox.RunID); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s could not reap %s: %v", source.Label, sandbox.RunID, err))
				continue
			}
			result.RemovedContainers += len(sandbox.Containers)
			result.RemovedNetworks += len(sandbox.Networks)
			result.RemovedVolumes += len(sandbox.Volumes)
		}
	}
}
