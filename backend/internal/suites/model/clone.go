package model

// CloneDefinition deep-copies a definition for handing to a caller outside the
// package. Besides unsharing the backing arrays it leaves every slice the JSON
// contract declares as an array non-nil: Go marshals a nil slice as null, and a
// client that trusts the declared type then calls an array method on null.

func CloneDefinition(input Definition) Definition {
	output := input
	output.Labels = CloneStringMap(input.Labels)
	output.Tags = append([]string{}, input.Tags...)
	output.Modules = append([]string{}, input.Modules...)
	output.Contracts = append([]string{}, input.Contracts...)
	output.Profiles = CloneProfiles(input.Profiles)
	output.Folders = CloneFolders(input.Folders)
	output.SeedSources = CloneSourceFiles(input.SeedSources)
	output.SourceFiles = CloneSourceFiles(input.SourceFiles)
	output.Topology = CloneTopology(input.Topology)
	output.ResolvedDependencies = CloneResolvedDependencies(input.ResolvedDependencies)
	output.ImportedSuites = CloneImportedSuites(input.ImportedSuites)
	output.APISurfaces = CloneSurfaces(input.APISurfaces)
	return output
}

func CloneProfiles(input []ProfileOption) []ProfileOption {
	output := make([]ProfileOption, len(input))
	copy(output, input)
	return output
}

func CloneFolders(input []FolderEntry) []FolderEntry {
	output := make([]FolderEntry, len(input))
	for index, folder := range input {
		output[index] = folder
		output[index].Files = append([]string{}, folder.Files...)
	}
	return output
}

func CloneSourceFiles(input []SourceFile) []SourceFile {
	output := make([]SourceFile, len(input))
	copy(output, input)
	return output
}

func CloneTopology(input []TopologyNode) []TopologyNode {
	output := make([]TopologyNode, len(input))
	for index, node := range input {
		output[index] = node
		output[index].DependsOn = append([]string{}, node.DependsOn...)
		output[index].ResetMocks = append([]string{}, node.ResetMocks...)
		output[index].OnFailure = append([]string{}, node.OnFailure...)
		output[index].ContinueOnFailure = node.ContinueOnFailure
		output[index].Evaluation = CloneStepEvaluation(node.Evaluation)
		output[index].ArtifactExports = append([]ArtifactExport{}, node.ArtifactExports...)
		output[index].Load = CloneLoadSpec(node.Load)
		output[index].RuntimeEnv = CloneStringMap(node.RuntimeEnv)
		output[index].RuntimeHeaders = CloneStringMap(node.RuntimeHeaders)
	}
	return output
}

func CloneResolvedDependencies(input []ResolvedDependency) []ResolvedDependency {
	output := make([]ResolvedDependency, len(input))
	for index, dependency := range input {
		output[index] = dependency
		output[index].Inputs = CloneStringMap(dependency.Inputs)
		output[index].SourceFiles = CloneSourceFiles(dependency.SourceFiles)
	}
	return output
}

func CloneSurfaces(input []APISurface) []APISurface {
	output := make([]APISurface, len(input))
	for surfaceIndex, surface := range input {
		output[surfaceIndex] = surface
		output[surfaceIndex].Operations = make([]APIOperation, len(surface.Operations))
		for operationIndex, operation := range surface.Operations {
			output[surfaceIndex].Operations[operationIndex] = operation
			output[surfaceIndex].Operations[operationIndex].MockMetadata = CloneMockMetadata(operation.MockMetadata)
			output[surfaceIndex].Operations[operationIndex].Exchanges = make([]ExchangeExample, len(operation.Exchanges))
			for exchangeIndex, exchange := range operation.Exchanges {
				output[surfaceIndex].Operations[operationIndex].Exchanges[exchangeIndex] = exchange
				output[surfaceIndex].Operations[operationIndex].Exchanges[exchangeIndex].When = append([]MatchCondition{}, exchange.When...)
				output[surfaceIndex].Operations[operationIndex].Exchanges[exchangeIndex].RequestHeaders = CloneHeaders(exchange.RequestHeaders)
				output[surfaceIndex].Operations[operationIndex].Exchanges[exchangeIndex].ResponseHeaders = CloneHeaders(exchange.ResponseHeaders)
			}
		}
	}
	return output
}

func CloneHeaders(input []Header) []Header {
	output := make([]Header, len(input))
	copy(output, input)
	return output
}

func CloneMockMetadata(input MockOperationMetadata) MockOperationMetadata {
	output := input
	output.ParameterConstraints = CloneParameterConstraints(input.ParameterConstraints)
	output.Fallback = CloneMockFallback(input.Fallback)
	output.State = CloneMockState(input.State)
	return output
}

func CloneParameterConstraints(input []ParameterConstraint) []ParameterConstraint {
	output := make([]ParameterConstraint, len(input))
	copy(output, input)
	return output
}

func CloneMockFallback(input *MockFallback) *MockFallback {
	if input == nil {
		return nil
	}

	output := *input
	output.Headers = CloneHeaders(input.Headers)
	return &output
}

func CloneMockState(input *MockState) *MockState {
	if input == nil {
		return nil
	}

	output := *input
	output.Defaults = CloneStringMap(input.Defaults)
	output.Transitions = make([]MockStateTransition, len(input.Transitions))
	for index, transition := range input.Transitions {
		output.Transitions[index] = transition
		output.Transitions[index].Set = CloneStringMap(transition.Set)
		output.Transitions[index].Delete = append([]string{}, transition.Delete...)
		output.Transitions[index].Increment = CloneIntMap(transition.Increment)
	}
	return &output
}

func CloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func CloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func CloneImportedSuites(input []ImportedSuite) []ImportedSuite {
	if len(input) == 0 {
		return nil
	}
	output := make([]ImportedSuite, len(input))
	for index, item := range input {
		output[index] = ImportedSuite{ID: item.ID, APISurfaces: CloneSurfaces(item.APISurfaces)}
	}
	return output
}

func CloneStepEvaluation(input *StepEvaluation) *StepEvaluation {
	if input == nil {
		return nil
	}

	output := *input
	output.ExpectLogs = append([]string{}, input.ExpectLogs...)
	output.FailOnLogs = append([]string{}, input.FailOnLogs...)
	if input.ExpectExit != nil {
		value := *input.ExpectExit
		output.ExpectExit = &value
	}
	return &output
}
