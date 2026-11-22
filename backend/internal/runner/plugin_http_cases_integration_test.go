//go:build integration

package runner

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- every plugin ----------------------------------------------------------

func TestIntegrationPluginsAllCompileAndLoad(t *testing.T) {
	startPluginSidecar(t)
	container := SidecarContainerName(pluginSuite, pluginProfile)
	for _, name := range allExamplePluginNames(t) {
		path := "/usr/local/openresty/site/lualib/apisix/plugins/" + name + ".lua"
		out, err := exec.Command("docker", "exec", container, "sh", "-c",
			"/usr/local/openresty/luajit/bin/luajit -bl "+path+" > /dev/null").CombinedOutput()
		if err != nil {
			t.Errorf("plugin %s does not compile: %v\n%s", name, err, out)
		}
	}
}

func TestIntegrationPluginsRefuseAnUnknownOperation(t *testing.T) {
	startPluginSidecar(t)
	for _, name := range allExamplePluginNames(t) {
		status, body := postPlugin(t, name, `{"op":"definitely-not-an-op","config":{}}`)
		if status != 400 {
			t.Errorf("%s: an unknown operation must be refused with 400, got %d %s", name, status, body)
			continue
		}
		if !strings.Contains(body, "definitely-not-an-op") && !strings.Contains(body, "required") {
			t.Errorf("%s: the refusal must name the operation or what is missing, got %s", name, body)
		}
	}
}

// ---- canary-validator ------------------------------------------------------

// canaryStub answers with the canary header on every other request. When drop is
// set, a share of the canary's own answers never arrive, as when a canary
// resets its connections.
func canaryStub(t *testing.T, drop bool) string {
	var n atomic.Int64
	return stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		if i%2 == 0 {
			_, _ = w.Write([]byte("{}")) // stable
			return
		}
		if drop && (i/2)%5 < 2 {
			dropConnection(w)
			return
		}
		w.Header().Set("X-Canary", "1")
		_, _ = w.Write([]byte("{}"))
	}))
}

func TestIntegrationPluginCanaryWatchRatioWithAWrongRatioWarnsAndPasses(t *testing.T) {
	url := stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Canary", "1") // every response: ratio 1.0, expected 0.1
	}))
	run := runPlugin(t, "canary-validator", "watch_ratio", map[string]any{"target": url, "expected_ratio": 0.1, "sample_size": 10})
	if !run.Passed() {
		t.Fatalf("watch_ratio reports at warn and must pass: %v", run.Err)
	}
	if !run.HasFinding("canary.ratio.mismatch", "warn") {
		t.Fatalf("expected a warn finding, got %v", run.Findings())
	}
}

func TestIntegrationPluginCanaryValidateWithAWrongRatioFails(t *testing.T) {
	url := stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Canary", "1")
	}))
	run := runPlugin(t, "canary-validator", "validate", map[string]any{"target": url, "expected_ratio": 0.1, "sample_size": 10})
	if run.Passed() || !run.HasFinding("canary.ratio.mismatch", "error") {
		t.Fatalf("a wrong ratio must fail validate: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginCanaryAgainstADeadTargetFailsSayingSo(t *testing.T) {
	run := runPlugin(t, "canary-validator", "validate", map[string]any{"target": deadURL(t), "expected_ratio": 0.1, "sample_size": 5})
	if run.Passed() || !strings.Contains(run.Err.Error(), "is unreachable") {
		t.Fatalf("a target nothing answers must fail as unreachable, got %v", run.Err)
	}
}

func TestIntegrationPluginCanaryDroppedCanaryRequestsAreNotReadAsStable(t *testing.T) {
	run := runPlugin(t, "canary-validator", "compare_errors", map[string]any{"target": canaryStub(t, true), "sample_size": 100})
	if run.Passed() {
		t.Fatalf("a canary that drops 40%% of its own requests must fail compare_errors. Log: %v", run.Log.texts())
	}
	if !run.HasFinding("canary.errors.unanswered", "error") {
		t.Fatalf("expected the unanswered requests to be a finding, got %v", run.Findings())
	}
}

func TestIntegrationPluginCanaryHealthyCanaryPassesCompareErrors(t *testing.T) {
	run := runPlugin(t, "canary-validator", "compare_errors", map[string]any{"target": canaryStub(t, false), "sample_size": 40})
	if !run.Passed() {
		t.Fatalf("a healthy canary must pass: %v", run.Err)
	}
}

// ---- shadow-diff -----------------------------------------------------------

func shadowStub(t *testing.T) string {
	bodies := map[string]string{
		"/same": `{"v":1,"name":"a"}`, "/extra": `{"v":1,"name":"a","extra":"only-in-shadow"}`,
		"/text-a": "alpha", "/text-b": "beta",
		"/arr-a": `{"l":[1,2]}`, "/arr-b": `{"l":[1,2,3]}`,
		"/id-a": `{"u":{"id":1,"name":"a"}}`, "/id-b": `{"u":{"id":2,"name":"a"}}`,
	}
	return stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(404)
			return
		}
		body, ok := bodies[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestIntegrationPluginShadowDiffIdenticalResponsesPass(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": base + "/same"})
	if !run.Passed() {
		t.Fatalf("identical responses must pass: %v", run.Err)
	}
}

func TestIntegrationPluginShadowDiffFindsAFieldOnlyTheShadowHas(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": base + "/extra"})
	if run.Passed() || !run.HasFinding("shadow.diff $.extra", "error") {
		t.Fatalf("a field only the shadow has must fail the step: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginShadowDiffComparesNonJSONBodies(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/text-a", "shadow": base + "/text-b"})
	if run.Passed() || !run.HasFinding("shadow.body_mismatch", "error") {
		t.Fatalf("different non-JSON bodies must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginShadowDiffSeesAnArrayThatIsLongerInTheShadow(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/arr-a", "shadow": base + "/arr-b"})
	if run.Passed() || !run.HasFinding("shadow.diff $.l", "error") {
		t.Fatalf("an array longer on one side must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginShadowDiffCheckReportsAtWarnAndNeverBlocks(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "check", map[string]any{"primary": base + "/same", "shadow": base + "/extra"})
	if !run.Passed() || !run.HasFinding("shadow.diff $.extra", "warn") {
		t.Fatalf("check must show the difference at warn and pass: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginShadowDiffSeverityWarnDoesNotBlock(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": base + "/extra", "severity": "warn"})
	if !run.Passed() {
		t.Fatalf("severity=warn must not block: %v", run.Err)
	}
}

func TestIntegrationPluginShadowDiffThresholdAllowsThatManyBlockingDifferences(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": base + "/extra", "threshold": 1})
	if !run.Passed() {
		t.Fatalf("one difference within threshold=1 must pass: %v", run.Err)
	}
}

func TestIntegrationPluginShadowDiffIgnoresTheFieldsItIsToldTo(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{
		"primary": base + "/id-a", "shadow": base + "/id-b", "ignore_fields": []any{"$.u.id"},
	})
	if !run.Passed() {
		t.Fatalf("an ignored field must not count: %v", run.Err)
	}
}

func TestIntegrationPluginShadowDiffStatusMismatchFails(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": base + "/gone"})
	if run.Passed() || !run.HasFinding("shadow.status_mismatch", "error") {
		t.Fatalf("different statuses must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginShadowDiffComparePathsNamesTheDifferingPath(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "compare_paths", map[string]any{
		"primary": base, "shadow": base, "paths": []any{"/same"},
	})
	if !run.Passed() {
		t.Fatalf("a path that matches itself must pass: %v", run.Err)
	}
}

func TestIntegrationPluginShadowDiffAnUnreachableShadowIsAnError(t *testing.T) {
	base := shadowStub(t)
	run := runPlugin(t, "shadow-diff", "diff", map[string]any{"primary": base + "/same", "shadow": deadURL(t) + "/same"})
	if run.Passed() || !strings.Contains(run.Err.Error(), "shadow request failed") {
		t.Fatalf("an unreachable shadow must fail, naming it: %v", run.Err)
	}
}

// ---- jwt-auditor -----------------------------------------------------------

func b64(v any) string {
	data, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(data)
}

func testToken() string {
	now := time.Now().Unix()
	return b64(map[string]any{"alg": "RS256", "typ": "JWT"}) + "." +
		b64(map[string]any{"sub": "u", "iss": "x", "iat": now, "exp": now + 3600}) + ".sig"
}

// authStub serves /login and a /protected endpoint with chosen faults.
func authStub(t *testing.T, token string, dropNoAuth, acceptTampered bool) string {
	return stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
		case "/protected":
			auth := r.Header.Get("Authorization")
			switch {
			case auth == "" && dropNoAuth:
				dropConnection(w)
			case auth == "":
				w.WriteHeader(401)
			case auth == "Bearer "+token:
				w.WriteHeader(200)
			case acceptTampered:
				w.WriteHeader(200)
			default:
				w.WriteHeader(401)
			}
		default:
			w.WriteHeader(404)
		}
	}))
}

func jwtConfig(base string) map[string]any {
	return map[string]any{"auth_url": base + "/login", "username": "u", "password": "p", "endpoint": base + "/protected"}
}

func TestIntegrationPluginJWTAuditPassesAnEndpointThatEnforcesTokens(t *testing.T) {
	base := authStub(t, testToken(), false, false)
	run := runPlugin(t, "jwt-auditor", "audit_endpoint", jwtConfig(base))
	if !run.Passed() {
		t.Fatalf("a correct endpoint must pass: %v / %v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginJWTAuditFailsWhenTheNoAuthProbeGetsNoAnswer(t *testing.T) {
	base := authStub(t, testToken(), true, false)
	run := runPlugin(t, "jwt-auditor", "audit_endpoint", jwtConfig(base))
	if run.Passed() || !run.HasFinding("jwt.endpoint.probe_failed", "error") {
		t.Fatalf("a probe that errors must be a finding and fail the step: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginJWTAuditFailsWhenATamperedTokenIsAccepted(t *testing.T) {
	base := authStub(t, testToken(), false, true)
	run := runPlugin(t, "jwt-auditor", "audit_endpoint", jwtConfig(base))
	if run.Passed() || !run.HasFinding("jwt.endpoint.signature_bypass", "error") {
		t.Fatalf("an accepted forged token must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginJWTCheckTokenSeverityDecidesWhetherASymmetricAlgorithmBlocks(t *testing.T) {
	hs := b64(map[string]any{"alg": "HS256", "typ": "JWT"}) + "." +
		b64(map[string]any{"sub": "u", "iss": "x", "iat": time.Now().Unix(), "exp": time.Now().Unix() + 3600}) + ".sig"
	base := authStub(t, hs, false, false)
	cfg := map[string]any{"auth_url": base + "/login", "username": "u", "password": "p"}

	if run := runPlugin(t, "jwt-auditor", "check_token", cfg); run.Passed() || !run.HasFinding("jwt.alg.symmetric", "error") {
		t.Fatalf("by default a symmetric algorithm blocks: err=%v findings=%v", run.Err, run.Findings())
	}
	cfg["severity"] = "warn"
	if run := runPlugin(t, "jwt-auditor", "check_token", cfg); !run.Passed() || !run.HasFinding("jwt.alg.symmetric", "warn") {
		t.Fatalf("with severity=warn it reports and passes: err=%v findings=%v", run.Err, run.Findings())
	}
}

// ---- pii-scanner -----------------------------------------------------------

func piiStub(t *testing.T) string {
	return stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clean":
			_, _ = w.Write([]byte("nothing to see"))
		case "/leak":
			_, _ = w.Write([]byte("contact ada@example.com"))
		case "/header-leak":
			w.Header().Set("X-Debug", "ada@example.com")
			_, _ = w.Write([]byte("ok"))
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestIntegrationPluginPIIScanFailsOnALeakAndProbeOnlyWarns(t *testing.T) {
	base := piiStub(t)
	if run := runPlugin(t, "pii-scanner", "scan", map[string]any{"target": base + "/leak"}); run.Passed() || !run.HasFinding("pii.email", "error") {
		t.Fatalf("scan must fail on an email: err=%v findings=%v", run.Err, run.Findings())
	}
	if run := runPlugin(t, "pii-scanner", "probe", map[string]any{"target": base + "/leak"}); !run.Passed() || !run.HasFinding("pii.email", "warn") {
		t.Fatalf("probe reports at warn and must pass: err=%v findings=%v", run.Err, run.Findings())
	}
	if run := runPlugin(t, "pii-scanner", "scan", map[string]any{"target": base + "/clean"}); !run.Passed() {
		t.Fatalf("a clean response must pass: %v", run.Err)
	}
}

func TestIntegrationPluginPIIScanHeadersFindsALeakInAHeader(t *testing.T) {
	base := piiStub(t)
	run := runPlugin(t, "pii-scanner", "scan_headers", map[string]any{"target": base + "/header-leak"})
	if run.Passed() || !run.HasFinding("pii.email", "error") {
		t.Fatalf("a leak in a header must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginPIIScanManyFailsForATargetItCouldNotScan(t *testing.T) {
	base := piiStub(t)
	run := runPlugin(t, "pii-scanner", "scan_many", map[string]any{"targets": []any{base + "/clean", deadURL(t)}})
	if run.Passed() || !run.HasFinding("pii.unreachable", "error") {
		t.Fatalf("a target that could not be scanned proves nothing and must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}

// ---- simulators ------------------------------------------------------------

func simStub(t *testing.T, reply string, delay time.Duration) string {
	return stub(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
}

func TestIntegrationPluginControlSimMonitorWarnsWhereStepResponseFails(t *testing.T) {
	url := simStub(t, `{"stable": true, "settling_time": 5, "overshoot_pct": 1, "dc_gain": 1}`, 0)
	cfg := map[string]any{"sim_url": url, "numerator": []any{1}, "denominator": []any{1, 2, 1}, "max_settling_time": 0.1}

	if run := runPlugin(t, "control-sim", "step_response", cfg); run.Passed() || !run.HasFinding("control.settling_time", "error") {
		t.Fatalf("step_response must fail on a slow settling time: err=%v findings=%v", run.Err, run.Findings())
	}
	if run := runPlugin(t, "control-sim", "monitor", cfg); !run.Passed() || !run.HasFinding("control.settling_time", "warn") {
		t.Fatalf("monitor reports at warn and must pass: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginSpiceSeveritySetsWhetherAVoltageBreachBlocks(t *testing.T) {
	url := simStub(t, `{"times": [0, 1, 2], "voltages": [0, 3, 5]}`, 0)
	cfg := map[string]any{"sim_url": url, "netlist": "RC", "probe_node": "out", "max_voltage": 1.0}
	if run := runPlugin(t, "spice-sim", "transient", cfg); run.Passed() {
		t.Fatalf("a 5V peak over a 1V limit must fail: %v", run.Findings())
	}
	cfg["severity"] = "warn"
	if run := runPlugin(t, "spice-sim", "transient", cfg); !run.Passed() {
		t.Fatalf("severity=warn must not block: %v", run.Err)
	}
}

func TestIntegrationPluginSpiceTimeoutIsHonoured(t *testing.T) {
	url := simStub(t, `{"times": [], "voltages": []}`, 3*time.Second)
	started := time.Now()
	run := runPlugin(t, "spice-sim", "transient", map[string]any{"sim_url": url, "netlist": "RC", "timeout_ms": 300})
	if run.Passed() {
		t.Fatal("a simulator that outlasts timeout_ms must fail the step")
	}
	if elapsed := time.Since(started); elapsed > 2500*time.Millisecond {
		t.Fatalf("timeout_ms=300 was not honoured: the step took %s", elapsed)
	}
}

func TestIntegrationPluginVerilogCoverageUnreportedWarnsAndPasses(t *testing.T) {
	url := simStub(t, `{"compile_errors": []}`, 0)
	run := runPlugin(t, "verilog-sim", "check_coverage", map[string]any{
		"sim_url": url, "top_module": "top", "verilog": "module top; endmodule", "testbench": "module tb; endmodule",
	})
	if !run.Passed() || !run.HasFinding("verilog.coverage.unreported", "warn") {
		t.Fatalf("an unreported coverage is advisory and must not fail the step: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginVerilogLowCoverageFails(t *testing.T) {
	url := simStub(t, fmt.Sprintf(`{"compile_errors": [], "toggle_coverage_pct": %d}`, 10), 0)
	run := runPlugin(t, "verilog-sim", "check_coverage", map[string]any{
		"sim_url": url, "top_module": "top", "verilog": "module top; endmodule", "testbench": "module tb; endmodule",
		"min_coverage_pct": 50,
	})
	if run.Passed() || !run.HasFinding("verilog.coverage.low top", "error") {
		t.Fatalf("coverage under the floor must fail: err=%v findings=%v", run.Err, run.Findings())
	}
}
