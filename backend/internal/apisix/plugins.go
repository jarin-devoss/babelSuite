package apisix

import (
	_ "embed"
	"strings"
)

// The BabelSuite plugins that run inside the sidecar. TrafficCannonPluginName and
// AttackScannerPluginName are defined with their sources (cannon.go, scanner.go).
const (
	// MockPluginName answers a suite's HTTP-family mock operations from the mock
	// engine, and checks its gRPC calls.
	MockPluginName = "babelsuite-mock"
	// TCPMockPluginName and UDPMockPluginName are stream plugins that answer a
	// suite's TCP and UDP mocks.
	TCPMockPluginName = "babelsuite-tcp-mock"
	UDPMockPluginName = "babelsuite-udp-mock"
	// AsyncMockPluginName publishes a suite's async mock messages to its broker.
	AsyncMockPluginName = "babelsuite-async-mock"
)

// AsyncPublishRoute is the URI the control plane POSTs to, to have the sidecar
// publish an async mock's message.
const AsyncPublishRoute = "/_babelsuite/async/publish"

// ReservedPluginPrefix starts the name of every plugin BabelSuite ships. A user
// plugin may not use it, so it can never shadow one of them.
const ReservedPluginPrefix = "babelsuite-"

// IsReservedPluginName reports whether a plugin name belongs to BabelSuite.
func IsReservedPluginName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), ReservedPluginPrefix)
}

//go:embed lua/babelsuite-mock.lua
var mockLua string

//go:embed lua/babelsuite-async-mock.lua
var asyncMockLua string

//go:embed lua/babelsuite-tcp-mock.lua
var tcpMockLua string

//go:embed lua/babelsuite-udp-mock.lua
var udpMockLua string

// StreamLuaMountPath is the container path of the stream plugins, which APISIX
// loads from a directory of their own.
const StreamLuaMountPath = "/usr/local/openresty/site/lualib/apisix/stream/plugins"

// StreamPluginDir is the directory, under the one the HTTP plugins are written
// to, that holds the stream plugins. It is mounted at StreamLuaMountPath.
const StreamPluginDir = "stream"

// StreamLuaPluginFiles returns the stream plugin files the sidecar mounts, by
// file name.
func StreamLuaPluginFiles() map[string]string {
	return map[string]string{
		TCPMockPluginName + ".lua": tcpMockLua,
		UDPMockPluginName + ".lua": udpMockLua,
	}
}
