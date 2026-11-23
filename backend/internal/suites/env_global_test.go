package suites

import (
	"strings"
	"testing"
)

const envSuiteStar = `
count = int(env.get("COUNT", "1"))
for i in range(count):
    service.run(name="worker-" + str(i), image="alpine:3.19")
`

func envSuite(profileEnv string) Definition {
	return Definition{
		ID:        "env-suite",
		Title:     "Env Suite",
		SuiteStar: envSuiteStar,
		Profiles:  []ProfileOption{{FileName: "local.yaml", Default: true}},
		SourceFiles: []SourceFile{
			{Path: "profiles/local.yaml", Content: "name: Local\nenv:\n" + profileEnv},
		},
	}
}

func workerCount(t *testing.T, nodes []TopologyNode) int {
	t.Helper()
	count := 0
	for _, node := range nodes {
		if strings.HasPrefix(node.Name, "worker-") {
			count++
		}
	}
	return count
}

// env.get() reads the env the run's profile gives: COUNT: "3" builds three
// nodes. It used to be a frozen empty dict, so every profile built one.
func TestEnvGlobalReadsTheLaunchProfilesEnv(t *testing.T) {
	t.Parallel()
	suite := envSuite("  COUNT: \"3\"\n")

	withProfile, err := ResolveRuntimeWithEnv(suite, []Definition{suite}, nil, map[string]string{"COUNT": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if got := workerCount(t, withProfile.Nodes); got != 3 {
		t.Fatalf("with COUNT=3 the topology has %d workers, want 3", got)
	}

	withoutProfile, err := ResolveRuntimeWithEnv(suite, []Definition{suite}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := workerCount(t, withoutProfile.Nodes); got != 1 {
		t.Fatalf("with no env the topology has %d workers, want the default 1", got)
	}
}

// The catalog and the graph have no launch, so they show what the default
// profile builds.
func TestTheCatalogTopologyUsesTheDefaultProfilesEnv(t *testing.T) {
	t.Parallel()
	suite := envSuite("  COUNT: \"2\"\n")
	resolved := ResolveDefinitionTopology(suite, []Definition{suite})
	if resolved.TopologyError != "" {
		t.Fatal(resolved.TopologyError)
	}
	if got := workerCount(t, resolved.Topology); got != 2 {
		t.Fatalf("the default profile builds %d workers, want 2", got)
	}
}

// The env cannot be written: a suite must not reach around its profile.
func TestEnvGlobalIsReadOnly(t *testing.T) {
	t.Parallel()
	suite := Definition{ID: "ro", SuiteStar: "env[\"X\"] = \"1\"\nservice.run(name=\"a\", image=\"alpine\")\n"}
	if _, err := ResolveRuntimeWithEnv(suite, []Definition{suite}, nil, map[string]string{"A": "b"}); err == nil {
		t.Fatal("writing to env must fail")
	}
}

// A nested suite does not see its parent's env: it reads the profile its
// dependency entry names.
func TestANestedSuiteReadsItsOwnProfilesEnvNotTheParents(t *testing.T) {
	t.Parallel()
	child := envSuite("  COUNT: \"2\"\n")
	child.ID = "child-suite"
	child.Repository = "localhost:5000/core/child-suite"
	child.Version = "workspace"

	parent := Definition{
		ID:        "parent-suite",
		Title:     "Parent",
		SuiteStar: `child = suite.run(ref="child-module")`,
		SourceFiles: []SourceFile{{Path: "dependencies.yaml", Content: strings.TrimSpace(`
dependencies:
  child-module:
    ref: localhost:5000/core/child-suite
    version: workspace
    profile: local.yaml
`)}},
	}
	resolved, err := ResolveRuntimeWithEnv(parent, []Definition{parent, child}, nil, map[string]string{"COUNT": "9"})
	if err != nil {
		t.Fatal(err)
	}
	if got := workerCount(t, resolved.Nodes) + countPrefixed(resolved.Nodes, "child/worker-"); got != 2 {
		t.Fatalf("the nested suite built %d workers, want 2 (its own profile's COUNT, not the parent's 9)", got)
	}
}

func countPrefixed(nodes []TopologyNode, prefix string) int {
	count := 0
	for _, node := range nodes {
		if strings.HasPrefix(node.ID, prefix) {
			count++
		}
	}
	return count
}
