package suites

import (
	"fmt"
	"strings"
)

// PreHookFile is the suite-relative path that, when present in a suite package,
// is evaluated and run to completion before any suite.star step starts.
const PreHookFile = "pre-hook.star"

// preHookNamespace prefixes every pre-hook step id so it can never collide with
// a suite.star step. Starlark identifiers cannot contain "/" or "-", so neither
// a local step nor an imported suite can produce an id in this namespace.
const preHookNamespace = "pre-hook"

// resolvePreHookNodes evaluates the suite's pre-hook.star, if it has one, and
// returns its steps namespaced under preHookNamespace. Returns nil when the
// suite ships no pre-hook.
func resolvePreHookNodes(suite Definition, resolve ModuleResolver) ([]TopologyNode, error) {
	source, ok := explicitSuiteSourceContent(suite.SourceFiles, PreHookFile)
	if !ok || strings.TrimSpace(source) == "" {
		return nil, nil
	}

	rawNodes, err := parseRawTopology(source, resolve)
	if err != nil {
		return nil, fmt.Errorf("invalid pre-hook: %w", err)
	}
	rawNodes = resolveAssignmentDependencies(rawNodes)

	nodes := make([]TopologyNode, 0, len(rawNodes))
	for _, raw := range rawNodes {
		// Importing a suite pulls in a dependency manifest and its own
		// pre-hook, which a readiness gate has no way to express sensibly.
		if raw.Kind == "suite" {
			return nil, fmt.Errorf("invalid pre-hook: %s cannot import other suites", PreHookFile)
		}
		node, err := topologyNodeFromRaw(raw, suite, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid pre-hook: %w", err)
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("invalid pre-hook: %s registers no steps", PreHookFile)
	}

	return namespacePreHookNodes(nodes), nil
}

func namespacePreHookNodes(nodes []TopologyNode) []TopologyNode {
	qualify := func(id string) string { return preHookNamespace + "/" + id }

	for index := range nodes {
		nodes[index].ID = qualify(nodes[index].ID)
		nodes[index].Name = preHookNamespace + "/" + nodes[index].Name
		nodes[index].DependsOn = mapStrings(nodes[index].DependsOn, qualify)
		nodes[index].OnFailure = mapStrings(nodes[index].OnFailure, qualify)
		nodes[index].ResetMocks = mapStrings(nodes[index].ResetMocks, qualify)
	}
	return nodes
}

// prependPreHookNodes puts the pre-hook ahead of the suite: every suite step
// that would otherwise start immediately is made to wait on the pre-hook's
// terminal steps. Dependencies are transitive through the execution queue, so
// gating the roots gates the whole suite — and a failed pre-hook step leaves
// the suite steps skipped rather than running against an unready environment.
func prependPreHookNodes(suiteNodes, preHookNodes []TopologyNode) []TopologyNode {
	if len(preHookNodes) == 0 {
		return suiteNodes
	}

	depended := make(map[string]bool, len(preHookNodes))
	for _, node := range preHookNodes {
		for _, dependency := range node.DependsOn {
			depended[dependency] = true
		}
	}
	gates := make([]string, 0, len(preHookNodes))
	for _, node := range preHookNodes {
		if !depended[node.ID] {
			gates = append(gates, node.ID)
		}
	}

	for index := range suiteNodes {
		if len(suiteNodes[index].DependsOn) == 0 {
			suiteNodes[index].DependsOn = append([]string{}, gates...)
		}
	}

	return append(preHookNodes, suiteNodes...)
}

func mapStrings(values []string, fn func(string) string) []string {
	if len(values) == 0 {
		return values
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, fn(value))
	}
	return result
}
