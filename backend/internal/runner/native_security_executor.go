package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

var nativeSecurityClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func executeNativeSecurityStep(ctx context.Context, step StepSpec, emit func(logstream.Line)) error {
	if step.Security == nil {
		return fmt.Errorf("security step %q has no security spec", step.Node.Name)
	}
	target := strings.TrimRight(strings.TrimSpace(step.Security.Target), "/")
	if target == "" {
		return fmt.Errorf("security step %q: target is required for native security checks", step.Node.Name)
	}

	variant := step.Security.Variant
	mode := strings.TrimPrefix(variant, "security.")
	emit(line(step, "info", fmt.Sprintf("[%s] Running native %s assessment against %s.", step.Node.Name, mode, target)))

	var result scannerResult
	var err error

	switch variant {
	case "security.probe":
		result, err = runNativeProbe(ctx, step, target, emit)
	case "security.headers":
		result, err = runNativeHeaders(ctx, step, target, emit)
	case "security.verbs":
		result, err = runNativeVerbs(ctx, step, target, emit)
	case "security.cors":
		result, err = runNativeCORS(ctx, step, target, emit)
	case "security.auth":
		result, err = runNativeAuth(ctx, step, target, emit)
	case "security.fuzz":
		result, err = runNativeFuzz(ctx, step, target, emit)
	default:
		emit(line(step, "warn", fmt.Sprintf("[%s] Native executor does not support %s; skipping.", step.Node.Name, variant)))
		return nil
	}
	if err != nil {
		return err
	}

	for _, f := range result.Findings {
		emit(line(step, "warn", fmt.Sprintf("[%s] Finding [%s] %s %s status=%d severity=%s — %s.",
			step.Node.Name, f.Label, f.Method, f.Path, f.Status, f.Severity, f.Detail)))
	}
	emit(line(step, "info", fmt.Sprintf("[%s] Assessment complete: total=%d passed=%d failed=%d findings=%d.",
		step.Node.Name, result.Total, result.Passed, result.Failed, len(result.Findings))))

	return evaluateSecurityThresholds(step, &result)
}

// runNativeProbe checks that the target responds and measures latency.
func runNativeProbe(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	result := scannerResult{Total: 1}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/", nil)
	if err != nil {
		return result, fmt.Errorf("probe: build request: %w", err)
	}
	resp, err := nativeSecurityClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		result.Failed++
		result.Findings = append(result.Findings, scannerFinding{
			Label: "probe.unreachable", Method: "GET", Path: "/",
			Severity: "high", Detail: err.Error(),
		})
		return result, nil //nolint:nilerr // network failure is a finding, not a fatal executor error
	}
	resp.Body.Close()
	emit(line(step, "info", fmt.Sprintf("[%s] Probe: status=%d latency=%s.", step.Node.Name, resp.StatusCode, latency.Round(time.Millisecond))))
	if resp.StatusCode >= 500 {
		result.Failed++
		result.Findings = append(result.Findings, scannerFinding{
			Label: "probe.server-error", Method: "GET", Path: "/",
			Status: resp.StatusCode, Severity: "high",
			Detail: fmt.Sprintf("target returned HTTP %d", resp.StatusCode),
		})
		return result, nil
	}
	result.Passed++
	return result, nil
}

// runNativeHeaders checks for the presence of expected security response headers.
func runNativeHeaders(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	type headerCheck struct {
		name     string
		header   string
		required bool
		severity string
	}
	checks := []headerCheck{
		{"hsts", "Strict-Transport-Security", false, "medium"},
		{"x-frame-options", "X-Frame-Options", true, "medium"},
		{"x-content-type-options", "X-Content-Type-Options", true, "low"},
		{"csp", "Content-Security-Policy", true, "medium"},
		{"referrer-policy", "Referrer-Policy", false, "low"},
		{"permissions-policy", "Permissions-Policy", false, "low"},
	}
	result := scannerResult{Total: len(checks)}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/", nil)
	if err != nil {
		return result, fmt.Errorf("headers: build request: %w", err)
	}
	resp, err := nativeSecurityClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("headers: request failed: %w", err)
	}
	resp.Body.Close()

	for _, check := range checks {
		val := resp.Header.Get(check.header)
		if val == "" {
			if check.required {
				result.Failed++
			} else {
				result.Passed++
			}
			result.Findings = append(result.Findings, scannerFinding{
				Label: "header.missing." + check.name, Method: "GET", Path: "/",
				Status: resp.StatusCode, Severity: check.severity,
				Detail: fmt.Sprintf("response missing %s header", check.header),
			})
		} else {
			result.Passed++
		}
	}
	return result, nil
}

// runNativeVerbs checks for unsafe HTTP methods being accepted by the target.
func runNativeVerbs(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	dangerousMethods := []string{"TRACE", "CONNECT", "PROPFIND", "PROPPATCH", "MKCOL", "COPY", "MOVE", "LOCK", "UNLOCK"}
	result := scannerResult{Total: len(dangerousMethods)}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, method := range dangerousMethods {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, m, target+"/", nil)
			if err != nil {
				return
			}
			resp, err := nativeSecurityClient.Do(req)
			if err != nil {
				mu.Lock()
				result.Passed++
				mu.Unlock()
				return
			}
			resp.Body.Close()
			mu.Lock()
			defer mu.Unlock()
			if resp.StatusCode < 400 {
				result.Failed++
				result.Findings = append(result.Findings, scannerFinding{
					Label: "verb." + strings.ToLower(m), Method: m, Path: "/",
					Status: resp.StatusCode, Severity: "medium",
					Detail: fmt.Sprintf("server accepted unsafe method %s with status %d", m, resp.StatusCode),
				})
			} else {
				result.Passed++
			}
		}(method)
	}
	wg.Wait()
	return result, nil
}

// runNativeCORS checks for CORS misconfiguration (wildcard or reflected origins).
func runNativeCORS(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	result := scannerResult{Total: 2}

	probeOrigins := []struct {
		label  string
		origin string
	}{
		{"wildcard-null", "null"},
		{"reflected-attacker", "https://attacker.example.com"},
	}

	for _, probe := range probeOrigins {
		req, err := http.NewRequestWithContext(ctx, http.MethodOptions, target+"/", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Origin", probe.origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := nativeSecurityClient.Do(req)
		if err != nil {
			result.Passed++
			continue
		}
		resp.Body.Close()
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		acac := resp.Header.Get("Access-Control-Allow-Credentials")
		if acao == "*" || acao == probe.origin {
			sev := "medium"
			detail := fmt.Sprintf("Access-Control-Allow-Origin reflects %q", acao)
			if strings.EqualFold(acac, "true") {
				sev = "high"
				detail += " with Access-Control-Allow-Credentials: true"
			}
			result.Failed++
			result.Findings = append(result.Findings, scannerFinding{
				Label: "cors." + probe.label, Method: "OPTIONS", Path: "/",
				Status: resp.StatusCode, Severity: sev, Detail: detail,
			})
		} else {
			result.Passed++
		}
	}
	return result, nil
}

// runNativeAuth tests for common authentication bypass patterns.
func runNativeAuth(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	bypassHeaders := []struct {
		label  string
		header string
		value  string
	}{
		{"x-original-url", "X-Original-URL", "/admin"},
		{"x-rewrite-url", "X-Rewrite-URL", "/admin"},
		{"x-forwarded-host", "X-Forwarded-Host", "localhost"},
		{"x-custom-ip", "X-Forwarded-For", "127.0.0.1"},
	}
	result := scannerResult{Total: len(bypassHeaders)}

	// Establish a baseline response code without bypass headers.
	baseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/api/", nil)
	if err != nil {
		return result, fmt.Errorf("auth: build baseline request: %w", err)
	}
	baseResp, err := nativeSecurityClient.Do(baseReq)
	baseStatus := 401
	if err == nil {
		baseStatus = baseResp.StatusCode
		baseResp.Body.Close()
	}

	for _, bypass := range bypassHeaders {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"/api/", nil)
		if err != nil {
			result.Passed++
			continue
		}
		req.Header.Set(bypass.header, bypass.value)
		resp, err := nativeSecurityClient.Do(req)
		if err != nil {
			result.Passed++
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < baseStatus && resp.StatusCode < 400 {
			result.Failed++
			result.Findings = append(result.Findings, scannerFinding{
				Label: "auth." + bypass.label, Method: "GET", Path: "/api/",
				Status: resp.StatusCode, Severity: "high",
				Detail: fmt.Sprintf("request with %s: %s returned %d vs baseline %d", bypass.header, bypass.value, resp.StatusCode, baseStatus),
			})
		} else {
			result.Passed++
		}
	}
	return result, nil
}

// runNativeFuzz sends basic injection payloads and checks for error disclosure.
func runNativeFuzz(ctx context.Context, step StepSpec, target string, emit func(logstream.Line)) (scannerResult, error) {
	probes := []struct {
		label   string
		path    string
		payload string
	}{
		{"sqli-basic", "/?id=1'", ""},
		{"sqli-union", "/?id=1+UNION+SELECT+1--", ""},
		{"xss-reflect", "/?q=<script>alert(1)</script>", ""},
		{"path-traversal", "/../../../etc/passwd", ""},
		{"ssti-basic", "/?name={{7*7}}", ""},
		{"open-redirect", "/?redirect=//attacker.example.com", ""},
	}

	sqlErrorMarkers := []string{
		"sql syntax", "mysql_fetch", "ora-", "pg_query", "sqlite_", "unclosed quotation",
		"you have an error in your sql", "warning: mysql", "supplied argument is not a valid mysql",
	}
	xssMarkers := []string{"<script>alert(1)</script>"}

	result := scannerResult{Total: len(probes)}

	for _, probe := range probes {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+probe.path, nil)
		if err != nil {
			result.Passed++
			continue
		}
		resp, err := nativeSecurityClient.Do(req)
		if err != nil {
			result.Passed++
			continue
		}

		bodyBuf := make([]byte, 8192)
		n, _ := resp.Body.Read(bodyBuf)
		resp.Body.Close()
		bodyLower := strings.ToLower(string(bodyBuf[:n]))

		finding := false
		detail := ""
		switch {
		case strings.HasPrefix(probe.label, "sqli"):
			for _, marker := range sqlErrorMarkers {
				if strings.Contains(bodyLower, marker) {
					finding = true
					detail = "SQL error disclosure in response body"
					break
				}
			}
		case strings.HasPrefix(probe.label, "xss"):
			for _, marker := range xssMarkers {
				if strings.Contains(bodyLower, strings.ToLower(marker)) {
					finding = true
					detail = "unescaped payload reflected in response"
					break
				}
			}
		case strings.HasPrefix(probe.label, "path"):
			if strings.Contains(bodyLower, "root:") || strings.Contains(bodyLower, "/bin/bash") {
				finding = true
				detail = "path traversal reached system file"
			}
		}

		if finding {
			result.Failed++
			result.Findings = append(result.Findings, scannerFinding{
				Label: probe.label, Method: "GET", Path: probe.path,
				Status: resp.StatusCode, Severity: "high", Detail: detail,
			})
		} else {
			result.Passed++
		}
	}
	return result, nil
}
