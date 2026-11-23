package apisix

import "regexp"

// DefaultSidecarImage is the APISIX image a sidecar runs when settings name
// none. It is the version the sidecar is built and tested against: the
// standalone configuration, the writable config.yaml the 3.9 entrypoint
// rewrites, and the HTTP/2 and stream settings all depend on it, and a floating
// tag would change them under a running installation.
const DefaultSidecarImage = "apache/apisix:3.9.0-debian"

// The sidecar resolves ${{NAME}} in its routes from its own environment, which
// the runner sets when it starts the container.
const (
	// engineAddrEnv is the control plane's HTTP mock engine.
	engineAddrEnv = "${{BABELSUITE_ENGINE_ADDR}}"
	// engineGRPCAddrEnv is the control plane's gRPC mock front end.
	engineGRPCAddrEnv = "${{BABELSUITE_ENGINE_GRPC_ADDR}}"
	// mockTokenEnv is the secret the control plane requires on every mock call.
	mockTokenEnv = "${{BABELSUITE_MOCK_TOKEN}}"
)

var pathParameterPattern = regexp.MustCompile(`\{[^/]+\}`)

type SuiteConfig struct {
	ID            string
	APISurfaces   []SurfaceConfig
	CustomPlugins []CustomPluginConfig
	// MockNames are the names of the suite's mock nodes. A service of the suite
	// reaches a mock by that name (http://stripe-mock:9080), so a route with a
	// host of its own also answers to them.
	MockNames []string
	// Harden opts the rendered gateway into platform-provided defenses
	// (baseline security response headers, a rate-limited synthetic probe
	// target) on top of the suite's real backend. It must stay opt-in: a
	// suite's security/load checks should observe the real backend by
	// default, not a gateway-injected stand-in, or a genuinely broken
	// backend could never fail them.
	Harden bool
}

// CustomPluginConfig carries the registration data for a user-defined Lua plugin.
type CustomPluginConfig struct {
	Name    string
	Trigger string
	Lua     string
}

type SurfaceConfig struct {
	ID         string
	Protocol   string
	MockHost   string
	Operations []OperationConfig
}

type OperationConfig struct {
	ID           string
	Method       string
	Name         string
	Summary      string
	ContractPath string
	MockURL      string
	MockMetadata OperationMetadataConfig
}

type OperationMetadataConfig struct {
	Adapter string
}

type routeDocument struct {
	Deployment   deploymentBlock    `yaml:"deployment"`
	Plugins      []pluginSpec       `yaml:"plugins,omitempty"`
	Routes       []routeBlock       `yaml:"routes,omitempty"`
	StreamRoutes []streamRouteBlock `yaml:"stream_routes,omitempty"`
	GlobalRules  []globalRuleBlock  `yaml:"global_rules,omitempty"`
}

type globalRuleBlock struct {
	ID      string         `yaml:"id"`
	Plugins map[string]any `yaml:"plugins,omitempty"`
}

type pluginSpec struct {
	Name   string `yaml:"name"`
	Stream bool   `yaml:"stream,omitempty"`
}

type deploymentBlock struct {
	Role          string              `yaml:"role"`
	RoleDataPlane deploymentDataPlane `yaml:"role_data_plane"`
}

type deploymentDataPlane struct {
	ConfigProvider string `yaml:"config_provider"`
}

type routeBlock struct {
	ID              string         `yaml:"id"`
	Name            string         `yaml:"name,omitempty"`
	Desc            string         `yaml:"desc,omitempty"`
	URI             string         `yaml:"uri"`
	Methods         []string       `yaml:"methods,omitempty"`
	Hosts           []string       `yaml:"hosts,omitempty"`
	EnableWebsocket bool           `yaml:"enable_websocket,omitempty"`
	Priority        int            `yaml:"priority,omitempty"`
	Vars            [][]any        `yaml:"vars,omitempty"`
	Plugins         map[string]any `yaml:"plugins,omitempty"`
	Upstream        upstreamBlock  `yaml:"upstream"`
}

type upstreamBlock struct {
	Type   string         `yaml:"type"`
	Scheme string         `yaml:"scheme,omitempty"`
	Nodes  map[string]int `yaml:"nodes"`
}

type streamRouteBlock struct {
	ID         string         `yaml:"id"`
	ServerPort int            `yaml:"server_port"`
	Plugins    map[string]any `yaml:"plugins,omitempty"`
	Upstream   upstreamBlock  `yaml:"upstream"`
}
