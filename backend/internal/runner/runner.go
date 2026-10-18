package runner

import (
	"context"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/platform"
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
	Commands    []string
	FileContent string
	Devices     []string
	// MemoryBytes overrides the runner's default container memory ceiling for
	// this step. Zero keeps the default.
	MemoryBytes int64
	// MilliCPU caps this step's CPU in thousandths of a core. Zero is no
	// limit, the default on every backend.
	MilliCPU  int64
	DependsOn []string
}

type ArtifactExport struct {
	Path   string
	Name   string
	On     string
	Format string
}

type StepSpec struct {
	ExecutionID       string
	SuiteID           string
	SuiteTitle        string
	SuiteRepository   string
	Profile           string
	RuntimeProfile    string
	Env               map[string]string
	Headers           map[string]string
	Trigger           string
	BackendID         string
	BackendLabel      string
	BackendKind       string
	SourceSuiteID     string
	SourceSuiteTitle  string
	SourceRepository  string
	SourceVersion     string
	ResolvedRef       string
	Digest            string
	DependencyAlias   string
	StepIndex         int
	TotalSteps        int
	HealthySteps      int
	LeaseTTL          time.Duration
	Load              *suites.LoadSpec
	Security          *suites.SecuritySpec
	Plugin            *suites.PluginSpec
	RegisteredPlugins []platform.CustomPlugin
	Evaluation        *suites.StepEvaluation
	OnFailure         []string
	ArtifactExports   []ArtifactExport
	// OnArtifact is called once per collected artifact file, with its container path and raw bytes.
	// May be nil when the backend does not support container-level file collection.
	OnArtifact func(path string, content []byte)
	Node       StepNode
	// NetworkName is the Docker network all step containers in this execution share.
	// Empty means no dedicated network (default Docker bridge).
	NetworkName string
	// NetworkAliases are the hostnames this step answers to on the execution
	// network. Empty falls back to the step's own name.
	//
	// It is set by the caller because deciding the name needs the whole
	// topology: node names are scoped to the suite that declared them while the
	// network is flat, so two composed suites can each contribute a "db". The
	// caller resolves that to one publisher per name.
	NetworkAliases []string
	// GatewayURL is the primary APISIX sidecar address (first mock node).
	GatewayURL string
	// GatewayURLs contains one address per mock node in topology order.
	GatewayURLs []string
	// PublishPorts lists container ports (e.g. "8082/tcp") that must be bound
	// to a host port so the shared APISIX sidecar can reach this node's
	// service over host.docker.internal instead of the per-execution Docker
	// network. Populated when another node's plugin config references this
	// node by name — see pluginHostPortRefs in the execution package.
	PublishPorts []string
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

// ExecutionLifecycle is implemented by a backend that holds resources for a
// whole execution rather than for one step — on Kubernetes the DNS Service,
// the shared workspace volume, and service pods that outlive the step that
// started them. SetupExecution runs once before the first step,
// DestroyExecution once after the last, and DestroyExecution must be safe to
// call when setup failed or never ran.
type ExecutionLifecycle interface {
	SetupExecution(ctx context.Context, executionID string) error
	DestroyExecution(ctx context.Context, executionID string) error
}

// CapacityReporter reports how many steps a backend runs at once; a value
// <= 0 is no limit. The execution queue has no worker pool of its own: it
// starts a step when the backend that runs it has a free slot, so a slow step
// on one backend never holds back another.
type CapacityReporter interface {
	Capacity() int
}

// HostPortResolver reports the host port that reaches a step's container
// port, or "" when there is none. Plugin steps run in the suite's APISIX
// sidecar outside the execution, and dial sibling services through it.
type HostPortResolver interface {
	HostPort(executionID, nodeID, containerPort string) string
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
