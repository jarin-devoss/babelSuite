package suites

import "github.com/babelsuite/babelsuite/internal/suites/topology"

// The resolver of a suite's topology lives in suites/topology.
type resolvedTopology = topology.Resolved

func ResolveTopology(suite Definition, catalog []Definition) ([]TopologyNode, error) {
	return topology.ResolveTopology(suite, catalog)
}

func ResolveRuntime(suite Definition, catalog []Definition) (resolvedTopology, error) {
	return topology.ResolveRuntime(suite, catalog)
}

func ResolveRuntimeWithModules(suite Definition, catalog []Definition, resolve ModuleResolver) (resolvedTopology, error) {
	return topology.ResolveRuntimeWithModules(suite, catalog, resolve)
}

func ResolveRuntimeWithEnv(suite Definition, catalog []Definition, resolve ModuleResolver, env map[string]string) (resolvedTopology, error) {
	return topology.ResolveRuntimeWithEnv(suite, catalog, resolve, env)
}

func ResolveDefinitionTopology(suite Definition, catalog []Definition) Definition {
	return topology.ResolveDefinitionTopology(suite, catalog)
}

func parseRawTopology(suiteStar string, resolve ModuleResolver) ([]rawTopologyNode, error) {
	return topology.ParseRaw(suiteStar, resolve)
}

func defaultProfileEnv(suite Definition) map[string]string { return topology.DefaultProfileEnv(suite) }
