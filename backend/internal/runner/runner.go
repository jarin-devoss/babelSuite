package runner

import (
	"context"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

type StepNode struct {
	ID          string
	Name        string
	Kind        string
	Variant     string
	Image       string
	Message     string
	File        string
	Commands      []string
	FileContent   string
	Devices       []string
	ResourceClass string
	DependsOn     []string
}

type ArtifactExport struct {
	Path   string
	Name   string
	On     string
	Format string
}

type StepSpec struct {
	ExecutionID      string
	SuiteID          string
	SuiteTitle       string
	SuiteRepository  string
	Profile          string
	RuntimeProfile   string
	Env              map[string]string
	Headers          map[string]string
	Trigger          string
	BackendID        string
	BackendLabel     string
	BackendKind      string
	SourceSuiteID    string
	SourceSuiteTitle string
	SourceRepository string
	SourceVersion    string
	ResolvedRef      string
	Digest           string
	DependencyAlias  string
	StepIndex        int
	TotalSteps       int
	HealthySteps     int
	LeaseTTL         time.Duration
	Load             *suites.LoadSpec
	Security         *suites.SecuritySpec
	Evaluation       *suites.StepEvaluation
	OnFailure        []string
	ArtifactExports  []ArtifactExport
	// OnArtifact is called once per collected artifact file, with its container path and raw bytes.
	// May be nil when the backend does not support container-level file collection.
	OnArtifact func(path string, content []byte)
	Node             StepNode
	// GatewayURL is the primary APISIX sidecar address (first mock node).
	GatewayURL string
	// GatewayURLs contains one address per mock node in topology order.
	GatewayURLs []string
}

// Executor is the minimal interface required to run a single step.
type Executor interface {
	Run(ctx context.Context, step StepSpec, emit func(logstream.Line)) error
}

// Backend is the contract every execution backend must satisfy.
// Local (Docker), Kubernetes, and Remote all implement this interface,
// enabling the execution layer to select and invoke them uniformly without
// depending on concrete types.
type Backend interface {
	Executor
	// ID returns the unique identifier for this backend instance.
	ID() string
	// Label returns a human-readable display name shown in the UI and logs.
	Label() string
	// Kind returns the backend type string ("local", "kubernetes", "remote").
	Kind() string
	// IsAvailable reports whether the backend is reachable and ready to accept work.
	IsAvailable(ctx context.Context) bool
}

// Compile-time assertions that all backend implementations satisfy the Backend interface.
var (
	_ Backend = (*Local)(nil)
	_ Backend = (*Kubernetes)(nil)
	_ Backend = (*Remote)(nil)
)

type BackendConfig struct {
	ID         string
	Label      string
	Kind       string
	Permissive bool
}
