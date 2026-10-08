package suites

import (
	"strings"
	"testing"
)

func preHookSuite(preHook string) Definition {
	suite := Definition{
		ID:         "gated-suite",
		Title:      "Gated Suite",
		Repository: "localhost:5000/core/gated-suite",
		Version:    "workspace",
		SuiteStar: strings.Join([]string{
			`db = service.run()`,
			`api = service.run(after=[db])`,
		}, "\n"),
	}
	if preHook != "" {
		suite.SourceFiles = []SourceFile{{Path: PreHookFile, Content: preHook}}
	}
	return suite
}

func nodeByID(t *testing.T, nodes []TopologyNode, id string) TopologyNode {
	t.Helper()
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("step %q not found in %v", id, nodeIDs(nodes))
	return TopologyNode{}
}

func nodeIDs(nodes []TopologyNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func TestPreHookGatesSuiteRoots(t *testing.T) {
	t.Parallel()

	suite := preHookSuite(`ready = task.run(file="check_env.py", image="python:3.12")`)

	resolved, err := ResolveRuntime(suite, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	gate := nodeByID(t, resolved.Nodes, "pre-hook/ready")
	if len(gate.DependsOn) != 0 {
		t.Fatalf("pre-hook step should not depend on anything, got %v", gate.DependsOn)
	}

	// db is the suite's only root, so it is what the gate has to block.
	db := nodeByID(t, resolved.Nodes, "db")
	if len(db.DependsOn) != 1 || db.DependsOn[0] != "pre-hook/ready" {
		t.Fatalf("db should wait on the pre-hook, got %v", db.DependsOn)
	}

	// Non-root suite steps keep their original edges rather than being
	// re-gated, since the pre-hook already blocks them transitively.
	api := nodeByID(t, resolved.Nodes, "api")
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "db" {
		t.Fatalf("api should still depend only on db, got %v", api.DependsOn)
	}

	if resolved.Nodes[0].ID != "pre-hook/ready" {
		t.Fatalf("pre-hook should be ordered first, got %v", nodeIDs(resolved.Nodes))
	}
}

func TestPreHookChainGatesOnTerminalStepsOnly(t *testing.T) {
	t.Parallel()

	suite := preHookSuite(strings.Join([]string{
		`probe = task.run(file="probe.py", image="python:3.12")`,
		`verify = task.run(file="verify.py", image="python:3.12", after=[probe])`,
	}, "\n"))

	resolved, err := ResolveRuntime(suite, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got := nodeByID(t, resolved.Nodes, "pre-hook/verify").DependsOn; len(got) != 1 || got[0] != "pre-hook/probe" {
		t.Fatalf("pre-hook edges should be namespaced, got %v", got)
	}

	// Only the tail of the pre-hook chain gates the suite; probe is reached
	// transitively through verify.
	if got := nodeByID(t, resolved.Nodes, "db").DependsOn; len(got) != 1 || got[0] != "pre-hook/verify" {
		t.Fatalf("db should wait on the terminal pre-hook step, got %v", got)
	}
}

func TestSuiteWithoutPreHookIsUnchanged(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveRuntime(preHookSuite(""), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(resolved.Nodes) != 2 {
		t.Fatalf("expected only the suite steps, got %v", nodeIDs(resolved.Nodes))
	}
	if got := nodeByID(t, resolved.Nodes, "db").DependsOn; len(got) != 0 {
		t.Fatalf("db should have no dependencies, got %v", got)
	}
}

func TestPreHookRejectsSuiteImport(t *testing.T) {
	t.Parallel()

	suite := preHookSuite(`dep = suite.run(ref="other-suite")`)

	if _, err := ResolveRuntime(suite, nil); err == nil {
		t.Fatal("expected a suite import inside the pre-hook to be rejected")
	} else if !strings.Contains(err.Error(), "cannot import other suites") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreHookRejectsEmptyTopology(t *testing.T) {
	t.Parallel()

	suite := preHookSuite(`x = 1`)

	if _, err := ResolveRuntime(suite, nil); err == nil {
		t.Fatal("expected a pre-hook registering no steps to be rejected")
	} else if !strings.Contains(err.Error(), "invalid pre-hook") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreHookIDsCannotCollideWithSuiteSteps(t *testing.T) {
	t.Parallel()

	// Same identifier on both sides: namespacing is what keeps them distinct.
	suite := preHookSuite(`db = task.run(file="check.py", image="python:3.12")`)

	resolved, err := ResolveRuntime(suite, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	nodeByID(t, resolved.Nodes, "pre-hook/db")
	if got := nodeByID(t, resolved.Nodes, "db").Kind; got != "service" {
		t.Fatalf("suite's own db should survive untouched, got kind %q", got)
	}
}

func TestNestedSuiteKeepsItsOwnPreHook(t *testing.T) {
	t.Parallel()

	child := Definition{
		ID:         "auth-suite",
		Title:      "Auth Suite",
		Repository: "localhost:5000/core/auth-suite",
		Version:    "workspace",
		SuiteStar:  `api = service.run()`,
		SourceFiles: []SourceFile{
			{Path: PreHookFile, Content: `ready = task.run(file="check.py", image="python:3.12")`},
		},
	}

	parent := Definition{
		ID:         "parent-suite",
		Title:      "Parent Suite",
		Repository: "localhost:5000/core/parent-suite",
		Version:    "workspace",
		SuiteStar:  `auth = suite.run(ref="auth-module")`,
		SourceFiles: []SourceFile{
			{Path: "dependencies.yaml", Content: strings.TrimSpace(`
dependencies:
  auth-module:
    ref: localhost:5000/core/auth-suite
    version: workspace
`)},
		},
	}

	topology, err := ResolveTopology(parent, []Definition{parent, child})
	if err != nil {
		t.Fatalf("resolve topology: %v", err)
	}

	gate := nodeByID(t, topology, "auth/pre-hook/ready")
	if len(gate.DependsOn) != 0 {
		t.Fatalf("imported gate should have no dependencies, got %v", gate.DependsOn)
	}
	if got := nodeByID(t, topology, "auth/api").DependsOn; len(got) != 1 || got[0] != "auth/pre-hook/ready" {
		t.Fatalf("imported suite step should wait on its own gate, got %v", got)
	}
}
