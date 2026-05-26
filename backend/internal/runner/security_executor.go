package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Label         string            `json:"label,omitempty"`
	Method        string            `json:"method,omitempty"`
	Path          string            `json:"path,omitempty"`
	Body          string            `json:"body,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Payloads      []string          `json:"payloads,omitempty"`
	ExpectStatus  []int             `json:"expect_status,omitempty"`
	ErrorMarkers  []string          `json:"error_markers,omitempty"`
	Severity      string            `json:"severity,omitempty"`
}

// scannerResult is the JSON findings report returned by the Lua plugin.
type scannerResult struct {
	Total    int               `json:"total"`
	Passed   int               `json:"passed"`
	Failed   int               `json:"failed"`
	Findings []scannerFinding  `json:"findings"`
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
		return fmt.Errorf("security step %q requires an APISIX sidecar — add a service.mock node before this step in the topology", step.Node.Name)
	}

	gatewayURL := strings.TrimSuffix(strings.TrimSpace(step.GatewayURL), "/")
	mode := strings.TrimPrefix(step.Security.Variant, "security.")

	emit(line(step, "info", fmt.Sprintf("[%s] Starting %s assessment via APISIX sidecar at %s.", step.Node.Name, mode, gatewayURL)))

	cfg := buildScannerConfig(step.Security, gatewayURL)
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("security: marshal config: %w", err)
	}

	result, err := triggerScanner(ctx, gatewayURL, cfgJSON)
	if err != nil {
		return fmt.Errorf("security: scanner failed: %w", err)
	}

	for _, f := range result.Findings {
		emit(line(step, "warn", fmt.Sprintf("[%s] Finding [%s] %s %s status=%d severity=%s — %s.",
			step.Node.Name, f.Label, f.Method, f.Path, f.Status, f.Severity, f.Detail)))
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Assessment complete: total=%d passed=%d failed=%d findings=%d.",
		step.Node.Name, result.Total, result.Passed, result.Failed, len(result.Findings))))

	return evaluateSecurityThresholds(step, result)
}

func buildScannerConfig(spec *suites.SecuritySpec, gatewayURL string) scannerConfig {
	mode := strings.TrimPrefix(spec.Variant, "security.")
	target := spec.Target
	if target == "" {
		target = gatewayURL
	}
	cfg := scannerConfig{
		Target:    target,
		Mode:      mode,
		Technique: spec.Technique,
		TimeoutMS: 10000,
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

func evaluateSecurityThresholds(step StepSpec, result *scannerResult) error {
	if result.Failed > 0 && (step.Security == nil || len(step.Security.Thresholds) == 0) {
		return fmt.Errorf("%d security check(s) failed with %d finding(s)", result.Failed, len(result.Findings))
	}

	for _, threshold := range step.Security.Thresholds {
		var actual float64
		switch threshold.Metric {
		case "findings.total":
			actual = float64(len(result.Findings))
		case "findings.failed":
			actual = float64(result.Failed)
		default:
			continue
		}
		if !compareLoadValue(actual, threshold.Operator, threshold.Value) {
			return fmt.Errorf("security threshold %s %s %.0f violated (got %.0f)",
				threshold.Metric, threshold.Operator, threshold.Value, actual)
		}
	}
	return nil
}
