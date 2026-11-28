package suites

import "github.com/babelsuite/babelsuite/internal/suites/model"

// The data types of a suite live in suites/model, a package that imports nothing of
// this one, so the packages that evaluate, resolve and load suites can share them
// without importing each other. They are re-exported here under their old names.

type (
	APIOperation          = model.APIOperation
	APISurface            = model.APISurface
	ArtifactExport        = model.ArtifactExport
	CanaryValidatorSpec   = model.CanaryValidatorSpec
	ConsumerLagSpec       = model.ConsumerLagSpec
	ControlSimSpec        = model.ControlSimSpec
	DLQInspectorSpec      = model.DLQInspectorSpec
	Definition            = model.Definition
	ExchangeExample       = model.ExchangeExample
	FolderEntry           = model.FolderEntry
	Header                = model.Header
	ImportedSuite         = model.ImportedSuite
	LoadRequest           = model.LoadRequest
	LoadSpec              = model.LoadSpec
	LoadStage             = model.LoadStage
	LoadTask              = model.LoadTask
	LoadThreshold         = model.LoadThreshold
	LoadUser              = model.LoadUser
	LoadWait              = model.LoadWait
	MatchCondition        = model.MatchCondition
	MockFallback          = model.MockFallback
	MockOperationMetadata = model.MockOperationMetadata
	MockState             = model.MockState
	MockStateTransition   = model.MockStateTransition
	PIIScannerSpec        = model.PIIScannerSpec
	ParameterConstraint   = model.ParameterConstraint
	PluginSpec            = model.PluginSpec
	ProfileOption         = model.ProfileOption
	RegisterRequest       = model.RegisterRequest
	ResolvedDependency    = model.ResolvedDependency
	SchemaCompatSpec      = model.SchemaCompatSpec
	SecuritySpec          = model.SecuritySpec
	ShadowDiffSpec        = model.ShadowDiffSpec
	SourceFile            = model.SourceFile
	SpiceSimSpec          = model.SpiceSimSpec
	StepEvaluation        = model.StepEvaluation
	TopologyNode          = model.TopologyNode
	VerilogSimSpec        = model.VerilogSimSpec
)

const (
	MaxSecurityFloodDuration = model.MaxSecurityFloodDuration
	MaxSecurityFloodRate     = model.MaxSecurityFloodRate
	NodeKindLog              = model.NodeKindLog
	NodeKindMock             = model.NodeKindMock
	NodeKindPlugin           = model.NodeKindPlugin
	NodeKindSecurity         = model.NodeKindSecurity
	NodeKindService          = model.NodeKindService
	NodeKindSuite            = model.NodeKindSuite
	NodeKindTask             = model.NodeKindTask
	NodeKindTest             = model.NodeKindTest
	NodeKindTraffic          = model.NodeKindTraffic
	VariantMockAPI           = model.VariantMockAPI
	VariantServiceRun        = model.VariantServiceRun
)

var (
	ErrAlreadyExists     = model.ErrAlreadyExists
	ErrMissingDependency = model.ErrMissingDependency
	ErrNotFound          = model.ErrNotFound
	ErrTopologyCycle     = model.ErrTopologyCycle
	ErrUnsupportedCall   = model.ErrUnsupportedCall
	SecurityFailLevels   = model.SecurityFailLevels
	SecurityTechniques   = model.SecurityTechniques
)

func cloneLoadSpec(input *LoadSpec) *LoadSpec { return model.CloneLoadSpec(input) }

// Helpers that live in suites/model, under the names the rest of this package uses.
var (
	cloneDefinition           = model.CloneDefinition
	cloneProfiles             = model.CloneProfiles
	cloneFolders              = model.CloneFolders
	cloneSourceFiles          = model.CloneSourceFiles
	cloneTopology             = model.CloneTopology
	cloneResolvedDependencies = model.CloneResolvedDependencies
	cloneSurfaces             = model.CloneSurfaces
	cloneHeaders              = model.CloneHeaders
	cloneMockMetadata         = model.CloneMockMetadata
	cloneParameterConstraints = model.CloneParameterConstraints
	cloneMockFallback         = model.CloneMockFallback
	cloneMockState            = model.CloneMockState
	cloneStringMap            = model.CloneStringMap
	cloneIntMap               = model.CloneIntMap
	cloneImportedSuites       = model.CloneImportedSuites
	cloneStepEvaluation       = model.CloneStepEvaluation

	explicitSuiteSourceContent = model.ExplicitSourceContent
	normalizeSourcePath        = model.NormalizeSourcePath
)

// CloneDefinition deep-copies a definition for handing to a caller outside the package.
func CloneDefinition(input Definition) Definition { return model.CloneDefinition(input) }

// ValidateDefinition checks a definition a caller registers.
func ValidateDefinition(suite Definition) error { return model.ValidateDefinition(suite) }

// PreHookFile is the file of a suite package that gates the run on its environment.
const PreHookFile = model.PreHookFile

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
