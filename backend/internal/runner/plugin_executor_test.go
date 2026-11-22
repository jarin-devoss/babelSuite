package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
)

const checkerSchema = `
target:   string
max_lag:  int & >=0 | *1000
severity: "warn" | "critical" | *"critical"
`

// fakeSidecar answers the plugin's trigger route with a canned status and body,
// and counts the requests it received.
type fakeSidecar struct {
	*httptest.Server
	calls atomic.Int32
	body  atomic.Value
	code  atomic.Int32
}

func newFakeSidecar(t *testing.T, code int, body string) *fakeSidecar {
	t.Helper()
	f := &fakeSidecar{}
	f.code.Store(int32(code))
	f.body.Store(body)
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(f.code.Load()))
		_, _ = w.Write([]byte(f.body.Load().(string)))
	}))
	t.Cleanup(f.Close)
	return f
}

type capturedLines struct {
	mu    sync.Mutex
	lines []logstream.Line
}

func (c *capturedLines) emit(entry logstream.Line) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, entry)
}

func (c *capturedLines) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.lines))
	for _, entry := range c.lines {
		out = append(out, entry.Level+": "+entry.Text)
	}
	return out
}

func (c *capturedLines) has(level, fragment string) bool {
	for _, text := range c.texts() {
		if strings.HasPrefix(text, level+":") && strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func pluginStep(sidecar *fakeSidecar, config map[string]any, env map[string]string) (StepSpec, *platform.CustomPlugin) {
	plugin := &platform.CustomPlugin{Name: "checker", Trigger: "/_babelsuite/plugins/checker/start", Schema: checkerSchema}
	step := StepSpec{
		GatewayURL:        sidecar.URL,
		Env:               env,
		Node:              StepNode{ID: "check", Name: "check", Kind: "plugin"},
		Plugin:            &suites.PluginSpec{Name: "checker", Op: "check", Config: config},
		RegisteredPlugins: []platform.CustomPlugin{*plugin},
	}
	return step, plugin
}

func TestPluginReplyWithoutPassedFails(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{}`, `{"findings": []}`, `{"passed": null}`} {
		sidecar := newFakeSidecar(t, 200, body)
		step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
		err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin)
		if err == nil || !strings.Contains(err.Error(), `no boolean "passed" field`) {
			t.Fatalf("body %s: expected a named failure, got %v", body, err)
		}
	}
}

func TestPluginReplyWithANonBooleanPassedFails(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": "true", "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	if err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin); err == nil {
		t.Fatal("a string where a boolean belongs must fail the step")
	}
}

func TestPluginErrorReplyShowsWhatThePluginSaid(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 502, `{"error": "GET http://rest:8082/v3/clusters returned 404"}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin)
	if err == nil || !strings.Contains(err.Error(), "returned 404") || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("expected the plugin's message and the status, got %v", err)
	}
}

func TestPluginErrorReplyThatIsNotJSONIsShownAsItIs(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 500, "boom")
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the raw body, got %v", err)
	}
}

func TestPluginFailsWhenPassedIsFalse(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": false, "findings": [{"label": "lag", "severity": "critical", "detail": "lag=99"}]}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	log := &capturedLines{}
	err := executePlugin(context.Background(), step, log.emit, plugin)
	if err == nil || !strings.Contains(err.Error(), "exceeded thresholds") {
		t.Fatalf("expected a threshold failure, got %v", err)
	}
	if !log.has("error", "severity=critical") {
		t.Fatalf("a critical finding must be logged at error level: %v", log.texts())
	}
}

func TestPluginWarnFindingsPassAndShowAtWarnLevel(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": [{"label": "lag", "severity": "warn", "detail": "lag=99"}]}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	log := &capturedLines{}
	if err := executePlugin(context.Background(), step, log.emit, plugin); err != nil {
		t.Fatalf("a warn-only reply with passed=true must pass, got %v", err)
	}
	if !log.has("warn", "severity=warn") {
		t.Fatalf("the warn finding must stay visible: %v", log.texts())
	}
}

func TestPluginPassedDecidesTheStepEvenWithCriticalFindings(t *testing.T) {
	t.Parallel()
	// A plugin that grades against a threshold passes while some case missed.
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": [{"label": "a", "severity": "critical", "detail": "case 2 missed"}]}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	log := &capturedLines{}
	if err := executePlugin(context.Background(), step, log.emit, plugin); err != nil {
		t.Fatalf("passed decides the step: %v", err)
	}
	if !log.has("error", "severity=critical") {
		t.Fatalf("the critical finding must still be shown: %v", log.texts())
	}
	for _, text := range log.texts() {
		if strings.Contains(text, "contract") || strings.Contains(text, "passed decides") {
			t.Fatalf("a verdict the plugin chose is not a contract violation, got %q", text)
		}
	}
}

func TestPluginSeverityAliasesAndUnknownValues(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		level string
		known bool
	}{
		"critical": {"error", true}, "high": {"error", true}, "error": {"error", true}, "CRITICAL": {"error", true},
		"warn": {"warn", true}, "warning": {"warn", true}, "info": {"info", true},
		"bogus": {"warn", false}, "": {"warn", false},
	}
	for severity, want := range cases {
		level, known := pluginFindingLevel(severity)
		if level != want.level || known != want.known {
			t.Fatalf("%q: got (%s, %v), want (%s, %v)", severity, level, known, want.level, want.known)
		}
	}

	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": [{"label": "a", "severity": "dubious", "detail": "x"}]}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)
	log := &capturedLines{}
	if err := executePlugin(context.Background(), step, log.emit, plugin); err != nil {
		t.Fatal(err)
	}
	if !log.has("warn", `severity "dubious", which is not one of info, warn or critical`) {
		t.Fatalf("an unknown severity must be named once: %v", log.texts())
	}
}

func TestPluginConfigThatBreaksTheSchemaFailsBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x", "max_lag": -1}, nil)
	err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin)
	if err == nil || !strings.Contains(err.Error(), "plugin checker config") {
		t.Fatalf("expected a config error, got %v", err)
	}
	if sidecar.calls.Load() != 0 {
		t.Fatalf("an invalid config must not reach the sidecar, saw %d request(s)", sidecar.calls.Load())
	}
}

func TestPluginConfigMissingARequiredSettingFails(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	// A wrapper's empty default for a setting nobody gave is "unset", not a value.
	step, plugin := pluginStep(sidecar, map[string]any{"target": ""}, nil)
	err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin)
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("expected the missing setting to be named, got %v", err)
	}
	if sidecar.calls.Load() != 0 {
		t.Fatal("a config missing a required setting must not reach the sidecar")
	}
}

func TestPluginConfigSchemaDefaultsApplyToUnsetValues(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x", "severity": "", "max_lag": nil}, nil)
	if err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin); err != nil {
		t.Fatalf("unset optional settings must take the schema's defaults, got %v", err)
	}
}

func TestPluginConfigFromTheProfileEnvironmentIsReadAsTheTypeItSpells(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x"}, map[string]string{"max_lag": "500"})
	if err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin); err != nil {
		t.Fatalf("a profile that sets max_lag to \"500\" must satisfy an int field, got %v", err)
	}
}

func TestPluginConfigTextFromTheSuiteIsNotCoerced(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"target": "http://x", "max_lag": "500"}, nil)
	if err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin); err == nil {
		t.Fatal("a string the suite wrote where an int belongs is a mistake and must fail")
	}
}

func TestPluginWithoutASchemaIsNotValidated(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": []}`)
	step, plugin := pluginStep(sidecar, map[string]any{"anything": -1}, nil)
	plugin.Schema = ""
	if err := executePlugin(context.Background(), step, (&capturedLines{}).emit, plugin); err != nil {
		t.Fatalf("a plugin with no schema accepts any config, got %v", err)
	}
}

func TestPluginStepChecksItsLogExpectations(t *testing.T) {
	t.Parallel()
	sidecar := newFakeSidecar(t, 200, `{"passed": true, "findings": [], "summary": "all good"}`)
	step, _ := pluginStep(sidecar, map[string]any{"target": "http://x"}, nil)

	step.Evaluation = &suites.StepEvaluation{ExpectLogs: []string{"never printed"}}
	log := &capturedLines{}
	handled, err := runInProcessStep(context.Background(), step, log.emit, log.texts)
	if !handled || err == nil || !strings.Contains(err.Error(), "expected logs containing") {
		t.Fatalf("expect_logs must apply to a plugin step, got handled=%v err=%v", handled, err)
	}

	step.Evaluation = &suites.StepEvaluation{ExpectLogs: []string{"all good"}, FailOnLogs: []string{"FATAL"}}
	log = &capturedLines{}
	if _, err := runInProcessStep(context.Background(), step, log.emit, log.texts); err != nil {
		t.Fatalf("satisfied expectations must pass, got %v", err)
	}
}
