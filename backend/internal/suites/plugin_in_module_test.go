package suites

import (
	"strings"
	"testing"
)

// TestPluginLoadableFromModule verifies that a module's .star file can call
// load("@plugins/<name>", ...) and re-export the resulting node builder,
// which the suite then uses to register a topology node.
func TestPluginLoadableFromModule(t *testing.T) {
	t.Parallel()

	// plugin.star — minimal plugin wrapper that returns a plugin node
	pluginStar := strings.TrimSpace(`
load("@babelsuite/runtime", "plugin")
_ref = plugin("test-checker")
def check(name, target, after=[]):
    return _ref.check(name=name, after=after, target=target)
`)

	// health.star inside my-module — loads the plugin and re-exports it.
	// Note: loadModuleDir skips module.star and usage.star; exported symbols
	// must live in other .star files (cluster.star, admin.star, health.star, …).
	healthStar := strings.TrimSpace(`
load("@plugins/test-checker", "check")
check_health = check
`)

	// suite.star — loads the module and calls the plugin through it
	suiteStar := strings.TrimSpace(`
load("@babelsuite/my-module", "check_health")
api = service.run()
health = check_health(name="health-check", target="http://api:8080/health", after=[api])
`)

	resolver := func(name string) (map[string]string, error) {
		switch name {
		case "my-module":
			return map[string]string{"health.star": healthStar}, nil
		case "test-checker":
			return map[string]string{"plugin.star": pluginStar}, nil
		}
		return nil, nil
	}

	nodes, err := evalStarlarkTopology(suiteStar, resolver)
	if err != nil {
		t.Fatalf("evalStarlarkTopology: %v", err)
	}

	var found bool
	for _, n := range nodes {
		if n.Name == "health-check" || n.ID == "health" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a node named 'health-check' or id 'health', got nodes: %+v", nodes)
	}
}

// TestPluginLoadFromModuleErrorsWhenPluginMissing verifies that a clear error
// is returned when a module references a plugin that is not registered.
func TestPluginLoadFromModuleErrorsWhenPluginMissing(t *testing.T) {
	t.Parallel()

	healthStar := strings.TrimSpace(`
load("@plugins/nonexistent-plugin", "check")
check_health = check
`)

	suiteStar := strings.TrimSpace(`
load("@babelsuite/my-module", "check_health")
api = service.run()
`)

	resolver := func(name string) (map[string]string, error) {
		if name == "my-module" {
			return map[string]string{"health.star": healthStar}, nil
		}
		return nil, nil
	}

	_, err := evalStarlarkTopology(suiteStar, resolver)
	if err == nil {
		t.Fatal("expected error when plugin not found, got nil")
	}
}
