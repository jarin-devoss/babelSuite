package suites

import (
	"strings"
	"testing"
)

func resolvePluginSuite(t *testing.T, body string) ([]TopologyNode, error) {
	t.Helper()
	suite := Definition{
		ID:    "plugin-suite",
		Title: "Plugin Suite",
		SuiteStar: `load("@babelsuite/runtime", "mock", "plugin")
ref = plugin("my-checker")
gateway = mock.api(name="gateway")
` + body,
	}
	return ResolveTopology(suite, []Definition{suite})
}

func findNode(t *testing.T, nodes []TopologyNode, name string) TopologyNode {
	t.Helper()
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("no node %q in %+v", name, nodes)
	return TopologyNode{}
}

func TestPluginCallStepOptionsAreTheEnginesNotThePlugins(t *testing.T) {
	t.Parallel()
	nodes, err := resolvePluginSuite(t, `
cleanup = ref.check(name="cleanup", after=[gateway])
check = ref.check(
    name="check",
    after=[gateway],
    on_failure=[cleanup],
    continue_on_failure=True,
    expect_logs=["checked"],
    fail_on_logs="FATAL",
    target="http://svc:8080",
    max_lag=5,
)`)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	node := findNode(t, nodes, "check")
	if !node.ContinueOnFailure {
		t.Fatal("continue_on_failure=True must set the node's flag")
	}
	if len(node.OnFailure) != 1 || node.OnFailure[0] != "cleanup" {
		t.Fatalf("on_failure must name the node, got %v", node.OnFailure)
	}
	if node.Evaluation == nil || len(node.Evaluation.ExpectLogs) != 1 || node.Evaluation.ExpectLogs[0] != "checked" ||
		len(node.Evaluation.FailOnLogs) != 1 || node.Evaluation.FailOnLogs[0] != "FATAL" {
		t.Fatalf("expect_logs and fail_on_logs must reach the evaluation, got %+v", node.Evaluation)
	}
	if node.Plugin == nil {
		t.Fatal("expected a plugin spec")
	}
	config, _ := node.Plugin.Config.(map[string]any)
	for _, key := range []string{"continue_on_failure", "on_failure", "expect_logs", "fail_on_logs", "after", "name"} {
		if _, leaked := config[key]; leaked {
			t.Fatalf("step option %q must not be sent to the plugin as config: %v", key, config)
		}
	}
	if config["target"] != "http://svc:8080" || config["max_lag"] == nil {
		t.Fatalf("the plugin's own settings must stay in its config, got %v", config)
	}
}

func TestPluginOperationNameNeverMakesAStepSoftFail(t *testing.T) {
	t.Parallel()
	nodes, err := resolvePluginSuite(t, `
a = ref.monitor(name="a", after=[gateway])
b = ref.watch(name="b", after=[gateway])
c = ref.watch_lag(name="c", after=[gateway])
d = ref.monitor(name="d", after=[gateway], continue_on_failure=True)`)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if findNode(t, nodes, name).ContinueOnFailure {
			t.Fatalf("%s: an operation name must not decide whether a failure stops the run", name)
		}
	}
	if !findNode(t, nodes, "d").ContinueOnFailure {
		t.Fatal("an explicit continue_on_failure=True must be honoured")
	}
}

func TestPluginCallRefusesExpectExit(t *testing.T) {
	t.Parallel()
	_, err := resolvePluginSuite(t, `check = ref.check(name="check", after=[gateway], expect_exit=0)`)
	if err == nil || !strings.Contains(err.Error(), "expect_exit does not apply to a plugin step") {
		t.Fatalf("expected expect_exit to be refused, got %v", err)
	}
}

func TestPluginCallOptionTypesAreChecked(t *testing.T) {
	t.Parallel()
	_, err := resolvePluginSuite(t, `check = ref.check(name="check", after=[gateway], continue_on_failure="yes")`)
	if err == nil || !strings.Contains(err.Error(), "continue_on_failure must be a bool") {
		t.Fatalf("expected a type error, got %v", err)
	}
}
