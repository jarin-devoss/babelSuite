package execution

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/queue"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// agentCapacity is the number of steps the remote backend's agent runs at
// once, as it declared at registration. Claims are matched to the backend by
// id and the registry keeps one record per id, so this is one agent's
// capacity — a backend is served by one agent. Offline it is 1, so one step
// waits in the coordinator for an agent to come back and the rest wait in the
// queue until it polls again.
func agentCapacity(registry *agent.Registry, backendID string) int {
	if registry == nil {
		return 1
	}
	total := 0
	for _, reg := range registry.ListRegistrations() {
		if strings.TrimSpace(reg.AgentID) == backendID && registry.IsAvailable(reg.AgentID) {
			total += reg.SlotCount()
		}
	}
	if total < 1 {
		return 1
	}
	return total
}

type backendBinding struct {
	option  BackendOption
	backend runner.Backend
}

func NewServiceWithPlatform(source suiteSource, platforms platformSource, observers ...Observer) *Service {
	if source == nil {
		source = suites.NewService()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		ctx:            ctx,
		cancel:         cancel,
		queue:          queue.NewMemory(ctx),
		suiteSource:    source,
		platformSource: platforms,
		observers:      observers,
		logs:           logstream.NewHub(),
		suiteMeta:      seedExecutionMetadata(),
		executions:     make(map[string]*executionState),
		subs:           make(map[string]map[chan StreamEvent]struct{}),
		concurrencySem: make(chan struct{}, maxConcurrentExecutions),
	}
	s.signals = newTelemetrySet(s)
	s.seedHistory()
	return s
}

func (s *Service) BackendOptions() []BackendOption {
	bindings := s.configuredBackends()
	options := make([]BackendOption, 0, len(bindings))
	for _, binding := range bindings {
		option := binding.option
		option.Available = s.backendAvailable(binding.backend)
		if capacity := backendCapacity(binding.backend); capacity != nil {
			option.Capacity = max(capacity(), 0)
		}
		options = append(options, option)
	}
	return options
}

// resolveBackend picks the backend for a launch. A backend named in the
// request is used as asked, and the profile tags it lacks are returned so the
// run can say so. Otherwise only backends carrying every required tag are
// candidates, the default among them first.
//
// Only backends the deployment mode allows are considered. A requested or
// tag-matching backend the mode excludes is refused with
// ErrModeExcludesBackend rather than reported missing, so the run says which
// setting is in the way.
func (s *Service) resolveBackend(ctx context.Context, requested string, requiredTags []string) (backendBinding, []string, error) {
	bindings, excluded, mode := s.backendCatalog()

	requested = strings.TrimSpace(requested)
	if requested != "" && !strings.EqualFold(requested, runner.AutoBackend) {
		for _, binding := range bindings {
			if binding.option.ID != requested {
				continue
			}
			if !s.backendAvailableWithContext(ctx, binding.backend) {
				return backendBinding{}, nil, ErrBackendUnavailable
			}
			return binding, missingTags(binding.option.Tags, requiredTags), nil
		}
		for _, binding := range excluded {
			if binding.option.ID == requested {
				return backendBinding{}, nil, fmt.Errorf("%w: %s is a %s backend, which %s mode does not use", ErrModeExcludesBackend, binding.option.Label, kindWord(binding.option.Kind), mode)
			}
		}
		return backendBinding{}, nil, ErrBackendNotFound
	}
	if len(bindings) == 0 {
		return backendBinding{}, nil, fmt.Errorf("%w: %s mode leaves no enabled backend to run on", ErrModeExcludesBackend, mode)
	}

	candidates := make([]backendBinding, 0, len(bindings))
	for _, binding := range bindings {
		if len(missingTags(binding.option.Tags, requiredTags)) == 0 {
			candidates = append(candidates, binding)
		}
	}
	if len(candidates) == 0 {
		for _, binding := range excluded {
			if len(missingTags(binding.option.Tags, requiredTags)) == 0 {
				return backendBinding{}, nil, fmt.Errorf("%w: only %s carries the profile's agent tags (%s), and %s mode does not use %s backends", ErrModeExcludesBackend, binding.option.Label, strings.Join(requiredTags, ", "), mode, kindWord(binding.option.Kind))
			}
		}
		return backendBinding{}, nil, fmt.Errorf("%w: needs %s", ErrNoTaggedBackend, strings.Join(requiredTags, ", "))
	}

	for _, binding := range candidates {
		if binding.option.Default && s.backendAvailableWithContext(ctx, binding.backend) {
			return binding, nil, nil
		}
	}
	for _, binding := range candidates {
		if s.backendAvailableWithContext(ctx, binding.backend) {
			return binding, nil, nil
		}
	}

	return backendBinding{}, nil, ErrBackendUnavailable
}

// missingTags returns the required tags a backend does not carry, compared
// case-insensitively, as tags are typed by hand on both sides.
func missingTags(have, required []string) []string {
	carried := make(map[string]bool, len(have))
	for _, tag := range have {
		carried[strings.ToLower(strings.TrimSpace(tag))] = true
	}
	var missing []string
	for _, tag := range required {
		if !carried[strings.ToLower(strings.TrimSpace(tag))] {
			missing = append(missing, tag)
		}
	}
	return missing
}

// configuredBackends is the enabled backends the deployment mode lets runs
// use — what launches choose from and the backend list offers.
func (s *Service) configuredBackends() []backendBinding {
	allowed, _, _ := s.backendCatalog()
	return allowed
}

// backendCatalog builds the enabled backends, split into those the deployment
// mode allows and those it excludes, and returns the mode. Without loadable
// settings there is only the host's Docker, as there always was; with a mode
// that allows local runs and no enabled backend, the same.
func (s *Service) backendCatalog() (allowed, excluded []backendBinding, mode string) {
	settings, err := s.loadPlatformSettings()
	if err != nil || settings == nil {
		return []backendBinding{fallbackBackendBinding()}, nil, platform.ModeLocal
	}
	mode = strutil.FirstNonEmpty(strings.ToLower(strings.TrimSpace(settings.Mode)), platform.ModeLocal)

	for _, agentConfig := range settings.Agents {
		if !agentConfig.Enabled {
			continue
		}

		kind := normalizeBackendKind(agentConfig.Type)
		option := BackendOption{
			ID:          strutil.FirstNonEmpty(agentConfig.AgentID, agentConfig.Name, kind),
			Label:       strutil.FirstNonEmpty(agentConfig.Name, agentConfig.AgentID, kind),
			Kind:        kind,
			Description: agentConfig.Description,
			Default:     agentConfig.Default,
			Tags:        append([]string{}, agentConfig.RoutingTags...),
		}

		var backend runner.Backend
		switch kind {
		case "local":
			backend = runner.NewLocal(runner.BackendConfig{ID: option.ID, Label: option.Label, Kind: kind})
		case "kubernetes":
			backend = runner.NewKubernetes(runner.KubernetesConfig{
				BackendConfig: runner.BackendConfig{ID: option.ID, Label: option.Label, Kind: kind},
				Namespace:     agentConfig.TargetNamespace,
				Kubeconfig:    agentConfig.KubeconfigPath,
			})
		case "remote":
			backend = runner.NewRemote(runner.RemoteConfig{
				BackendConfig: runner.BackendConfig{ID: option.ID, Label: option.Label, Kind: kind},
				BaseURL:       agentConfig.HostURL,
				Dispatcher:    agent.NewBrokerDispatcher(option.ID, s.registry, s.coordinator),
				Capacity:      func() int { return agentCapacity(s.registry, option.ID) },
				Secret:        s.agentSecret,
			})
		default:
			continue
		}

		binding := backendBinding{option: option, backend: backend}
		if platform.ModeAllowsAgent(mode, agentConfig.Type) {
			allowed = append(allowed, binding)
		} else {
			excluded = append(excluded, binding)
		}
	}

	if len(allowed) == 0 && platform.ModeAllowsAgent(mode, "local") && len(excluded) == 0 {
		allowed = []backendBinding{fallbackBackendBinding()}
	}
	return allowed, excluded, mode
}

// kindWord names a backend kind for a message.
func kindWord(kind string) string {
	switch kind {
	case "local":
		return "local Docker"
	case "remote":
		return "remote agent"
	default:
		return kind
	}
}

func (s *Service) loadPlatformSettings() (*platform.PlatformSettings, error) {
	if s.platformSource == nil {
		return nil, nil
	}
	return s.platformSource.Load()
}

func (s *Service) loadRegisteredPlugins() []platform.CustomPlugin {
	settings, err := s.loadPlatformSettings()
	if err != nil || settings == nil {
		return nil
	}
	return append([]platform.CustomPlugin{}, settings.Plugins...)
}

func (s *Service) pluginAwareModuleResolver() suites.ModuleResolver {
	plugins := s.loadRegisteredPlugins()
	base := s.suiteSource.ResolveModuleFiles
	return func(name string) (map[string]string, error) {
		for _, p := range plugins {
			if p.Name == name && strings.TrimSpace(p.Star) != "" {
				return map[string]string{"plugin.star": p.Star}, nil
			}
		}
		if base != nil {
			return base(name)
		}
		return nil, nil
	}
}

func (s *Service) backendAvailable(backend runner.Backend) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.backendAvailableWithContext(ctx, backend)
}

func (s *Service) backendAvailableWithContext(ctx context.Context, backend runner.Backend) bool {
	if backend == nil {
		return false
	}
	return backend.IsAvailable(ctx)
}

func fallbackBackendBinding() backendBinding {
	option := BackendOption{
		ID:          "local-docker",
		Label:       "Local Docker",
		Kind:        "local",
		Description: "Runs on the host worker without additional remote plumbing.",
		Default:     true,
		Available:   true,
	}
	return backendBinding{
		option:  option,
		backend: runner.NewLocal(runner.BackendConfig{ID: option.ID, Label: option.Label, Kind: option.Kind, Permissive: true}),
	}
}

func normalizeBackendKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "local", "docker":
		return "local"
	case "kubernetes", "k8s":
		return "kubernetes"
	case "remote", "remote-agent", "remote-docker":
		return "remote"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}
