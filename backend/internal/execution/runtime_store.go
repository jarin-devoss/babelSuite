package execution

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

type RuntimeStore interface {
	LoadExecutionRuntime(ctx context.Context) ([]PersistedExecution, error)
	SaveExecutionRuntime(ctx context.Context, executions []PersistedExecution) error
}

type PersistedExecution struct {
	Record    ExecutionRecord  `json:"record"`
	Total     int              `json:"total"`
	Completed int              `json:"completed"`
	Logs      []logstream.Line `json:"logs"`
}

func (s *Service) ConfigureRuntimeStore(store RuntimeStore) {
	s.mu.Lock()
	s.runtimeStore = store
	s.mu.Unlock()
	s.restorePersistedExecutions()
}

func (s *Service) restorePersistedExecutions() {
	s.mu.Lock()
	store := s.runtimeStore
	s.mu.Unlock()
	if store == nil {
		return
	}

	persisted, err := store.LoadExecutionRuntime(context.Background())
	if err != nil {
		slog.Error("could not load the saved executions; none is listed until the store answers", "error", err)
		return
	}
	if len(persisted) == 0 {
		return
	}

	sort.Slice(persisted, func(i, j int) bool {
		return persisted[i].Record.StartedAt.Before(persisted[j].Record.StartedAt)
	})

	s.mu.Lock()
	s.executions = make(map[string]*executionState, len(persisted))
	s.order = s.order[:0]
	for _, item := range persisted {
		if item.Record.ID == "" {
			continue
		}
		statuses, completed := buildStepStatus(item.Record.Suite.Topology, item.Record.Events)
		state := &executionState{
			record:      item.Record,
			workspaceID: item.Record.WorkspaceID,
			total:       item.Total,
			completed:   completed,
			stepStatus:  statuses,
		}
		if state.total == 0 {
			state.total = len(item.Record.Suite.Topology)
		}
		if state.completed == 0 && item.Completed > 0 {
			state.completed = item.Completed
		}
		s.executions[item.Record.ID] = state
		s.order = append(s.order, item.Record.ID)
		s.logs.Open(item.Record.ID)
		for _, line := range item.Logs {
			s.logs.Append(item.Record.ID, line)
		}
	}
	s.mu.Unlock()

	// A run that was unfinished when its control plane stopped is over.
	s.interruptOrphans(time.Now())
}

func (s *Service) persistExecutionRuntime() {
	s.persistExecutionStore()
	s.persistExecutionCache()
}

func (s *Service) persistExecutionStore() {
	s.mu.Lock()
	store := s.runtimeStore
	persisted := make([]PersistedExecution, 0, len(s.executions))
	marks := make(map[string]saveMark, len(s.executions))
	for _, state := range s.executions {
		if state == nil {
			continue
		}
		logs := s.executionLogLinesLocked(state.record.ID)
		// A finished execution that has not changed since it was saved is not
		// written again: every save used to rewrite every execution and its whole
		// log, however many had not moved.
		mark := markOf(state, len(logs))
		if s.saved[state.record.ID] == mark {
			continue
		}
		marks[state.record.ID] = mark
		persisted = append(persisted, PersistedExecution{
			Record:    state.record,
			Total:     state.total,
			Completed: state.completed,
			Logs:      logs,
		})
	}
	s.mu.Unlock()

	if store == nil || len(persisted) == 0 {
		return
	}

	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.SaveExecutionRuntime(saveCtx, persisted); err != nil {
		slog.Warn("failed to persist execution runtime", "error", err, "executions", len(persisted))
		return
	}
	s.mu.Lock()
	if s.saved == nil {
		s.saved = make(map[string]saveMark, len(marks))
	}
	for id, mark := range marks {
		s.saved[id] = mark
	}
	s.mu.Unlock()
}

// saveMark identifies the state of an execution as it was last saved.
type saveMark struct {
	updatedAt int64
	status    string
	events    int
	artifacts int
	completed int
	logs      int
	lease     int64
}

func markOf(state *executionState, logLines int) saveMark {
	mark := saveMark{
		updatedAt: state.record.UpdatedAt.UnixNano(),
		status:    state.record.Status,
		events:    len(state.record.Events),
		artifacts: len(state.record.Artifacts),
		completed: state.completed,
		logs:      logLines,
	}
	if state.record.LeaseUntil != nil {
		mark.lease = state.record.LeaseUntil.UnixNano()
	}
	return mark
}
