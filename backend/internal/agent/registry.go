package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/strutil"
)

const (
	heartbeatExpiry    = 30 * time.Second
	heartbeatCheckTick = 10 * time.Second
)

type Registry struct {
	store RuntimeStore

	mu     sync.RWMutex
	agents map[string]Registration
}

func NewRegistry(store RuntimeStore) *Registry {
	return &Registry{
		store:  store,
		agents: make(map[string]Registration),
	}
}

// Start runs a background loop that marks agents offline when no heartbeat has
// been received within heartbeatExpiry. It returns when ctx is cancelled.
func (r *Registry) Start(ctx context.Context) {
	ticker := time.NewTicker(heartbeatCheckTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.expireStaleAgents(time.Now().UTC())
		}
	}
}

func (r *Registry) expireStaleAgents(now time.Time) {
	settings, err := r.loadSettings()
	if err == nil {
		changed := false
		for i := range settings.Agents {
			a := &settings.Agents[i]
			if strings.ToLower(a.Status) != "online" && strings.ToLower(a.Status) != "ready" {
				continue
			}
			if !a.LastHeartbeatAt.IsZero() && now.Sub(a.LastHeartbeatAt) > heartbeatExpiry {
				a.Status = "offline"
				changed = true
			}
		}
		if changed {
			_ = r.store.Save(settings)
		}
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for id, agent := range r.agents {
		status := strings.ToLower(agent.Status)
		if status != "online" && status != "ready" {
			continue
		}
		if !agent.LastHeartbeatAt.IsZero() && now.Sub(agent.LastHeartbeatAt) > heartbeatExpiry {
			agent.Status = "offline"
			r.agents[id] = agent
		}
	}
}

// ErrAgentOnline is returned by Claim when the id belongs to an agent on another
// host that is still sending heartbeats.
var ErrAgentOnline = errors.New("agent id is in use by an agent that is still online")

// AgentOnlineError says which agent holds the id.
type AgentOnlineError struct {
	AgentID  string
	HostURL  string
	LastSeen time.Time
}

func (e *AgentOnlineError) Error() string {
	return fmt.Sprintf("agent %q is online at %s (last heartbeat %s); stop it, wait %s for it to expire, or register with force", e.AgentID, e.HostURL, e.LastSeen.UTC().Format(time.RFC3339), heartbeatExpiry)
}

func (e *AgentOnlineError) Is(target error) bool { return target == ErrAgentOnline }

// Claim registers an agent unless its id belongs to an agent on another host
// that is still online. The same host may register again at once (an agent that
// restarted), and force takes the id over; either way the registration is
// recorded as Register records it.
func (r *Registry) Claim(request RegisterRequest) (Registration, error) {
	if existing, found := r.Get(request.AgentID); found && !request.Force &&
		isOnline(existing, time.Now().UTC()) && !sameHost(existing.HostURL, request.HostURL) {
		return Registration{}, &AgentOnlineError{AgentID: existing.AgentID, HostURL: existing.HostURL, LastSeen: existing.LastHeartbeatAt}
	}
	return r.Register(request), nil
}

// isOnline reports whether the agent has sent a heartbeat recently enough to be
// taken for alive.
func isOnline(record Registration, now time.Time) bool {
	status := strings.ToLower(strings.TrimSpace(record.Status))
	if status != "online" && status != "ready" {
		return false
	}
	return record.LastHeartbeatAt.IsZero() || now.Sub(record.LastHeartbeatAt) <= heartbeatExpiry
}

// sameHost compares the machines two agent URLs name, ignoring the port: an
// agent that restarts often listens on a new one.
func sameHost(a, b string) bool {
	host := func(raw string) string {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Hostname() == "" {
			return strings.ToLower(strings.TrimSpace(raw))
		}
		return strings.ToLower(parsed.Hostname())
	}
	return strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" || host(a) == host(b)
}

func (r *Registry) Register(request RegisterRequest) Registration {
	now := time.Now().UTC()
	if settings, err := r.loadSettings(); err == nil {
		record := upsertAgentRegistration(settings, request, now)
		if err := r.store.Save(settings); err == nil {
			return record
		}
	}

	record := Registration{
		AgentID:         request.AgentID,
		Name:            request.Name,
		HostURL:         request.HostURL,
		Status:          "online",
		Capabilities:    append([]string{}, request.Capabilities...),
		Capacity:        request.Capacity,
		RegisteredAt:    now,
		LastHeartbeatAt: now,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.agents[request.AgentID]; ok {
		record.RegisteredAt = existing.RegisteredAt
	}
	r.agents[request.AgentID] = record
	return record
}

func (r *Registry) Heartbeat(agentID string) (Registration, bool) {
	now := time.Now().UTC()
	if settings, err := r.loadSettings(); err == nil {
		record, ok := updateAgentHeartbeat(settings, agentID, now)
		if !ok {
			return Registration{}, false
		}
		if err := r.store.Save(settings); err == nil {
			return record, true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.agents[agentID]
	if !ok {
		return Registration{}, false
	}
	record.LastHeartbeatAt = now
	record.Status = "online"
	r.agents[agentID] = record
	return record, true
}

func (r *Registry) Unregister(agentID string) {
	if settings, err := r.loadSettings(); err == nil {
		if markAgentOffline(settings, agentID) {
			_ = r.store.Save(settings)
			return
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.agents[agentID]; ok {
		record.Status = "offline"
		r.agents[agentID] = record
	}
}

func (r *Registry) ListRegistrations() []Registration { return r.List() }

func (r *Registry) List() []Registration {
	if settings, err := r.loadSettings(); err == nil {
		records := append([]Registration{}, settings.Agents...)
		sortRegistrations(records)
		return records
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]Registration, 0, len(r.agents))
	for _, record := range r.agents {
		records = append(records, record)
	}
	sortRegistrations(records)
	return records
}

func (r *Registry) Get(agentID string) (Registration, bool) {
	if settings, err := r.loadSettings(); err == nil {
		for _, agent := range settings.Agents {
			if strings.TrimSpace(agent.AgentID) == strings.TrimSpace(agentID) {
				return agent, true
			}
		}
		return Registration{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.agents[agentID]
	return record, ok
}

func (r *Registry) IsAvailable(agentID string) bool {
	record, ok := r.Get(agentID)
	if !ok {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(record.Status))
	return status == "online" || status == "ready"
}

func (r *Registry) loadSettings() (*RuntimeState, error) {
	if r.store == nil {
		return nil, ErrRuntimeNotFound
	}
	return r.store.Load()
}

func upsertAgentRegistration(settings *RuntimeState, request RegisterRequest, now time.Time) Registration {
	for index := range settings.Agents {
		if strings.TrimSpace(settings.Agents[index].AgentID) != strings.TrimSpace(request.AgentID) {
			continue
		}

		record := &settings.Agents[index]
		record.Name = strutil.FirstNonEmpty(request.Name, record.Name)
		record.HostURL = strutil.FirstNonEmpty(request.HostURL, record.HostURL)
		record.Status = "online"
		record.Capabilities = append([]string{}, request.Capabilities...)
		record.Capacity = request.Capacity
		if record.RegisteredAt.IsZero() {
			record.RegisteredAt = now
		}
		record.LastHeartbeatAt = now
		return *record
	}

	record := Registration{
		AgentID:         request.AgentID,
		Name:            strutil.FirstNonEmpty(request.Name, request.AgentID),
		HostURL:         request.HostURL,
		Status:          "online",
		Capabilities:    append([]string{}, request.Capabilities...),
		Capacity:        request.Capacity,
		RegisteredAt:    now,
		LastHeartbeatAt: now,
	}
	settings.Agents = append(settings.Agents, record)
	return record
}

func updateAgentHeartbeat(settings *RuntimeState, agentID string, now time.Time) (Registration, bool) {
	for index := range settings.Agents {
		if strings.TrimSpace(settings.Agents[index].AgentID) != strings.TrimSpace(agentID) {
			continue
		}
		record := &settings.Agents[index]
		record.LastHeartbeatAt = now
		record.Status = "online"
		return *record, true
	}
	return Registration{}, false
}

func markAgentOffline(settings *RuntimeState, agentID string) bool {
	for index := range settings.Agents {
		if strings.TrimSpace(settings.Agents[index].AgentID) != strings.TrimSpace(agentID) {
			continue
		}
		settings.Agents[index].Status = "offline"
		return true
	}
	return false
}

func sortRegistrations(records []Registration) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})
}
