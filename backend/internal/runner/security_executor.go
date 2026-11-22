package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/suites"
)

var scannerHTTPClient = &http.Client{Timeout: 10 * time.Minute}

// scannerConfig is the JSON payload sent to the attack-scanner Lua plugin.
type scannerConfig struct {
	Target         string         `json:"target"`
	HostHeader     string         `json:"host_header,omitempty"`
	Mode           string         `json:"mode"`
	Technique      string         `json:"technique,omitempty"`
	Checks         []scannerCheck `json:"checks,omitempty"`
	Path           string         `json:"path,omitempty"`
	Method         string         `json:"method,omitempty"`
	Rate           float64        `json:"rate,omitempty"`
	Duration       float64        `json:"duration,omitempty"`
	Severity       string         `json:"severity,omitempty"`
	ExpectThrottle bool           `json:"expect_throttle,omitempty"`
	RateLimit      int            `json:"rate_limit,omitempty"`
	TimeoutMS      int            `json:"timeout_ms,omitempty"`
}

type scannerCheck struct {
	Label        string            `json:"label,omitempty"`
	Method       string            `json:"method,omitempty"`
	Path         string            `json:"path,omitempty"`
	Body         string            `json:"body,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Payloads     []string          `json:"payloads,omitempty"`
	ExpectStatus []int             `json:"expect_status,omitempty"`
	ErrorMarkers []string          `json:"error_markers,omitempty"`
	Severity     string            `json:"severity,omitempty"`
}

// scannerResult is the JSON findings report returned by the Lua plugin. Every
// check ends as passed, failed or unknown: Total is their sum. Unknown counts the
// checks the target gave no usable answer to, and Skipped the payloads the
// scanner's own HTTP client could not send; neither is a pass.
type scannerResult struct {
	Total      int              `json:"total"`
	Passed     int              `json:"passed"`
	Failed     int              `json:"failed"`
	Unknown    int              `json:"unknown"`
	Skipped    int              `json:"skipped"`
	Requests   int              `json:"requests"`
	FirstError string           `json:"first_error"`
	Findings   []scannerFinding `json:"findings"`
}

type scannerFinding struct {
	Label    string `json:"label"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Payload  string `json:"payload,omitempty"`
	Status   int    `json:"status"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

func canUseAPISIXSecurity(step StepSpec) bool {
	if strings.TrimSpace(step.GatewayURL) == "" {
		return false
	}
	if step.Security == nil {
		return false
	}
	switch step.Security.Variant {
	case "security.probe", "security.fuzz", "security.auth", "security.flood",
		"security.headers", "security.verbs", "security.graphql", "security.cors":
		return true
	}
	return false
}

func executeSecurityStep(ctx context.Context, step StepSpec, emit func(logstream.Line)) error {
	if step.Security == nil {
		return fmt.Errorf("security step %q has no security spec", step.Node.Name)
	}

	if !canUseAPISIXSecurity(step) {
		return fmt.Errorf("security step %q requires an APISIX sidecar — add a mock.api node before this step in the topology", step.Node.Name)
	}

	gatewayURL := strings.TrimSuffix(strings.TrimSpace(step.GatewayURL), "/")
	mode := strings.TrimPrefix(step.Security.Variant, "security.")

	raw, err := securityTargetOf(step)
	if err != nil {
		return err
	}
	target, err := resolveSidecarTarget(ctx, step, "security", "a security scan of a service", raw)
	if err != nil {
		return err
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Starting %s assessment via APISIX sidecar at %s.", step.Node.Name, mode, gatewayURL)))
	emit(line(step, "info", fmt.Sprintf("[%s] Target %s resolved to %s: %s.", step.Node.Name, raw, target.URL, target.Note)))
	warnWhenScanningTheMock(step, raw, target, emit)

	cfg := buildScannerConfig(step.Security, target)
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("security: marshal config: %w", err)
	}

	// A flood is a load run; the other modes send a few requests and do not wait.
	if mode == "flood" {
		release, err := acquireSidecarLoad(ctx, step, emit)
		if err != nil {
			return err
		}
		defer release()
	}

	result, err := triggerScanner(ctx, gatewayURL, cfgJSON)
	if err != nil {
		return fmt.Errorf("security: scanner failed: %w", err)
	}

	floor := securityFailFloor(step.Security)
	for _, f := range result.Findings {
		level := "warn"
		if securityFindingRank(f.Severity) >= floor {
			level = "error"
		}
		emit(line(step, level, fmt.Sprintf("[%s] Finding [%s] %s %s status=%d severity=%s — %s.",
			step.Node.Name, f.Label, f.Method, f.Path, f.Status, f.Severity, f.Detail)))
	}
	if result.Skipped > 0 {
		emit(line(step, "warn", fmt.Sprintf("[%s] %d payload(s) could not be sent by the scanner's HTTP client (control characters) and were not counted.", step.Node.Name, result.Skipped)))
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Assessment complete: total=%d passed=%d failed=%d unknown=%d findings=%d requests=%d.",
		step.Node.Name, result.Total, result.Passed, result.Failed, result.Unknown, len(result.Findings), result.Requests)))

	publishSecurityReport(step, raw, mode, result, emit)

	return evaluateSecurityResult(step, raw, result)
}

// securityTargetOf is the target a security step scans, as the suite wrote it.
// Without one it is the suite's own mock surface, by its host. A suite with no
// mock to scan fails to load (see validateSecurityTargets), so reaching here
// without one is a step that was built some other way.
func securityTargetOf(step StepSpec) (string, error) {
	if target := strings.TrimSpace(step.Security.Target); target != "" {
		return target, nil
	}
	for _, host := range step.MockHosts {
		if host = strings.TrimSpace(host); host != "" {
			return "https://" + host, nil
		}
	}
	return "", fmt.Errorf("security step %q has no target= and the suite has no mock host to scan by default", step.Node.Name)
}

// warnWhenScanningTheMock says what a scan of the suite's own mock measures. The
// gateway answers for a mock, so a header or rate-limit check against one
// describes the gateway, and under harden=True it describes the defences the
// gateway adds on top.
func warnWhenScanningTheMock(step StepSpec, raw string, target resolvedTarget, emit func(logstream.Line)) {
	if target.HostHeader == "" {
		return
	}
	cause := ""
	if strings.TrimSpace(step.Security.Target) == "" {
		cause = "no target= given: "
	}
	emit(line(step, "warn", fmt.Sprintf("[%s] %sscanning the suite's own mock at %s, which measures the gateway and the mock, not a service of yours.", step.Node.Name, cause, raw)))
	if step.Hardened {
		emit(line(step, "warn", fmt.Sprintf("[%s] harden=True: the gateway adds security headers and a rate-limited route, so headers and flood results here describe the gateway.", step.Node.Name)))
	}
}

func buildScannerConfig(spec *suites.SecuritySpec, target resolvedTarget) scannerConfig {
	mode := strings.TrimPrefix(spec.Variant, "security.")
	cfg := scannerConfig{
		Target:     target.URL,
		HostHeader: target.HostHeader,
		Mode:       mode,
		Technique:  spec.Technique,
		TimeoutMS:  10000,
	}
	if mode == "flood" {
		if spec.FloodPath != "" {
			cfg.Path = spec.FloodPath
		}
		if spec.FloodRate > 0 {
			cfg.Rate = spec.FloodRate
		}
		if spec.FloodDuration > 0 {
			cfg.Duration = spec.FloodDuration
		}
		cfg.ExpectThrottle = spec.FloodThrottle
	}
	return cfg
}

func triggerScanner(ctx context.Context, gatewayURL string, cfgJSON []byte) (*scannerResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		gatewayURL+apisix.AttackScannerTriggerRoute,
		bytes.NewReader(cfgJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := scannerHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scanner returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result scannerResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// securityRanks orders the severities a finding can carry, lowest first.
var securityRanks = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}

// securityFindingRank is a finding's severity as a number. A severity the
// scanner does not define counts as medium, so it is neither ignored nor
// treated as the worst.
func securityFindingRank(severity string) int {
	if rank, ok := securityRanks[strings.ToLower(strings.TrimSpace(severity))]; ok {
		return rank
	}
	return securityRanks["medium"]
}

// securityFailFloor is the lowest severity that fails the step: fail_on=, and
// low (any finding) when it is not given.
func securityFailFloor(spec *suites.SecuritySpec) int {
	if spec != nil {
		if rank, ok := securityRanks[strings.ToLower(strings.TrimSpace(spec.FailOn))]; ok && spec.FailOn != "" {
			return rank
		}
	}
	return securityRanks["low"]
}

// securityFloorName is the fail_on value for a floor rank.
func securityFloorName(floor int) string {
	for _, name := range []string{"low", "medium", "high", "critical"} {
		if securityRanks[name] == floor {
			return name
		}
	}
	return "low"
}

// evaluateSecurityResult decides the step from what the scanner found. A check
// the target gave no answer to is unknown, and unknown is never a pass:
//
//   - nothing determined at all fails with the first error, so a target that
//     could not be reached reads as such and not as "no findings";
//   - some checks unknown fails with how many, and the first error;
//   - findings at or above fail_on fail the step; findings below it are shown
//     as warnings and do not.
func evaluateSecurityResult(step StepSpec, target string, result *scannerResult) error {
	if result.Passed+result.Failed == 0 {
		reason := strings.TrimSpace(result.FirstError)
		if reason == "" {
			reason = "no check was executed"
			if result.Skipped > 0 {
				reason = fmt.Sprintf("%d payload(s) could not be sent by the scanner's HTTP client", result.Skipped)
			}
		}
		return fmt.Errorf("no check reached %s: %s", target, reason)
	}

	floor := securityFailFloor(step.Security)
	blocking := 0
	for _, f := range result.Findings {
		if securityFindingRank(f.Severity) >= floor {
			blocking++
		}
	}

	var problems []string
	if blocking > 0 {
		problems = append(problems, fmt.Sprintf("%d finding(s) at or above %s severity", blocking, securityFloorName(floor)))
	}
	if result.Unknown > 0 {
		reason := strings.TrimSpace(result.FirstError)
		if reason == "" {
			reason = "no usable answer"
		}
		problems = append(problems, fmt.Sprintf("%d of %d checks could not be determined: %s", result.Unknown, result.Total, reason))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// securityReport is the JSON a security step exports: what was scanned, how
// every check ended, and the findings.
type securityReport struct {
	Step       string           `json:"step"`
	Mode       string           `json:"mode"`
	Target     string           `json:"target"`
	FailOn     string           `json:"fail_on"`
	Total      int              `json:"total"`
	Passed     int              `json:"passed"`
	Failed     int              `json:"failed"`
	Unknown    int              `json:"unknown"`
	Skipped    int              `json:"skipped"`
	Requests   int              `json:"requests"`
	FirstError string           `json:"first_error,omitempty"`
	Findings   []scannerFinding `json:"findings"`
}

// publishSecurityReport hands the report to every export the step declares, so
// it shows in the execution view, and, on the Docker backend, writes it into the
// execution's workspace at the export's path, where a later step reads it. A
// security step runs in the control plane with no container to write a file, so
// before this the exports could never exist.
func publishSecurityReport(step StepSpec, target, mode string, result *scannerResult, emit func(logstream.Line)) {
	if len(step.ArtifactExports) == 0 {
		return
	}
	findings := result.Findings
	if findings == nil {
		findings = []scannerFinding{}
	}
	content, err := json.MarshalIndent(securityReport{
		Step: step.Node.Name, Mode: mode, Target: target, FailOn: securityFloorName(securityFailFloor(step.Security)),
		Total: result.Total, Passed: result.Passed, Failed: result.Failed, Unknown: result.Unknown,
		Skipped: result.Skipped, Requests: result.Requests, FirstError: result.FirstError, Findings: findings,
	}, "", "  ")
	if err != nil {
		emit(line(step, "warn", fmt.Sprintf("[%s] The report could not be encoded: %v.", step.Node.Name, err)))
		return
	}

	results := make([]ArtifactResult, 0, len(step.ArtifactExports))
	for _, export := range step.ArtifactExports {
		exportPath := strings.TrimSpace(export.Path)
		results = append(results, ArtifactResult{
			Export: exportPath,
			Files:  []ArtifactFile{{Path: exportPath, Content: content}},
		})
	}
	deliverArtifacts(step, results)

	if step.BackendKind == "kubernetes" || step.BackendKind == "remote" {
		emit(line(step, "warn", fmt.Sprintf("[%s] The report was not written to the execution workspace: the control plane cannot write into the volume a %s backend gives its steps.", step.Node.Name, step.BackendKind)))
		return
	}
	for _, export := range step.ArtifactExports {
		if err := writeWorkspaceFile(step.ExecutionID, export.Path, content); err != nil {
			emit(line(step, "warn", fmt.Sprintf("[%s] The report was not written to the execution workspace at %s: %v.", step.Node.Name, export.Path, err)))
		}
	}
}

// writeWorkspaceFile writes content into an execution's workspace at a path
// relative to its root. The path is cleaned and checked to stay inside the
// workspace.
func writeWorkspaceFile(executionID, relative string, content []byte) error {
	root := filepath.Clean(ExecutionWorkspaceDir(executionID))
	target := filepath.Join(root, filepath.FromSlash(path.Clean("/"+strings.TrimSpace(relative))))
	if !strings.HasPrefix(target+string(filepath.Separator), root+string(filepath.Separator)) || target == root {
		return fmt.Errorf("the path %q is outside the workspace", relative)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, content, 0o644)
}
