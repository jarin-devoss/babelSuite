package suites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/pluginschema"
)

// The example plugins are registered with a Lua file, a CUE schema and a
// Starlark wrapper that must agree: the wrapper builds the config, the schema
// judges it, and the runner now enforces that before a step runs. These tests
// read the shipped files and keep the three from drifting apart.

const examplePluginsDir = "../../../examples/oci-plugins"

var defRe = regexp.MustCompile(`(?m)^def ([a-z][a-z0-9_]*)\(([^)]*)\):`)

// wrapperOps returns the public functions of a plugin.star with the
// parameters that have no default.
func wrapperOps(t *testing.T, star string) map[string][]string {
	t.Helper()
	ops := map[string][]string{}
	for _, match := range defRe.FindAllStringSubmatch(star, -1) {
		var required []string
		for _, param := range splitWrapperParams(match[2]) {
			param = strings.TrimSpace(param)
			if param == "" || strings.Contains(param, "=") || strings.HasPrefix(param, "*") {
				continue
			}
			required = append(required, param)
		}
		ops[match[1]] = required
	}
	return ops
}

// splitWrapperParams splits a parameter list on the commas that are not inside a
// list, a dict, a call or a string, so a default such as ["a", "b"] stays whole.
func splitWrapperParams(text string) []string {
	var parts []string
	depth, start := 0, 0
	var quote rune
	for i, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '[' || r == '(' || r == '{':
			depth++
		case r == ']' || r == ')' || r == '}':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

// manifestOps reads `operations: [a, b]` from a plugin.yaml.
func manifestOps(t *testing.T, manifest string) []string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^operations:\s*\[([^\]]*)\]`).FindStringSubmatch(manifest)
	if match == nil {
		t.Fatalf("plugin.yaml has no operations list:\n%s", manifest)
	}
	var ops []string
	for _, op := range strings.Split(match[1], ",") {
		if op = strings.TrimSpace(op); op != "" {
			ops = append(ops, op)
		}
	}
	sort.Strings(ops)
	return ops
}

// requiredValue gives a value for a parameter a wrapper requires. A parameter
// that is not listed here is a new one: add it, so the test knows what a valid
// call looks like.
func requiredValue(t *testing.T, plugin, param string) string {
	t.Helper()
	values := map[string]string{
		"name": `"n"`, "target": `"http://svc"`, "targets": `["http://svc"]`,
		"group": `"g"`, "topic": `"t"`, "topics": `["t"]`, "paths": `["/a"]`,
		"primary": `"http://a"`, "shadow": `"http://b"`,
		"registry_url": `"http://registry"`, "subject": `"s"`, "new_schema": `"{}"`,
		"auth_url": `"http://auth"`, "username": `"u"`, "password": `"p"`,
		"endpoint": `"http://svc/x"`, "endpoints": `["http://svc/x"]`,
		"netlist": `"RC"`, "top_module": `"top"`, "verilog": `"module top; endmodule"`,
		"testbench": `"module tb; endmodule"`,
		"numerator": `[1]`, "denominator": `[1, 2, 1]`,
		"model_url": `"http://model"`, "judge_url": `"http://model"`,
		"skills": `[{"name": "s", "description": "d"}]`,
		"cases":  `[{"prompt": "p", "expect": "s"}]`,
		"text":   `"some text"`, "criteria": `["is polite"]`, "candidates": `["a", "b"]`,
		"expected_ratio": `0.1`, "output_a": `"a"`, "output_b": `"b"`,
	}
	value, ok := values[param]
	if !ok {
		t.Fatalf("%s: the wrapper requires %q and this test does not know what to pass for it; add it to requiredValue", plugin, param)
	}
	return value
}

func examplePluginNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(examplePluginsDir)
	if err != nil {
		t.Fatalf("read %s: %v", examplePluginsDir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func readExample(t *testing.T, plugin, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(examplePluginsDir, plugin, file))
	if err != nil {
		t.Fatalf("read %s/%s: %v", plugin, file, err)
	}
	return string(data)
}

func TestExamplePluginsEveryOperationHasAWrapperAndEveryWrapperAnOperation(t *testing.T) {
	t.Parallel()
	for _, plugin := range examplePluginNames(t) {
		ops := manifestOps(t, readExample(t, plugin, "plugin.yaml"))
		wrappers := wrapperOps(t, readExample(t, plugin, "plugin.star"))
		for _, op := range ops {
			if _, ok := wrappers[op]; !ok {
				t.Errorf("%s: operation %q is in plugin.yaml but plugin.star defines no function for it", plugin, op)
			}
		}
		for wrapper := range wrappers {
			found := false
			for _, op := range ops {
				found = found || op == wrapper
			}
			if !found {
				t.Errorf("%s: plugin.star defines %q, which plugin.yaml does not list as an operation", plugin, wrapper)
			}
		}
	}
}

func TestExamplePluginsSchemasAreValidCUE(t *testing.T) {
	t.Parallel()
	for _, plugin := range examplePluginNames(t) {
		if err := pluginschema.Check(readExample(t, plugin, "schema.cue")); err != nil {
			t.Errorf("%s: %v", plugin, err)
		}
	}
}

func TestExamplePluginsSchemaHeadersDescribeTheCurrentCallForm(t *testing.T) {
	t.Parallel()
	for _, plugin := range examplePluginNames(t) {
		schema := readExample(t, plugin, "schema.cue")
		if strings.Contains(schema, "plugin.run(") {
			t.Errorf("%s: schema.cue still describes the old plugin.run(plugin=...) call", plugin)
		}
	}
}

// TestExamplePluginsDefaultsSatisfyTheirSchema calls every operation of every
// example plugin with only the arguments its wrapper requires, and checks the
// config the wrapper builds against the plugin's schema. A wrapper whose
// default is something the schema refuses would fail every suite that omits the
// argument.
func TestExamplePluginsDefaultsSatisfyTheirSchema(t *testing.T) {
	t.Parallel()
	for _, plugin := range examplePluginNames(t) {
		star := readExample(t, plugin, "plugin.star")
		schema := readExample(t, plugin, "schema.cue")
		wrappers := wrapperOps(t, star)

		var ops []string
		for op := range wrappers {
			ops = append(ops, op)
		}
		sort.Strings(ops)

		for _, op := range ops {
			var args []string
			for _, param := range wrappers[op] {
				args = append(args, fmt.Sprintf("%s=%s", param, requiredValue(t, plugin, param)))
			}
			if !strings.Contains(strings.Join(args, ","), "name=") {
				args = append([]string{`name="n"`}, args...)
			}
			suiteStar := fmt.Sprintf("load(\"@plugins/%s\", %q)\nnode = %s(%s)\n", plugin, op, op, strings.Join(args, ", "))

			resolver := func(name string) (map[string]string, error) {
				if name == plugin {
					return map[string]string{"plugin.star": star}, nil
				}
				return nil, nil
			}
			nodes, err := evalStarlarkTopology(suiteStar, resolver)
			if err != nil {
				t.Errorf("%s.%s: the wrapper does not accept its required arguments: %v", plugin, op, err)
				continue
			}
			if len(nodes) != 1 {
				t.Errorf("%s.%s: expected one node, got %d", plugin, op, len(nodes))
				continue
			}
			raw := nodes[0].PluginConfig
			config := map[string]any{}
			for key, value := range raw {
				// The runner drops unset values before validating, so a wrapper's
				// empty default for an optional setting is not a value.
				if value == nil || value == "" {
					continue
				}
				config[key] = value
			}
			// Settings a profile supplies are not the wrapper's to fill in.
			if err := pluginschema.Validate(schema, withProfileSettings(config)); err != nil {
				t.Errorf("%s.%s: the config the wrapper builds fails the plugin's schema: %v\nconfig: %v", plugin, op, err, config)
			}
		}
	}
}

// withProfileSettings adds what a profile's env normally gives a plugin step
// when a wrapper leaves it to the profile: the URL of the service the plugin
// talks to. These are required by the schema and given by the wrapper's own
// empty default, which the runner reads as unset.
func withProfileSettings(config map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range config {
		out[k] = v
	}
	for _, key := range []string{"sim_url", "kafka_rest_url", "registry_url"} {
		if _, ok := out[key]; !ok {
			out[key] = "http://profile-supplied"
		}
	}
	return out
}

// TestExamplePluginWrappersForwardTheEnginesStepOptions calls every operation
// with continue_on_failure=True, on_failure and expect_logs: the engine must
// receive them as step options, and the plugin must not.
func TestExamplePluginWrappersForwardTheEnginesStepOptions(t *testing.T) {
	t.Parallel()
	for _, plugin := range examplePluginNames(t) {
		star := readExample(t, plugin, "plugin.star")
		wrappers := wrapperOps(t, star)
		for op, required := range wrappers {
			var args []string
			for _, param := range required {
				args = append(args, fmt.Sprintf("%s=%s", param, requiredValue(t, plugin, param)))
			}
			if !strings.Contains(strings.Join(args, ","), "name=") {
				args = append([]string{`name="n"`}, args...)
			}
			args = append(args, `continue_on_failure=True`, `expect_logs=["checked"]`, `fail_on_logs="FATAL"`)
			suiteStar := fmt.Sprintf("load(\"@plugins/%s\", %q)\nnode = %s(%s)\n", plugin, op, op, strings.Join(args, ", "))

			resolver := func(name string) (map[string]string, error) {
				if name == plugin {
					return map[string]string{"plugin.star": star}, nil
				}
				return nil, nil
			}
			nodes, err := evalStarlarkTopology(suiteStar, resolver)
			if err != nil {
				t.Errorf("%s.%s: the wrapper does not forward step options: %v", plugin, op, err)
				continue
			}
			node := nodes[0]
			if !node.ContinueOnFailure {
				t.Errorf("%s.%s: continue_on_failure=True did not reach the node", plugin, op)
			}
			if node.Evaluation == nil || len(node.Evaluation.ExpectLogs) != 1 || len(node.Evaluation.FailOnLogs) != 1 {
				t.Errorf("%s.%s: expect_logs and fail_on_logs did not reach the node: %+v", plugin, op, node.Evaluation)
			}
			for _, leaked := range []string{"continue_on_failure", "expect_logs", "fail_on_logs"} {
				if _, ok := node.PluginConfig[leaked]; ok {
					t.Errorf("%s.%s: %s was sent to the plugin as config", plugin, op, leaked)
				}
			}
		}
	}
}
