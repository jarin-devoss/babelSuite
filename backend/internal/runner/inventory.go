package runner

import (
	"context"
	"sort"
	"time"
)

// ExecutionInventory is what one execution has left on a backend: the
// containers or pods it runs, and the networks and volumes they share. The
// Environments page lists these per backend, so a run on a remote agent or in
// Kubernetes can be seen, and reaped, like one on the control plane's Docker.
type ExecutionInventory struct {
	RunID      string          `json:"runId"`
	Suite      string          `json:"suite,omitempty"`
	Profile    string          `json:"profile,omitempty"`
	StartedAt  *time.Time      `json:"startedAt,omitempty"`
	Containers []InventoryItem `json:"containers"`
	Networks   []InventoryItem `json:"networks"`
	Volumes    []InventoryItem `json:"volumes"`
}

// InventoryItem is one resource of an execution. State is Docker's container
// state, or a pod's phase in lower case, so both read the same: "running",
// "exited", "pending", "succeeded", "failed".
type InventoryItem struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind,omitempty"`
	Image     string     `json:"image,omitempty"`
	State     string     `json:"state,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

// InventoryReporter is a backend whose resources live outside the control
// plane's Docker, and which can list and remove them per execution.
type InventoryReporter interface {
	Inventory(ctx context.Context) ([]ExecutionInventory, error)
	Reap(ctx context.Context, runID string) error
}

// inventoryGroups collects items into one ExecutionInventory per run id, in a
// stable order, keeping the earliest start time any item reports.
type inventoryGroups map[string]*ExecutionInventory

func (g inventoryGroups) run(runID, suite, profile string) *ExecutionInventory {
	item, ok := g[runID]
	if !ok {
		item = &ExecutionInventory{RunID: runID, Containers: []InventoryItem{}, Networks: []InventoryItem{}, Volumes: []InventoryItem{}}
		g[runID] = item
	}
	if item.Suite == "" {
		item.Suite = suite
	}
	if item.Profile == "" {
		item.Profile = profile
	}
	return item
}

func (g inventoryGroups) list() []ExecutionInventory {
	out := make([]ExecutionInventory, 0, len(g))
	for _, item := range g {
		for _, c := range item.Containers {
			if c.StartedAt != nil && (item.StartedAt == nil || c.StartedAt.Before(*item.StartedAt)) {
				item.StartedAt = c.StartedAt
			}
		}
		sort.Slice(item.Containers, func(i, j int) bool { return item.Containers[i].Name < item.Containers[j].Name })
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}
