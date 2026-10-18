package execution

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// A profile's agent.tags route the run to a backend carrying them, instead of
// tags that only decorated the UI.
func TestResolveBackendRoutesByProfileTags(t *testing.T) {
	service := NewServiceWithPlatform(suites.NewWorkspaceService(), stubPlatformSource{
		settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{
				{AgentID: "laptop", Name: "Laptop", Type: "local", Enabled: true, Default: true, RoutingTags: []string{"local"}},
				{AgentID: "gpu-box", Name: "GPU Box", Type: "local", Enabled: true, RoutingTags: []string{"gpu-enabled", "high-memory"}},
			},
		},
	})
	defer service.Close()
	ctx := context.Background()

	binding, missing, err := service.resolveBackend(ctx, "", []string{"GPU-Enabled"})
	if err != nil || binding.option.ID != "gpu-box" || len(missing) != 0 {
		t.Errorf("gpu profile: got %q missing=%v err=%v, want gpu-box", binding.option.ID, missing, err)
	}

	binding, _, err = service.resolveBackend(ctx, "", nil)
	if err != nil || binding.option.ID != "laptop" {
		t.Errorf("untagged profile: got %q err=%v, want the default laptop", binding.option.ID, err)
	}

	if _, _, err := service.resolveBackend(ctx, "", []string{"tpu"}); !errors.Is(err, ErrNoTaggedBackend) {
		t.Errorf("unknown tag: err = %v, want ErrNoTaggedBackend", err)
	}

	binding, missing, err = service.resolveBackend(ctx, "laptop", []string{"gpu-enabled"})
	if err != nil || binding.option.ID != "laptop" || !reflect.DeepEqual(missing, []string{"gpu-enabled"}) {
		t.Errorf("explicit choice: got %q missing=%v err=%v, want laptop missing [gpu-enabled]", binding.option.ID, missing, err)
	}
}

func TestProfileAgentTagsParse(t *testing.T) {
	t.Parallel()

	parent, err := parseManagedProfileYAML("agent:\n  tags: [gpu-enabled, ' high-memory ', '']\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parent.AgentTags, []string{"gpu-enabled", "high-memory"}) {
		t.Fatalf("parsed tags = %v", parent.AgentTags)
	}
	if got := missingTags([]string{"GPU-enabled"}, parent.AgentTags); !reflect.DeepEqual(got, []string{"high-memory"}) {
		t.Errorf("missingTags = %v, want [high-memory]", got)
	}

	none, err := parseManagedProfileYAML("env:\n  A: b\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(none.AgentTags) != 0 {
		t.Errorf("a profile without agent: requires no tags, got %v", none.AgentTags)
	}
}
