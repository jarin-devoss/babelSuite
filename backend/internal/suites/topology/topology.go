package topology

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/babelsuite/babelsuite/internal/strutil"
)

type missingDependencyError struct{ node, dependency string }

func (e *missingDependencyError) Error() string {
	return fmt.Sprintf("invalid suite topology: %q depends on missing step %q", e.node, e.dependency)
}
func (e *missingDependencyError) Is(target error) bool { return target == ErrMissingDependency }

type topologyLinearCycleError struct{}

func (e *topologyLinearCycleError) Error() string {
	return "invalid suite topology: dependency cycle detected"
}
func (e *topologyLinearCycleError) Is(target error) bool { return target == ErrTopologyCycle }

type dependencyManifest struct {
	Dependencies map[string]dependencyEntry `yaml:"dependencies"`
}

type dependencyEntry struct {
	Ref     string            `yaml:"ref"`
	Version string            `yaml:"version,omitempty"`
	Profile string            `yaml:"profile,omitempty"`
	Inputs  map[string]string `yaml:"inputs,omitempty"`
}

func (e *dependencyEntry) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var ref string
		if err := value.Decode(&ref); err != nil {
			return err
		}
		e.Ref = strings.TrimSpace(ref)
		return nil
	case yaml.MappingNode:
		type raw dependencyEntry
		var decoded raw
		if err := value.Decode(&decoded); err != nil {
			return err
		}
		*e = dependencyEntry(decoded)
		e.Ref = strings.TrimSpace(e.Ref)
		e.Version = strings.TrimSpace(e.Version)
		e.Profile = strings.TrimSpace(e.Profile)
		return nil
	default:
		return fmt.Errorf("dependency entry must be a string or object")
	}
}

type topologyImport struct {
	EntryIDs []string
	ExitIDs  []string
	MockIDs  []string
	Nodes    []TopologyNode
}

// Resolved is a suite's topology after its imports were expanded.
type Resolved struct {
	Nodes        []TopologyNode
	Dependencies []ResolvedDependency
	// Imports are the suites imported at every depth, with their mock surfaces.
	Imports []ImportedSuite
}

func ResolveTopology(suite Definition, catalog []Definition) ([]TopologyNode, error) {
	resolved, err := ResolveRuntime(suite, catalog)
	if err != nil {
		return nil, err
	}
	return resolved.Nodes, nil
}

func ResolveRuntime(suite Definition, catalog []Definition) (Resolved, error) {
	return ResolveRuntimeWithModules(suite, catalog, nil)
}

// ResolveRuntimeWithModules is like ResolveRuntime but accepts an OCI-backed
// ModuleResolver so the execution path can pull @babelsuite/* module files from
// the catalog instead of falling back to the embedded stdlib.
func ResolveRuntimeWithModules(suite Definition, catalog []Definition, resolve ModuleResolver) (Resolved, error) {
	return ResolveRuntimeWithEnv(suite, catalog, resolve, nil)
}

// ResolveRuntimeWithEnv resolves the suite's topology with env as the `env`
// global of its suite.star: the environment of the profile the run was launched
// with. A nested suite does not see it: a dependency is a black box with
// declared inputs, so it reads its own declared profile's env instead.
func ResolveRuntimeWithEnv(suite Definition, catalog []Definition, resolve ModuleResolver, env map[string]string) (Resolved, error) {
	r := newTopologyResolver(catalog)
	r.moduleResolver = resolve
	return r.resolveSuite(suite, nil, env)
}

func ResolveDefinitionTopology(suite Definition, catalog []Definition) Definition {
	// The catalog and the graph have no launch, so they show what a launch with
	// the suite's default profile builds.
	resolved, err := ResolveRuntimeWithEnv(suite, catalog, nil, DefaultProfileEnv(suite))
	if err != nil {
		suite.Topology = nil
		suite.ResolvedDependencies = nil
		suite.TopologyError = err.Error()
		return suite
	}
	suite.Topology = resolved.Nodes
	suite.ResolvedDependencies = resolved.Dependencies
	suite.ImportedSuites = resolved.Imports
	suite.TopologyError = ""
	return suite
}

type topologyResolver struct {
	byID           map[string]Definition
	cached         map[string]Resolved
	moduleResolver ModuleResolver
}

func newTopologyResolver(catalog []Definition) *topologyResolver {
	byID := make(map[string]Definition, len(catalog))
	for _, suite := range catalog {
		byID[strings.TrimSpace(suite.ID)] = suite
	}
	return &topologyResolver{
		byID:   byID,
		cached: make(map[string]Resolved),
	}
}

func (r *topologyResolver) resolveSuite(suite Definition, stack []string, env map[string]string) (Resolved, error) {
	id := strings.TrimSpace(suite.ID)
	cacheKey := id + "\x00" + envKey(env)
	if cached, ok := r.cached[cacheKey]; ok {
		return cloneResolvedTopology(cached), nil
	}
	if err := ValidateDefinition(suite); err != nil {
		return Resolved{}, fmt.Errorf("invalid suite package: %w", err)
	}
	if containsString(stack, id) {
		path := append(append([]string{}, stack...), id)
		return Resolved{}, fmt.Errorf("invalid suite topology: nested suite cycle detected: %s", strings.Join(path, " -> "))
	}

	rawNodes, err := parseRawTopologyWithEnv(suite.SuiteStar, r.moduleResolver, env)
	if err != nil {
		return Resolved{}, err
	}
	rawNodes = resolveAssignmentDependencies(rawNodes)
	manifest, err := parseDependencyManifest(suite.SourceFiles)
	if err != nil {
		return Resolved{}, err
	}
	locks, err := parseDependencyLockManifest(suite.SourceFiles)
	if err != nil {
		return Resolved{}, err
	}

	imports := make(map[string]topologyImport)
	dependencies := make([]ResolvedDependency, 0)
	var importedSuites []ImportedSuite
	nextStack := append(append([]string{}, stack...), id)
	for _, raw := range rawNodes {
		if raw.Kind != "suite" {
			continue
		}

		entry, ok := manifest[raw.Ref]
		if !ok {
			return Resolved{}, fmt.Errorf("invalid suite topology: missing dependency alias %q", raw.Ref)
		}

		dependency, childSuite, err := r.resolveDependency(raw.Ref, entry, locks[raw.Ref])
		if err != nil {
			return Resolved{}, err
		}

		child, err := r.resolveSuite(childSuite, nextStack, dependencyEnv(childSuite, dependency))
		if err != nil {
			return Resolved{}, err
		}
		imports[raw.ID], err = namespaceImportedTopology(raw, childSuite, child.Nodes, dependency)
		if err != nil {
			return Resolved{}, err
		}
		importedSuites = appendImportedSuite(importedSuites, ImportedSuite{ID: childSuite.ID, APISurfaces: cloneSurfaces(childSuite.APISurfaces)})
		for _, nested := range child.Imports {
			importedSuites = appendImportedSuite(importedSuites, nested)
		}
		dependencies = append(dependencies, dependency)
	}

	final := make([]TopologyNode, 0, len(rawNodes))
	for _, raw := range rawNodes {
		if raw.Kind == "suite" {
			expansion := imports[raw.ID]
			rootDependsOn := expandImportedDependencies(raw.DependsOn, imports)
			for _, node := range expansion.Nodes {
				if containsString(expansion.EntryIDs, node.ID) {
					node.DependsOn = append(append([]string{}, rootDependsOn...), node.DependsOn...)
					node.DependsOn = normalizeDependencies(node.DependsOn)
				}
				final = append(final, node)
			}
			continue
		}

		node, err := topologyNodeFromRaw(raw, suite, imports, env)
		if err != nil {
			return Resolved{}, err
		}
		final = append(final, node)
	}

	preHook, err := resolvePreHookNodes(suite, r.moduleResolver, env)
	if err != nil {
		return Resolved{}, err
	}
	final = prependPreHookNodes(final, preHook)

	for index := range final {
		final[index].Order = index
	}

	resolved, err := resolveTopology(final)
	if err != nil {
		return Resolved{}, err
	}
	if err := validateTrafficTargets(suite, final); err != nil {
		return Resolved{}, err
	}
	if err := validateSecurityTargets(suite, final); err != nil {
		return Resolved{}, err
	}
	result := Resolved{
		Nodes:        resolved,
		Dependencies: cloneResolvedDependencies(dependencies),
		Imports:      importedSuites,
	}
	r.cached[cacheKey] = cloneResolvedTopology(result)
	return cloneResolvedTopology(result), nil
}

func topologyNodeFromRaw(raw rawTopologyNode, suite Definition, imports map[string]topologyImport, env map[string]string) (TopologyNode, error) {
	node := TopologyNode{
		ID:                raw.ID,
		Name:              raw.Name,
		Kind:              raw.Kind,
		Variant:           raw.Variant,
		Image:             raw.Image,
		File:              raw.File,
		Commands:          append([]string{}, raw.Commands...),
		Message:           raw.Message,
		Harden:            raw.Harden,
		RuntimeEnv:        cloneStringMap(raw.Env),
		DependsOn:         expandImportedDependencies(append(append([]string{}, raw.DependsOn...), raw.OnFailure...), imports),
		ResetMocks:        expandImportedMockTargets(raw.ResetMocks, imports),
		OnFailure:         expandImportedDependencies(raw.OnFailure, imports),
		ContinueOnFailure: raw.ContinueOnFailure,
		Evaluation:        cloneStepEvaluation(raw.Evaluation),
		ArtifactExports:   append([]ArtifactExport{}, raw.Exports...),
		SourceSuiteID:     suite.ID,
		SourceSuiteTitle:  suite.Title,
		SourceRepository:  suite.Repository,
		SourceVersion:     suite.Version,
	}
	if raw.Kind == "traffic" {
		spec, err := resolveLoadSpec(raw, suite.SourceFiles, env)
		if err != nil {
			return TopologyNode{}, err
		}
		node.Load = spec
	}
	if raw.Kind == "security" {
		node.Security = &SecuritySpec{
			Variant:       raw.Variant,
			Target:        raw.Target,
			Technique:     raw.Technique,
			FloodPath:     raw.FloodPath,
			FloodRate:     raw.FloodRate,
			FloodDuration: raw.FloodDuration,
			FloodThrottle: raw.FloodThrottle,
			FailOn:        raw.FailOn,
		}
	}
	if raw.Kind == NodeKindPlugin {
		node.Plugin = &PluginSpec{
			Name:   raw.Variant,
			Op:     raw.PluginOp,
			Config: raw.PluginConfig,
		}
	}
	return node, nil
}

func (r *topologyResolver) resolveDependency(alias string, entry dependencyEntry, lock dependencyLockEntry) (ResolvedDependency, Definition, error) {
	ref := strings.TrimSpace(entry.Ref)
	if ref == "" {
		return ResolvedDependency{}, Definition{}, fmt.Errorf("invalid suite topology: dependency entry %q is missing ref", alias)
	}

	normalizedRef, versionFromRef := splitDependencyRef(ref)
	version := strings.TrimSpace(entry.Version)
	if version != "" && versionFromRef != "" && !dependencyHasPinnedDigest(ref) && !strings.EqualFold(version, versionFromRef) {
		return ResolvedDependency{}, Definition{}, fmt.Errorf("invalid suite topology: dependency %q declares mismatched version %q for ref %q", alias, version, ref)
	}
	if version == "" {
		if dependencyHasPinnedDigest(ref) {
			version = strings.TrimSpace(lock.Version)
		} else {
			version = versionFromRef
		}
	}
	if version == "" {
		version = strings.TrimSpace(lock.Version)
	}

	if dependencyUsesLatest(ref) || strings.EqualFold(version, "latest") {
		return ResolvedDependency{}, Definition{}, fmt.Errorf("invalid suite topology: dependency %q must use a pinned version instead of latest", alias)
	}

	lockResolved := strings.TrimSpace(lock.Resolved)
	lockDigest := strutil.FirstNonEmpty(strings.TrimSpace(lock.Digest), dependencyDigest(lockResolved))

	candidates := make([]Definition, 0)
	for _, suite := range r.byID {
		if suite.ID == ref || suite.ID == normalizedRef || suite.Repository == ref || suite.Repository == normalizedRef {
			candidates = append(candidates, suite)
		}
	}
	if len(candidates) == 0 {
		return ResolvedDependency{}, Definition{}, fmt.Errorf("invalid suite topology: dependency ref %q could not be resolved", ref)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})

	var selected Definition
	if version == "" {
		selected = candidates[0]
	} else {
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.Version) == version {
				selected = candidate
				break
			}
		}
		if strings.TrimSpace(selected.ID) == "" {
			return ResolvedDependency{}, Definition{}, fmt.Errorf("invalid suite topology: dependency ref %q with version %q could not be resolved", normalizedRef, version)
		}
	}

	profile, err := resolveDependencyProfile(selected, strutil.FirstNonEmpty(strings.TrimSpace(entry.Profile), strings.TrimSpace(lock.Profile)))
	if err != nil {
		return ResolvedDependency{}, Definition{}, err
	}

	inputs := mergeStringMaps(lock.Inputs, entry.Inputs)
	baseResolved := strings.TrimSpace(strutil.FirstNonEmpty(lockResolved, ref))
	if lockDigest != "" && !strings.Contains(baseResolved, "@") {
		baseResolved = strings.TrimSpace(selected.Repository)
		if baseResolved == "" {
			baseResolved = normalizedRef
		}
		baseResolved = baseResolved + "@" + lockDigest
	}
	if baseResolved == strings.TrimSpace(ref) && version != "" && !dependencyHasPinnedDigest(baseResolved) {
		baseResolved = normalizedRef + ":" + version
	}

	dependency := ResolvedDependency{
		Alias:       alias,
		Ref:         ref,
		Version:     strutil.FirstNonEmpty(version, strings.TrimSpace(selected.Version)),
		Resolved:    baseResolved,
		Digest:      strutil.FirstNonEmpty(lockDigest, dependencyDigest(baseResolved), dependencyDigest(ref)),
		Profile:     profile,
		Inputs:      inputs,
		SuiteID:     selected.ID,
		SuiteTitle:  selected.Title,
		Repository:  selected.Repository,
		SourceFiles: cloneSourceFiles(selected.SourceFiles),
	}

	return dependency, selected, nil
}

func parseDependencyManifest(sourceFiles []SourceFile) (map[string]dependencyEntry, error) {
	for _, file := range sourceFiles {
		path := strings.TrimSpace(strings.ToLower(file.Path))
		if path != "dependencies.yaml" && path != "dependencies.yml" {
			continue
		}

		var document dependencyManifest
		if err := yaml.Unmarshal([]byte(file.Content), &document); err != nil {
			return nil, fmt.Errorf("invalid suite topology: could not parse %s: %w", file.Path, err)
		}

		manifest := make(map[string]dependencyEntry, len(document.Dependencies))
		for alias, entry := range document.Dependencies {
			trimmedAlias := strings.TrimSpace(alias)
			if trimmedAlias == "" {
				continue
			}
			manifest[trimmedAlias] = entry
		}
		return manifest, nil
	}

	return map[string]dependencyEntry{}, nil
}

// ParseRaw evaluates a suite.star and returns its steps, to check that it is one.
func ParseRaw(suiteStar string, resolve ModuleResolver) ([]rawTopologyNode, error) {
	return parseRawTopology(suiteStar, resolve)
}

func parseRawTopology(suiteStar string, resolve ModuleResolver) ([]rawTopologyNode, error) {
	return evalStarlarkTopologyWithEnv(suiteStar, resolve, nil)
}

func parseRawTopologyWithEnv(suiteStar string, resolve ModuleResolver, env map[string]string) ([]rawTopologyNode, error) {
	return evalStarlarkTopologyWithEnv(suiteStar, resolve, env)
}

func namespaceImportedTopology(raw rawTopologyNode, childSuite Definition, childNodes []TopologyNode, dependency ResolvedDependency) (topologyImport, error) {
	if len(childNodes) == 0 {
		return topologyImport{}, nil
	}

	profileRuntime, err := parseDependencyProfileRuntime(childSuite.SourceFiles, dependency.Profile)
	if err != nil {
		return topologyImport{}, err
	}

	rootIDs := make(map[string]struct{})
	dependants := make(map[string]int, len(childNodes))
	for _, node := range childNodes {
		if len(node.DependsOn) == 0 {
			rootIDs[node.ID] = struct{}{}
		}
		for _, dependency := range node.DependsOn {
			dependants[dependency]++
		}
	}

	namespaced := make([]TopologyNode, len(childNodes))
	idMap := make(map[string]string, len(childNodes))
	for index, node := range childNodes {
		idMap[node.ID] = raw.ID + "/" + node.ID
		namespaced[index] = node
		namespaced[index].ID = raw.ID + "/" + node.ID
		namespaced[index].Name = raw.Name + "/" + node.Name
		namespaced[index].Load = cloneLoadSpec(node.Load)
		namespaced[index].Evaluation = cloneStepEvaluation(node.Evaluation)
		namespaced[index].Level = 0
		namespaced[index].Order = index
		namespaced[index].SourceSuiteID = strutil.FirstNonEmpty(node.SourceSuiteID, childSuite.ID)
		namespaced[index].SourceSuiteTitle = strutil.FirstNonEmpty(node.SourceSuiteTitle, childSuite.Title)
		namespaced[index].SourceRepository = strutil.FirstNonEmpty(node.SourceRepository, childSuite.Repository)
		namespaced[index].SourceVersion = strutil.FirstNonEmpty(node.SourceVersion, childSuite.Version)
		namespaced[index].DependencyAlias = strutil.FirstNonEmpty(node.DependencyAlias, dependency.Alias)
		namespaced[index].ResolvedRef = strutil.FirstNonEmpty(node.ResolvedRef, dependency.Resolved)
		namespaced[index].Digest = strutil.FirstNonEmpty(node.Digest, dependency.Digest)
		namespaced[index].RuntimeProfile = strutil.FirstNonEmpty(node.RuntimeProfile, dependency.Profile)
		namespaced[index].RuntimeEnv = mergeStringMaps(node.RuntimeEnv, buildDependencyRuntimeEnv(node, childSuite, dependency, profileRuntime))
		if node.Kind == "mock" {
			namespaced[index].RuntimeHeaders = mergeStringMaps(node.RuntimeHeaders, buildDependencyRuntimeHeaders(dependency))
		}
	}
	for index := range namespaced {
		remapped := make([]string, 0, len(namespaced[index].DependsOn))
		for _, dependency := range namespaced[index].DependsOn {
			if mapped := idMap[dependency]; mapped != "" {
				remapped = append(remapped, mapped)
			}
		}
		namespaced[index].DependsOn = remapped
		if len(namespaced[index].OnFailure) > 0 {
			remappedOnFailure := make([]string, 0, len(namespaced[index].OnFailure))
			for _, dependency := range namespaced[index].OnFailure {
				if mapped := idMap[dependency]; mapped != "" {
					remappedOnFailure = append(remappedOnFailure, mapped)
				}
			}
			namespaced[index].OnFailure = remappedOnFailure
		}
		if len(namespaced[index].ResetMocks) > 0 {
			remappedResetMocks := make([]string, 0, len(namespaced[index].ResetMocks))
			for _, dependency := range namespaced[index].ResetMocks {
				if mapped := idMap[dependency]; mapped != "" {
					remappedResetMocks = append(remappedResetMocks, mapped)
				}
			}
			namespaced[index].ResetMocks = normalizeDependencies(remappedResetMocks)
		}
	}

	entryIDs := make([]string, 0, len(rootIDs))
	exitIDs := make([]string, 0, len(childNodes))
	mockIDs := make([]string, 0, len(childNodes))
	for _, node := range childNodes {
		if _, ok := rootIDs[node.ID]; ok {
			entryIDs = append(entryIDs, idMap[node.ID])
		}
		if dependants[node.ID] == 0 {
			exitIDs = append(exitIDs, idMap[node.ID])
		}
		if node.Kind == "mock" {
			mockIDs = append(mockIDs, idMap[node.ID])
		}
	}
	sort.Strings(entryIDs)
	sort.Strings(exitIDs)
	sort.Strings(mockIDs)

	return topologyImport{
		EntryIDs: entryIDs,
		ExitIDs:  exitIDs,
		MockIDs:  mockIDs,
		Nodes:    namespaced,
	}, nil
}

func expandImportedDependencies(dependsOn []string, imports map[string]topologyImport) []string {
	expanded := make([]string, 0, len(dependsOn))
	for _, dependency := range dependsOn {
		if imported, ok := imports[dependency]; ok && len(imported.ExitIDs) > 0 {
			expanded = append(expanded, imported.ExitIDs...)
			continue
		}
		expanded = append(expanded, dependency)
	}
	return normalizeDependencies(expanded)
}

func expandImportedMockTargets(targets []string, imports map[string]topologyImport) []string {
	expanded := make([]string, 0, len(targets))
	for _, target := range targets {
		if imported, ok := imports[target]; ok && len(imported.MockIDs) > 0 {
			expanded = append(expanded, imported.MockIDs...)
			continue
		}
		expanded = append(expanded, target)
	}
	return normalizeDependencies(expanded)
}

func resolveAssignmentDependencies(nodes []rawTopologyNode) []rawTopologyNode {
	if len(nodes) == 0 {
		return nil
	}

	assignmentToID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Assignment) == "" || strings.TrimSpace(node.ID) == "" {
			continue
		}
		assignmentToID[node.Assignment] = node.ID
	}

	resolved := make([]rawTopologyNode, len(nodes))
	copy(resolved, nodes)
	for index := range resolved {
		if len(resolved[index].DependsOn) > 0 {
			remapped := make([]string, 0, len(resolved[index].DependsOn))
			for _, dependency := range resolved[index].DependsOn {
				if mapped, ok := assignmentToID[dependency]; ok {
					remapped = append(remapped, mapped)
					continue
				}
				remapped = append(remapped, dependency)
			}
			resolved[index].DependsOn = normalizeDependencies(remapped)
		}
		if len(resolved[index].OnFailure) > 0 {
			remapped := make([]string, 0, len(resolved[index].OnFailure))
			for _, dependency := range resolved[index].OnFailure {
				if mapped, ok := assignmentToID[dependency]; ok {
					remapped = append(remapped, mapped)
					continue
				}
				remapped = append(remapped, dependency)
			}
			resolved[index].OnFailure = normalizeDependencies(remapped)
		}
		if len(resolved[index].ResetMocks) > 0 {
			remapped := make([]string, 0, len(resolved[index].ResetMocks))
			for _, dependency := range resolved[index].ResetMocks {
				if mapped, ok := assignmentToID[dependency]; ok {
					remapped = append(remapped, mapped)
					continue
				}
				remapped = append(remapped, dependency)
			}
			resolved[index].ResetMocks = normalizeDependencies(remapped)
		}
	}
	return resolved
}

func resolveTopology(nodes []TopologyNode) ([]TopologyNode, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	byID := make(map[string]*TopologyNode, len(nodes))
	for index := range nodes {
		nodes[index].DependsOn = normalizeDependencies(nodes[index].DependsOn)
		nodes[index].ResetMocks = normalizeDependencies(nodes[index].ResetMocks)
		if _, exists := byID[nodes[index].ID]; exists {
			return nil, fmt.Errorf("invalid suite topology: duplicate step %q", nodes[index].ID)
		}
		byID[nodes[index].ID] = &nodes[index]
	}

	for index := range nodes {
		if len(nodes[index].ResetMocks) == 0 {
			continue
		}
		if nodes[index].Kind != "test" {
			return nil, fmt.Errorf("invalid suite topology: reset_mocks is only supported on test.run nodes")
		}
		for _, target := range nodes[index].ResetMocks {
			dependency := byID[target]
			if dependency == nil {
				return nil, fmt.Errorf("invalid suite topology: %q resets missing mock %q", nodes[index].ID, target)
			}
			if dependency.Kind != "mock" {
				return nil, fmt.Errorf("invalid suite topology: %q reset_mocks target %q is not a mock node", nodes[index].ID, target)
			}
		}
	}

	indegree := make(map[string]int, len(nodes))
	dependants := make(map[string][]string, len(nodes))
	for index := range nodes {
		node := &nodes[index]
		indegree[node.ID] = len(node.DependsOn)
		for _, dependency := range node.DependsOn {
			if _, exists := byID[dependency]; !exists {
				return nil, &missingDependencyError{node: node.ID, dependency: dependency}
			}
			dependants[dependency] = append(dependants[dependency], node.ID)
		}
	}

	ready := make([]string, 0)
	for _, node := range nodes {
		if indegree[node.ID] == 0 {
			ready = append(ready, node.ID)
		}
	}
	sortTopologyIDs(ready, byID)

	ordered := make([]TopologyNode, 0, len(nodes))
	level := 0
	for len(ready) > 0 {
		current := append([]string{}, ready...)
		nextReady := make([]string, 0)

		for _, id := range current {
			node := *byID[id]
			node.Level = level
			ordered = append(ordered, node)

			for _, dependant := range dependants[id] {
				indegree[dependant]--
				if indegree[dependant] == 0 {
					nextReady = append(nextReady, dependant)
				}
			}
		}

		sortTopologyIDs(nextReady, byID)
		ready = nextReady
		level++
	}

	if len(ordered) != len(nodes) {
		return nil, &topologyLinearCycleError{}
	}

	return ordered, nil
}

func sortTopologyIDs(ids []string, byID map[string]*TopologyNode) {
	sort.Slice(ids, func(i, j int) bool {
		left := byID[ids[i]]
		right := byID[ids[j]]
		if left == nil || right == nil {
			return ids[i] < ids[j]
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return left.Name < right.Name
	})
}

func normalizeDependencies(dependsOn []string) []string {
	if len(dependsOn) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(dependsOn))
	result := make([]string, 0, len(dependsOn))
	for _, dependency := range dependsOn {
		trimmed := strings.TrimSpace(dependency)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func splitDependencyRef(value string) (string, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ""
	}
	if repository, digest, ok := strings.Cut(trimmed, "@"); ok {
		return repository, digest
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon > lastSlash {
		return trimmed[:lastColon], trimmed[lastColon+1:]
	}
	return trimmed, ""
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// envKey identifies an env so one suite imported under two profiles is resolved
// once for each.
func envKey(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key + "=" + env[key] + "\x00")
	}
	return b.String()
}

// defaultProfileEnv is the env: of the suite's default profile, which is what a
// launch without a choice runs.
func DefaultProfileEnv(suite Definition) map[string]string {
	name := ""
	for _, profile := range suite.Profiles {
		if profile.Default {
			name = profile.FileName
			break
		}
	}
	if name == "" && len(suite.Profiles) > 0 {
		name = suite.Profiles[0].FileName
	}
	runtime, err := parseDependencyProfileRuntime(suite.SourceFiles, name)
	if err != nil {
		return nil
	}
	return runtime.Env
}

// dependencyEnv is the env a nested suite's suite.star sees: its declared
// profile's, or its default one.
func dependencyEnv(child Definition, dependency ResolvedDependency) map[string]string {
	if strings.TrimSpace(dependency.Profile) == "" {
		return DefaultProfileEnv(child)
	}
	runtime, err := parseDependencyProfileRuntime(child.SourceFiles, dependency.Profile)
	if err != nil {
		return nil
	}
	return runtime.Env
}

// appendImportedSuite adds a suite once: two paths to the same suite are one
// import as far as the gateway is concerned.
func appendImportedSuite(list []ImportedSuite, item ImportedSuite) []ImportedSuite {
	for _, existing := range list {
		if existing.ID == item.ID {
			return list
		}
	}
	return append(list, item)
}
