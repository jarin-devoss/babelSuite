package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// modeService has a local Docker default and a Kubernetes pool carrying the
// gpu-enabled tag, both enabled, under the given deployment mode.
func modeService(t *testing.T, mode string) *Service {
	t.Helper()
	service := NewServiceWithPlatform(suites.NewWorkspaceService(), stubPlatformSource{
		settings: &platform.PlatformSettings{
			Mode: mode,
			Agents: []platform.ExecutionAgent{
				{AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true},
				{AgentID: "k8s", Name: "Kubernetes Pool", Type: "kubernetes", Enabled: true, RoutingTags: []string{"gpu-enabled"}},
			},
		},
	})
	t.Cleanup(service.Close)
	return service
}

func backendIDs(options []BackendOption) []string {
	ids := make([]string, 0, len(options))
	for _, option := range options {
		ids = append(ids, option.ID)
	}
	return ids
}

// The deployment mode decides which enabled backends runs use: the setting
// used to be saved and shown, and read by nothing.
func TestDeploymentModeDecidesTheBackends(t *testing.T) {
	ctx := context.Background()

	t.Run("local uses only the host's Docker", func(t *testing.T) {
		service := modeService(t, platform.ModeLocal)
		if ids := backendIDs(service.BackendOptions()); strings.Join(ids, ",") != "local-docker" {
			t.Errorf("offered backends = %v, want only local-docker", ids)
		}
		binding, _, err := service.resolveBackend(ctx, "", nil)
		if err != nil || binding.option.ID != "local-docker" {
			t.Errorf("untagged run: %q, %v", binding.option.ID, err)
		}
		_, _, err = service.resolveBackend(ctx, "k8s", nil)
		if !errors.Is(err, ErrModeExcludesBackend) {
			t.Errorf("asking for k8s: err = %v, want ErrModeExcludesBackend", err)
		}
		// The only backend with the tag is one the mode leaves out: say so,
		// rather than that no backend carries it.
		_, _, err = service.resolveBackend(ctx, "", []string{"gpu-enabled"})
		if !errors.Is(err, ErrModeExcludesBackend) || !strings.Contains(err.Error(), "Kubernetes Pool") {
			t.Errorf("gpu profile: err = %v, want ErrModeExcludesBackend naming the pool", err)
		}
	})

	t.Run("remote never uses the host's Docker", func(t *testing.T) {
		service := modeService(t, platform.ModeRemote)
		if ids := backendIDs(service.BackendOptions()); strings.Join(ids, ",") != "k8s" {
			t.Errorf("offered backends = %v, want only k8s", ids)
		}
		_, _, err := service.resolveBackend(ctx, "local-docker", nil)
		if !errors.Is(err, ErrModeExcludesBackend) {
			t.Errorf("asking for local-docker: err = %v, want ErrModeExcludesBackend", err)
		}
	})

	t.Run("hybrid uses both and routes by tag", func(t *testing.T) {
		service := modeService(t, platform.ModeHybrid)
		if ids := backendIDs(service.BackendOptions()); strings.Join(ids, ",") != "local-docker,k8s" {
			t.Errorf("offered backends = %v, want both", ids)
		}
		binding, _, err := service.resolveBackend(ctx, "", nil)
		if err != nil || binding.option.ID != "local-docker" {
			t.Errorf("untagged run: %q, %v; want the default", binding.option.ID, err)
		}
	})
}
