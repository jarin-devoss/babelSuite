package execution

import (
	"reflect"
	"testing"
)

func svcNode(id, name, owner string) topologyNode {
	return topologyNode{ID: id, Name: name, Kind: "service", SourceSuiteID: owner}
}

func taskNode(id, name, owner string) topologyNode {
	return topologyNode{ID: id, Name: name, Kind: "task", SourceSuiteID: owner}
}

// The case that must not regress: one suite's names are already unique on the
// shared network, so every alias stays exactly what it was.
func TestUncontestedNamesAreUnchanged(t *testing.T) {
	t.Parallel()

	nodes := []topologyNode{
		svcNode("db", "db", "payment-suite"),
		svcNode("cache", "cache", "payment-suite"),
		taskNode("migrate", "migrate", "payment-suite"),
	}
	plan := buildNetworkPlan(nodes, "payment-suite")

	if got := plan.contestedNames(); len(got) != 0 {
		t.Fatalf("expected nothing contested, got %v", got)
	}
	if got, want := plan.aliasesForNode(nodes[0], "payment-suite"), []string{"db"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("service aliases = %v, want %v", got, want)
	}
	// A task is dialled by nobody, so it publishes no name at all.
	if got := plan.aliasesForNode(nodes[2], "payment-suite"); got != nil {
		t.Fatalf("a task should publish no alias, got %v", got)
	}
}

// The composite-readiness shape. The suite being run keeps the bare name — so
// its own steps are untouched — and each imported suite's service answers to a
// qualified name instead of sharing one alias four ways.
func TestOnlyImportedSuitesQualifyAContestedName(t *testing.T) {
	t.Parallel()

	nodes := []topologyNode{
		svcNode("db", "db", "composite-readiness"),
		svcNode("identity-broker/db", "identity-broker/db", "identity-broker"),
		svcNode("payment-suite/db", "payment-suite/db", "payment-suite"),
		svcNode("returns-control-plane/db", "returns-control-plane/db", "returns-control-plane"),
		svcNode("identity-broker/broker-api", "identity-broker/broker-api", "identity-broker"),
	}
	plan := buildNetworkPlan(nodes, "composite-readiness")

	if got, want := plan.contestedNames(), []string{"db"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("contested = %v, want %v", got, want)
	}

	for _, tc := range []struct {
		name string
		node topologyNode
		want []string
	}{
		// The root suite keeps "db", so its own connect() still resolves.
		{"root suite keeps the bare name", nodes[0], []string{"db"}},
		{"imported db qualifies", nodes[1], []string{"identity-broker-db"}},
		{"second imported db qualifies", nodes[2], []string{"payment-suite-db"}},
		{"third imported db qualifies", nodes[3], []string{"returns-control-plane-db"}},
		// Uncontested, so cross-suite addressing of it is unaffected.
		{"uncontested imported service keeps its name", nodes[4], []string{"broker-api"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := plan.aliasesForNode(tc.node, "composite-readiness"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("aliases = %v, want %v", got, tc.want)
			}
		})
	}
}

// Exactly one container may publish a contested name, otherwise DNS is still
// free to round-robin and nothing has been fixed.
func TestAContestedNameHasExactlyOnePublisher(t *testing.T) {
	t.Parallel()

	nodes := []topologyNode{
		svcNode("db", "db", "parent"),
		svcNode("a/db", "a/db", "a"),
		svcNode("b/db", "b/db", "b"),
	}
	plan := buildNetworkPlan(nodes, "parent")

	seen := map[string]int{}
	for _, node := range nodes {
		for _, alias := range plan.aliasesForNode(node, "parent") {
			seen[alias]++
		}
	}
	for alias, count := range seen {
		if count != 1 {
			t.Fatalf("alias %q is published by %d containers, want 1 (all: %v)", alias, count, seen)
		}
	}
	if _, ok := seen["db"]; !ok {
		t.Fatalf("the root suite should still publish \"db\", got %v", seen)
	}
}

// Only services answer on the network, so tasks and mocks sharing a name across
// suites is not a resolution problem and must not qualify anything.
func TestOnlyServicesCanContestAName(t *testing.T) {
	t.Parallel()

	nodes := []topologyNode{
		taskNode("seed", "seed", "parent"),
		taskNode("dep/seed", "dep/seed", "dep"),
		{ID: "api", Name: "api", Kind: "mock", SourceSuiteID: "parent"},
		{ID: "dep/api", Name: "dep/api", Kind: "mock", SourceSuiteID: "dep"},
	}
	plan := buildNetworkPlan(nodes, "parent")

	if got := plan.contestedNames(); len(got) != 0 {
		t.Fatalf("tasks and mocks should not contest, got %v", got)
	}
}

// A node carrying no SourceSuiteID belongs to the suite being run, so it keeps
// the bare name like any other root node.
func TestNodeWithNoOwnerIsTreatedAsRoot(t *testing.T) {
	t.Parallel()

	nodes := []topologyNode{
		{ID: "db", Name: "db", Kind: "service"},
		svcNode("dep/db", "dep/db", "dep"),
	}
	plan := buildNetworkPlan(nodes, "parent")

	if got, want := plan.aliasesForNode(nodes[0], "parent"), []string{"db"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unowned service aliases = %v, want %v", got, want)
	}
	if got, want := plan.aliasesForNode(nodes[1], "parent"), []string{"dep-db"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency aliases = %v, want %v", got, want)
	}
}

// The qualified name is what a dependency's `inputs:` has to spell by hand, so
// it must stay predictable and DNS-safe.
func TestQualifiedAliasIsPredictableAndDNSSafe(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ owner, alias, want string }{
		{"identity-broker", "db", "identity-broker-db"},
		{"Payment_Suite", "db", "payment-suite-db"},
		{"core-platform/payments", "queue", "core-platform-payments-queue"},
		{"  spaced name  ", "db", "spaced-name-db"},
	} {
		if got := qualifiedAlias(tc.owner, tc.alias); got != tc.want {
			t.Fatalf("qualifiedAlias(%q, %q) = %q, want %q", tc.owner, tc.alias, got, tc.want)
		}
	}
}

// The alias is the last path segment of the node name, matching what the runner
// falls back to when no plan is supplied.
func TestNetworkAliasIsTheLastNameSegment(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, want string }{
		{"db", "db"},
		{"identity-broker/db", "db"},
		{"payment-suite/pre-hook/legacy-ledger-db", "legacy-ledger-db"},
		{"  spaced  ", "spaced"},
		{"", ""},
	} {
		if got := networkAlias(tc.name); got != tc.want {
			t.Fatalf("networkAlias(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
