package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/pluginschema"
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

// mergePluginConfig builds a plugin step's config: the profile env provides
// defaults, and the suite call's kwargs override them.
//
// A kwarg that is unset — None, or the empty string a plugin.star wrapper
// passes for an optional parameter nobody gave — is not an override. Without
// that rule every wrapper default erased the profile's value: electronics-lab
// sets sim_url per step in its profile, the spice-sim wrapper forwards
// sim_url="", and the plugin was handed an empty URL on every backend.
func mergePluginConfig(env map[string]string, pluginConfig any) map[string]any {
	config := make(map[string]any, len(env))
	for k, v := range env {
		config[k] = v
	}
	kwargs, _ := pluginConfig.(map[string]any)
	for k, v := range kwargs {
		if _, fromProfile := config[k]; fromProfile && isUnsetPluginValue(v) {
			continue
		}
		config[k] = v
	}
	return config
}

func isUnsetPluginValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// executePlugin dispatches a plugin.run() step to the registered APISIX Lua plugin.
func executePlugin(ctx context.Context, step StepSpec, emit func(logstream.Line), plugin *platform.CustomPlugin) error {
	gatewayURL := strings.TrimSuffix(strings.TrimSpace(step.GatewayURL), "/")

	op := ""
	if step.Plugin != nil {
		op = strings.TrimSpace(step.Plugin.Op)
	}
	if op != "" {
		emit(line(step, "debug", fmt.Sprintf("[%s] Dispatching to plugin %q op=%s at %s.", step.Node.Name, plugin.Name, op, gatewayURL)))
	} else {
		emit(line(step, "debug", fmt.Sprintf("[%s] Dispatching to plugin %q at %s.", step.Node.Name, plugin.Name, gatewayURL)))
	}

	config := mergePluginConfig(step.Env, step.Plugin.Config)
	if err := validatePluginStepConfig(plugin.Schema, step, config); err != nil {
		return fmt.Errorf("plugin %s config: %w", plugin.Name, err)
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
		// A plugin that cannot run its check replies with an error and a status
		// other than 200. Say what it said, and which status it came with.
		return fmt.Errorf("plugin %s: %s (HTTP %d)", plugin.Name, pluginErrorMessage(respBody), resp.StatusCode)
	}

	var result pluginResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("plugin %s: parse response: %w", plugin.Name, err)
	}
	if result.Passed == nil {
		// Without this a reply of {} reads as passed=false and is reported as
		// "0 findings exceeded thresholds", which describes nothing.
		return fmt.Errorf("plugin %s: the reply has no boolean \"passed\" field", plugin.Name)
	}
	passed := *result.Passed

	recordPluginSpanAttributes(ctx, result.Attributes)

	if result.Stderr != "" {
		emit(line(step, "warn", fmt.Sprintf("[%s] Plugin stderr: %s", step.Node.Name, strings.TrimSpace(result.Stderr))))
	}

	var unknownSeverity string
	for _, f := range result.Findings {
		level, known := pluginFindingLevel(f.Severity)
		if !known && unknownSeverity == "" {
			unknownSeverity = f.Severity
		}
		emit(line(step, level, fmt.Sprintf("[%s] Finding [%s] severity=%s — %s.", step.Node.Name, f.Label, f.Severity, f.Detail)))
	}
	if unknownSeverity != "" {
		emit(line(step, "warn", fmt.Sprintf("[%s] Plugin %q reports severity %q, which is not one of info, warn or critical; it is shown as a warning.", step.Node.Name, plugin.Name, unknownSeverity)))
	}

	if s := strings.TrimSpace(result.Summary); s != "" {
		lvl := strings.TrimSpace(result.Level)
		if lvl == "" {
			lvl = "info"
		}
		emit(line(step, lvl, fmt.Sprintf("[%s] %s", step.Node.Name, s)))
	}

	emit(line(step, "debug", fmt.Sprintf("[%s] Plugin complete: passed=%v findings=%d.", step.Node.Name, passed, len(result.Findings))))

	if !passed {
		return fmt.Errorf("plugin %s: %d finding(s) exceeded thresholds", plugin.Name, len(result.Findings))
	}
	// passed alone decides the step. A plugin that grades against a threshold
	// (a judge scoring cases, say) may pass with a critical finding for a case
	// that missed, so a critical finding next to passed=true is not an error.
	return nil
}

// pluginFindingLevel maps a finding's severity to a log level. critical blocks;
// high and error are accepted as aliases of it for plugins written before the
// vocabulary settled. The second result is false for a severity that is none of
// the known ones, which is logged as a warning.
func pluginFindingLevel(severity string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high", "error":
		return "error", true
	case "warn", "warning":
		return "warn", true
	case "info":
		return "info", true
	default:
		return "warn", false
	}
}

// pluginErrorMessage reads the {"error": "..."} body a plugin sends when it
// cannot run its check, and falls back to the raw body.
func pluginErrorMessage(body []byte) string {
	var reply struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(body, &reply); err == nil && reply.Error != nil {
		if text, ok := reply.Error.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 400 {
		text = text[:400] + "…"
	}
	if text == "" {
		return "the sidecar sent an empty reply"
	}
	return text
}

// validatePluginStepConfig checks a step's config against the plugin's CUE
// schema, if it has one. Unset values (None, or the empty string a wrapper
// passes for an optional setting nobody gave) count as absent, so a schema
// default applies and a required setting that was never given is reported.
//
// A value that came from the profile's environment is text. When it fails as
// text, it is tried again as the number or boolean it spells, so a profile that
// sets max_lag: "500" is not refused by a schema that says int.
func validatePluginStepConfig(schema string, step StepSpec, config map[string]any) error {
	if strings.TrimSpace(schema) == "" {
		return nil
	}
	present := make(map[string]any, len(config))
	for key, value := range config {
		if !isUnsetPluginValue(value) {
			present[key] = value
		}
	}
	err := pluginschema.Validate(schema, present)
	if err == nil {
		return nil
	}

	var kwargs map[string]any
	if step.Plugin != nil {
		kwargs, _ = step.Plugin.Config.(map[string]any)
	}
	coerced := make(map[string]any, len(present))
	changed := false
	for key, value := range present {
		coerced[key] = value
		text, isText := value.(string)
		if !isText {
			continue
		}
		if given, fromCall := kwargs[key]; fromCall && !isUnsetPluginValue(given) {
			continue // the suite wrote this one, and wrote it as text on purpose
		}
		if number, parseErr := strconv.ParseFloat(strings.TrimSpace(text), 64); parseErr == nil {
			coerced[key] = number
			changed = true
		} else if flag, parseErr := strconv.ParseBool(strings.TrimSpace(text)); parseErr == nil {
			coerced[key] = flag
			changed = true
		}
	}
	if changed && pluginschema.Validate(schema, coerced) == nil {
		return nil
	}
	return err
}

type pluginResult struct {
	// Passed is a pointer so a reply without the field can be told from one that
	// says false.
	Passed   *bool          `json:"passed"`
	Findings pluginFindings `json:"findings"`
	Stderr   string         `json:"stderr,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Level    string         `json:"level,omitempty"` // log level for summary: info, debug, warn, error
	// Attributes are added to the runner.run span for this step. A plugin
	// knows what it just did — which tool it called, which model answered — and
	// this is the only way that detail reaches the trace. See
	// recordPluginSpanAttributes for the cardinality limits.
	Attributes map[string]string `json:"attributes,omitempty"`
}

type pluginFinding struct {
	Label    string `json:"label"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// pluginFindings decodes the findings list leniently, because the JSON comes
// from plugin code this runner does not own. Lua's cjson serialises an empty
// table as {} rather than [], so a plugin that found nothing — the passing
// case, and the one most worth not breaking — otherwise fails to decode and
// reports a parse error instead of success.
type pluginFindings []pluginFinding

func (f *pluginFindings) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = nil
		return nil
	}
	if trimmed[0] == '{' {
		// Only an empty object is a findings list in disguise. Anything with
		// keys is a real mistake, and saying so beats guessing.
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return err
		}
		if len(object) > 0 {
			return fmt.Errorf("findings must be an array, got an object with %d field(s)", len(object))
		}
		*f = pluginFindings{}
		return nil
	}
	var list []pluginFinding
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return err
	}
	*f = list
	return nil
}
