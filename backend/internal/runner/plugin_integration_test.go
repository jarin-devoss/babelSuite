//go:build integration

package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// These tests run the example plugins in examples/oci-plugins, as shipped, in a
// real APISIX sidecar, against stub servers on this machine and, for the Kafka
// and schema-registry plugins, against real containers. A step goes through the
// same executePlugin the control plane uses, so the CUE schema, the wrapper-free
// config, the reply contract and the verdict are all exercised together.
//
//	go test -tags integration ./internal/runner -run IntegrationPlugin -count=1
//
// They need Docker and the apache/apisix:3.9.0-debian image.

const (
	pluginSuite   = "it-plugins"
	pluginProfile = "local.yaml"
	pluginsRoot   = "../../../examples/oci-plugins"
)

var (
	pluginSidecarOnce sync.Once
	pluginSidecarURL  string
	pluginSidecarErr  error
	pluginSet         = map[string]platform.CustomPlugin{}
)

// examplePlugin reads one shipped plugin from disk, exactly as scripts/install-plugins.py
// registers it: its Lua, its CUE schema and the route derived from its name.
func examplePlugin(t *testing.T, name string) platform.CustomPlugin {
	t.Helper()
	dir := filepath.Join(pluginsRoot, name)
	lua, err := os.ReadFile(filepath.Join(dir, "plugin.lua"))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	schema, err := os.ReadFile(filepath.Join(dir, "schema.cue"))
	if err != nil {
		t.Fatalf("read %s schema: %v", name, err)
	}
	return platform.CustomPlugin{
		Name:    name,
		Trigger: "/_babelsuite/plugins/" + name + "/start",
		Lua:     string(lua),
		Schema:  string(schema),
	}
}

func allExamplePluginNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		t.Fatalf("read %s: %v", pluginsRoot, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// startPluginSidecar starts one sidecar with every example plugin loaded. A
// start takes several seconds, and nothing a test does to it needs a fresh one.
func startPluginSidecar(t *testing.T) string {
	t.Helper()
	pluginSidecarOnce.Do(func() {
		_ = removeContainer(SidecarContainerName(pluginSuite, pluginProfile))
		var custom []apisix.CustomPluginConfig
		for _, name := range allExamplePluginNames(t) {
			plugin := examplePlugin(t, name)
			pluginSet[name] = plugin
			custom = append(custom, apisix.CustomPluginConfig{Name: plugin.Name, Trigger: plugin.Trigger, Lua: plugin.Lua})
		}
		if err := EnsureSuiteSidecar(apisix.SuiteConfig{ID: pluginSuite, CustomPlugins: custom}, pluginProfile, integrationImage, ""); err != nil {
			pluginSidecarErr = fmt.Errorf("start the sidecar: %w", err)
			return
		}
		pluginSidecarURL = SuiteSidecarURL(pluginSuite, pluginProfile)
		if pluginSidecarURL == "" {
			pluginSidecarErr = fmt.Errorf("the sidecar is not published on a host port")
			return
		}
		deadline := time.Now().Add(45 * time.Second)
		for {
			// An unknown operation is answered with 400 by a loaded plugin, and with
			// 404 by a sidecar whose route is not there yet.
			resp, err := http.Post(pluginSidecarURL+"/_babelsuite/plugins/consumer-lag/start", "application/json", strings.NewReader(`{"op":"probe","config":{}}`))
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusBadRequest {
					return
				}
			}
			if time.Now().After(deadline) {
				pluginSidecarErr = fmt.Errorf("the plugins never answered: %v", err)
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	})
	if pluginSidecarErr != nil {
		t.Fatal(pluginSidecarErr)
	}
	return pluginSidecarURL
}

// pluginRun is the outcome of one plugin step.
type pluginRun struct {
	Err error
	Log *capturedLines
}

// Passed reports whether the step passed.
func (r pluginRun) Passed() bool { return r.Err == nil }

// Findings returns the finding lines the step logged.
func (r pluginRun) Findings() []string {
	var out []string
	for _, text := range r.Log.texts() {
		if strings.Contains(text, "Finding [") {
			out = append(out, text)
		}
	}
	return out
}

// HasFinding reports whether a finding with this label was logged, at the given
// log level when level is not empty.
func (r pluginRun) HasFinding(label, level string) bool {
	for _, text := range r.Findings() {
		if !strings.Contains(text, "Finding ["+label) {
			continue
		}
		if level == "" || strings.HasPrefix(text, level+":") {
			return true
		}
	}
	return false
}

// runPlugin runs one operation of an example plugin through executePlugin.
func runPlugin(t *testing.T, name, op string, config map[string]any) pluginRun {
	t.Helper()
	base := startPluginSidecar(t)
	plugin, ok := pluginSet[name]
	if !ok {
		t.Fatalf("plugin %q is not loaded", name)
	}
	log := &capturedLines{}
	step := StepSpec{
		GatewayURL:        base,
		Node:              StepNode{ID: "it-" + op, Name: "it-" + op, Kind: "plugin"},
		Plugin:            &suites.PluginSpec{Name: name, Op: op, Config: config},
		RegisteredPlugins: []platform.CustomPlugin{plugin},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	err := executePlugin(ctx, step, log.emit, &plugin)
	return pluginRun{Err: err, Log: log}
}

// postPlugin posts a raw payload to a plugin's route, for what executePlugin
// would never send (an operation the wrapper does not define).
func postPlugin(t *testing.T, name, body string) (int, string) {
	t.Helper()
	base := startPluginSidecar(t)
	resp, err := http.Post(base+"/_babelsuite/plugins/"+name+"/start", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, string(data)
}

// stub starts an HTTP server on this machine and returns the URL the sidecar's
// container reaches it at.
func stub(t *testing.T, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return fmt.Sprintf("http://host.docker.internal:%d", listener.Addr().(*net.TCPAddr).Port)
}

// deadURL is an address nothing listens on.
func deadURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

// jsonHandler answers every request with 200 and this JSON body.
func jsonHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

// dropConnection closes the connection without answering, as a service that
// resets a request does.
func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hijacker.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func removeContainer(name string) error {
	return exec.Command("docker", "rm", "-f", name).Run()
}
