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

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/platform"
)

var pluginHTTPClient = &http.Client{Timeout: 15 * time.Minute}

// canUsePlugin returns the matching registered plugin for this step, if any.
func canUsePlugin(step StepSpec, plugins []platform.CustomPlugin) (*platform.CustomPlugin, error) {
	if step.Plugin == nil || strings.TrimSpace(step.GatewayURL) == "" {
		return nil, nil
	}
	for i := range plugins {
		p := &plugins[i]
		if p.Name != step.Plugin.Name {
			continue
		}
		if len(p.Operations) > 0 {
			for _, allowed := range p.Operations {
				if allowed == step.Plugin.Op {
					return p, nil
				}
			}
			return nil, fmt.Errorf("plugin %q does not support operation %q (supported: %s)",
				p.Name, step.Plugin.Op, strings.Join(p.Operations, ", "))
		}
		return p, nil
	}
	return nil, nil
}

// executePlugin dispatches a plugin.run() step to the registered APISIX Lua plugin.
func executePlugin(ctx context.Context, step StepSpec, emit func(logstream.Line), plugin *platform.CustomPlugin) error {
	gatewayURL := strings.TrimSuffix(strings.TrimSpace(step.GatewayURL), "/")

	emit(line(step, "info", fmt.Sprintf("[%s] Dispatching to plugin %q at %s.", step.Node.Name, plugin.Name, gatewayURL)))

	// Profile env provides defaults; suite-call kwargs (Plugin.Config) override per-step.
	config := make(map[string]any, len(step.Env))
	for k, v := range step.Env {
		config[k] = v
	}
	if pluginCfg, ok := step.Plugin.Config.(map[string]any); ok {
		for k, v := range pluginCfg {
			config[k] = v
		}
	}

	payload := map[string]any{
		"step":   step.Node.Name,
		"op":     step.Plugin.Op,
		"config": config,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("plugin %s: marshal payload: %w", plugin.Name, err)
	}

	trigger := strings.TrimSpace(plugin.Trigger)
	if trigger == "" {
		return fmt.Errorf("plugin %s: no trigger route configured", plugin.Name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gatewayURL+trigger, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("plugin %s: build request: %w", plugin.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := pluginHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("plugin %s: request failed: %w", plugin.Name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("plugin %s: read response: %w", plugin.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("plugin %s: sidecar returned HTTP %d: %s", plugin.Name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result pluginResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("plugin %s: parse response: %w", plugin.Name, err)
	}

	if result.Stderr != "" {
		emit(line(step, "warn", fmt.Sprintf("[%s] Plugin stderr: %s", step.Node.Name, strings.TrimSpace(result.Stderr))))
	}

	for _, f := range result.Findings {
		level := "warn"
		if f.Severity == "critical" {
			level = "error"
		}
		emit(line(step, level, fmt.Sprintf("[%s] Finding [%s] severity=%s — %s.", step.Node.Name, f.Label, f.Severity, f.Detail)))
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Plugin complete: passed=%v findings=%d.", step.Node.Name, result.Passed, len(result.Findings))))

	if !result.Passed {
		return fmt.Errorf("plugin %s: %d finding(s) exceeded thresholds", plugin.Name, len(result.Findings))
	}
	return nil
}

type pluginResult struct {
	Passed   bool            `json:"passed"`
	Findings []pluginFinding `json:"findings"`
	Stderr   string          `json:"stderr,omitempty"`
}

type pluginFinding struct {
	Label    string `json:"label"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}
