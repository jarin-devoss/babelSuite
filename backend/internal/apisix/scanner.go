package apisix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AttackScannerPluginName is the APISIX plugin name registered in apisix.yaml.
const AttackScannerPluginName = "babelsuite-attack-scanner"

// AttackScannerTriggerRoute is the URI the BabelSuite runner POSTs to.
const AttackScannerTriggerRoute = "/_babelsuite/attack/start"

// LuaPluginMountPath is the container path where custom Lua plugin files must
// be placed so APISIX can load them by name.
const LuaPluginMountPath = "/usr/local/openresty/site/lualib/apisix/plugins"

// LuaPluginFiles returns every Lua plugin file the sidecar mounts, by file name:
// the two built-in plugins and each user plugin that carries source.
func LuaPluginFiles(userPlugins ...CustomPluginConfig) map[string]string {
	plugins := map[string]string{
		TrafficCannonPluginName + ".lua": TrafficCannonLua,
		AttackScannerPluginName + ".lua": AttackScannerLua,
		MockPluginName + ".lua":          mockLua,
		AsyncMockPluginName + ".lua":     asyncMockLua,
	}
	for _, p := range userPlugins {
		if strings.TrimSpace(p.Lua) != "" {
			plugins[p.Name+".lua"] = p.Lua
		}
	}
	return plugins
}

// WriteLuaPluginFiles writes the Lua plugin sources to destDir so they can be
// bind-mounted into the APISIX sidecar container before it starts.
// Returns an error if any file cannot be written.
//
// The files are 0644, not 0600: the container runs as the unprivileged user
// apisix, which on a Linux host cannot read a file only its owner may read.
func WriteLuaPluginFiles(destDir string, userPlugins ...CustomPluginConfig) error {
	for name, code := range LuaPluginFiles(userPlugins...) {
		p := filepath.Join(destDir, name)
		if err := os.WriteFile(p, []byte(code), 0644); err != nil {
			return fmt.Errorf("write lua plugin %s: %w", name, err)
		}
	}
	// The stream plugins live in a directory of their own, mounted separately.
	streamDir := filepath.Join(destDir, StreamPluginDir)
	if err := os.MkdirAll(streamDir, 0755); err != nil {
		return fmt.Errorf("create the stream plugin dir: %w", err)
	}
	for name, code := range StreamLuaPluginFiles() {
		if err := os.WriteFile(filepath.Join(streamDir, name), []byte(code), 0644); err != nil {
			return fmt.Errorf("write stream lua plugin %s: %w", name, err)
		}
	}
	return nil
}

// AttackScannerLua is the OpenResty/Lua plugin embedded verbatim into the
// generated apisix.yaml.  It drives HTTP-layer attack simulations entirely
// inside the APISIX sidecar — no extra containers or external processes.
//
// Modes:
//   - probe:   Requests sensitive paths; flags unexpected status codes.
//   - fuzz:    Injects SQLi / XSS / traversal / header-injection payloads.
//   - auth:    Checks protected paths are unreachable without credentials.
//   - flood:   Verifies rate-limiting (HTTP 429), or availability, under sustained traffic.
//   - headers: Audits security response headers (HSTS, CSP, X-Frame, …).
//   - verbs:   Probes dangerous HTTP methods (PUT, DELETE, TRACE, TRACK).
//   - graphql: Detects exposed GraphQL introspection endpoints.
//   - cors:    Finds CORS misconfigurations (reflected origin, wildcard).
//
// Every check ends as exactly one of three outcomes: passed (the target
// answered and the answer is safe), failed (it answered and the answer is a
// finding) or unknown (no usable answer: DNS, connect, timeout, reset). A
// check that could not be determined is never counted as passed. A fuzz payload
// the scanner's own HTTP client refuses to send is counted as skipped, because
// it never reached the target.
//
// All modes POST to /_babelsuite/attack/start and return a synchronous JSON
// findings report once the run completes:
//
//	{"total":N,"passed":N,"failed":N,"unknown":N,"skipped":N,"requests":N,
//	 "first_error":"...","findings":[{...}]}
const AttackScannerLua = `
local json     = require("cjson")
local http_lib = require("resty.http")

local function as_array(t) return #t == 0 and json.empty_array or t end

-- ── requests ───────────────────────────────────────────────────────────────

-- send returns status, body, lowercase-keyed response headers and an error. The
-- Host header is set from cfg.host_header when the target is a mock surface the
-- sidecar selects by host: without it every request meets "route not found".
local function send(cfg, method, path, body, headers)
    local httpc = http_lib.new()
    httpc:set_timeout(cfg.timeout_ms or 10000)
    local sent = {}
    for k, v in pairs(headers or {}) do sent[k] = v end
    if cfg.host_header and cfg.host_header ~= "" and sent["Host"] == nil and sent["host"] == nil then
        sent["Host"] = cfg.host_header
    end
    local res, err = httpc:request_uri(cfg.target .. path, {
        method  = method or "GET",
        body    = body   or nil,
        headers = sent,
    })
    if not res then
        return nil, nil, {}, tostring(err or "no response")
    end
    local norm = {}
    for k, v in pairs(res.headers or {}) do norm[string.lower(k)] = v end
    return res.status, res.body or "", norm, nil
end

-- ── outcomes ───────────────────────────────────────────────────────────────

local function new_results()
    return {total = 0, passed = 0, failed = 0, unknown = 0, skipped = 0,
            requests = 0, first_error = "", findings = {}}
end

local function finding(cfg, label, method, path, payload, status, severity, detail)
    local lbl = label or path
    if cfg.technique and cfg.technique ~= "" then lbl = cfg.technique .. "/" .. lbl end
    return {
        label    = lbl,
        method   = method   or "GET",
        path     = path     or "/",
        payload  = payload  or "",
        status   = status   or 0,
        severity = severity or "medium",
        detail   = detail   or "",
    }
end

-- Every request ends in exactly one of pass, fail, unknown or skip.
local function pass(r) r.total = r.total + 1; r.passed = r.passed + 1 end

local function fail(r, f)
    r.total = r.total + 1; r.failed = r.failed + 1
    r.findings[#r.findings + 1] = f
end

-- unknown: the target gave no usable answer, so nothing is known about the
-- check. It is its own outcome, never a pass and never a finding.
local function unknown(r, method, path, err)
    r.total = r.total + 1; r.unknown = r.unknown + 1
    if r.first_error == "" then
        r.first_error = string.format("%s %s: %s", method or "GET", path or "/", tostring(err))
    end
end

local function skip(r) r.skipped = r.skipped + 1 end

local function throttle(cfg)
    if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
end

local function status_unexpected(status, expected_statuses)
    if not expected_statuses or #expected_statuses == 0 then return false end
    for _, s in ipairs(expected_statuses) do
        if status == s then return false end
    end
    return true
end

local function body_has_marker(body, markers)
    if not markers or #markers == 0 then return false end
    for _, m in ipairs(markers) do
        if string.find(body, m, 1, true) then return true end
    end
    return false
end

local function inject(template, payload)
    if not template or template == "" then return payload end
    local escaped = string.gsub(payload, "%%", "%%%%")
    return (string.gsub(template, "{{payload}}", escaped))
end

-- ── built-in defaults ──────────────────────────────────────────────────────

local DEFAULT_PROBE_CHECKS = {
    {label="admin-panel",       path="/admin",               expect_status={401,403,404}, severity="high"},
    {label="admin-login",       path="/admin/login",         expect_status={401,403,404}, severity="high"},
    {label="dashboard",         path="/dashboard",           expect_status={401,403,404}, severity="medium"},
    {label="actuator-env",      path="/actuator/env",        expect_status={401,403,404}, severity="high"},
    {label="actuator-dump",     path="/actuator/heapdump",   expect_status={401,403,404}, severity="high"},
    {label="metrics",           path="/metrics",             expect_status={401,403,404}, severity="medium"},
    {label="debug",             path="/debug",               expect_status={401,403,404}, severity="medium"},
    {label="env-file",          path="/.env",                expect_status={401,403,404}, severity="critical"},
    {label="git-config",        path="/.git/config",         expect_status={401,403,404}, severity="critical"},
    {label="swagger-ui",        path="/swagger-ui.html",     expect_status={401,403,404}, severity="low"},
    {label="api-docs",          path="/api-docs",            expect_status={401,403,404}, severity="low"},
    {label="internal",          path="/internal",            expect_status={401,403,404}, severity="high"},
    {label="private",           path="/private",             expect_status={401,403,404}, severity="high"},
    {label="phpinfo",           path="/phpinfo.php",         expect_status={401,403,404}, severity="high"},
    {label="backup",            path="/backup",              expect_status={401,403,404}, severity="high"},
}

-- Each fuzz check belongs to one technique family, so technique selects the
-- payloads that are sent: technique "sqli" sends SQL injection and nothing else.
local DEFAULT_FUZZ_CHECKS = {
    {
        family="sqli", label="sqli-query",  method="GET", path="/?id={{payload}}",
        payloads={"'", "' OR '1'='1", "' OR '1'='1'--", "1; DROP TABLE users--", "' UNION SELECT NULL--"},
        error_markers={"SQL syntax", "mysql_fetch", "ORA-", "syntax error", "Warning: mysql"},
        expect_status={200,301,302,404}, severity="critical",
    },
    {
        family="sqli", label="sqli-body",   method="POST", path="/",
        body='{"id":"{{payload}}"}',
        payloads={"'", "' OR '1'='1", "1; DROP TABLE users--"},
        error_markers={"SQL syntax", "mysql_fetch", "ORA-", "syntax error"},
        expect_status={200,400,404}, severity="critical",
    },
    {
        family="xss", label="xss-reflect",  method="GET", path="/?q={{payload}}",
        payloads={"<script>alert(1)</script>", '"><img src=x onerror=alert(1)>', "javascript:alert(1)"},
        error_markers={"<script>alert", "onerror=alert"},
        expect_status={200,301,302,404}, severity="high",
    },
    {
        family="traversal", label="path-traversal", method="GET", path="/{{payload}}",
        payloads={"../../../etc/passwd", "..%2F..%2F..%2Fetc%2Fpasswd", "....//....//etc/passwd"},
        error_markers={"root:x:", "[boot loader]", "\\[extensions\\]"},
        expect_status={200,301,302,400,404}, severity="critical",
    },
    {
        family="header-injection", label="header-inject",  method="GET", path="/",
        headers={["X-Custom-Header"]="{{payload}}"},
        payloads={"test\r\nX-Injected: evil", "test\nX-Injected: evil"},
        error_markers={"X-Injected"},
        expect_status={200,400,404}, severity="high",
    },
}

local TECHNIQUES = {sqli = true, xss = true, traversal = true, ["header-injection"] = true}

local DEFAULT_AUTH_CHECKS = {
    {label="admin-no-auth",      path="/admin",         severity="critical"},
    {label="admin-users",        path="/admin/users",   severity="critical"},
    {label="api-admin",          path="/api/v1/admin",  severity="critical"},
    {label="actuator-env",       path="/actuator/env",  severity="high"},
    {label="actuator-shutdown",  path="/actuator/shutdown", severity="critical"},
    {label="dashboard",          path="/dashboard",     severity="high"},
    {label="settings",           path="/settings",      severity="medium"},
    {label="api-keys",           path="/api/keys",      severity="critical"},
    {label="internal",           path="/internal",      severity="high"},
}

-- ── probe mode ─────────────────────────────────────────────────────────────

local function run_probe(cfg)
    local r = new_results()
    local checks = cfg.checks
    if not checks or #checks == 0 then checks = DEFAULT_PROBE_CHECKS end
    for _, check in ipairs(checks) do
        local method, path = check.method or "GET", check.path or "/"
        local status, _, _, err = send(cfg, method, path, nil, check.headers)
        r.requests = r.requests + 1
        if err then
            unknown(r, method, path, err)
        elseif status_unexpected(status, check.expect_status) then
            fail(r, finding(cfg, check.label, method, path, "", status, check.severity or "medium",
                "unexpected status " .. status))
        else
            pass(r)
        end
        throttle(cfg)
    end
    return r
end

-- ── fuzz mode ──────────────────────────────────────────────────────────────

local function run_fuzz(cfg)
    local r = new_results()
    local checks = cfg.checks
    if not checks or #checks == 0 then
        checks = {}
        for _, check in ipairs(DEFAULT_FUZZ_CHECKS) do
            if not cfg.technique or cfg.technique == "" or check.family == cfg.technique then
                checks[#checks + 1] = check
            end
        end
    end
    for _, check in ipairs(checks) do
        for _, payload in ipairs(check.payloads or {""}) do
            local path_payload = payload
            if check.path and string.find(check.path, "?", 1, true) then
                -- Query-string slots need a valid URI: percent-encode the raw
                -- payload so lua-resty-http will transmit it at all. The
                -- target still sees the decoded malicious value.
                path_payload = ngx.escape_uri(payload)
            end
            local method    = check.method or "GET"
            local fuzz_path = inject(check.path or "/", path_payload)
            local fuzz_body = inject(check.body or "", payload)
            local fuzz_hdrs = {}
            for k, v in pairs(check.headers or {}) do
                fuzz_hdrs[k] = inject(v, payload)
            end
            local status, body, _, err = send(cfg, method, fuzz_path,
                fuzz_body ~= "" and fuzz_body or nil, fuzz_hdrs)
            if err and string.find(err, "invalid characters", 1, true) then
                -- The scanner's HTTP client refuses to build a request that
                -- carries raw control characters (a literal CRLF in a header
                -- value). The payload never reached the target, so nothing was
                -- learned about it either way: it is skipped, not passed.
                skip(r)
            else
                r.requests = r.requests + 1
                if err then
                    unknown(r, method, fuzz_path, err)
                elseif body_has_marker(body, check.error_markers) then
                    fail(r, finding(cfg, check.label, method, fuzz_path, payload, status,
                        check.severity or "high", "response contains error marker"))
                elseif status_unexpected(status, check.expect_status) then
                    fail(r, finding(cfg, check.label, method, fuzz_path, payload, status,
                        check.severity or "medium", "unexpected status " .. status))
                else
                    pass(r)
                end
            end
            throttle(cfg)
        end
    end
    return r
end

-- ── auth mode ──────────────────────────────────────────────────────────────

local function run_auth(cfg)
    local r = new_results()
    local checks = cfg.checks
    if not checks or #checks == 0 then checks = DEFAULT_AUTH_CHECKS end
    for _, check in ipairs(checks) do
        local method, path = check.method or "GET", check.path or "/"
        local status, _, _, err = send(cfg, method, path, check.body, check.headers or {})
        r.requests = r.requests + 1
        if err then
            unknown(r, method, path, err)
        elseif not status_unexpected(status, {401, 403, 404}) then
            pass(r)
        else
            fail(r, finding(cfg, check.label, method, path, "", status,
                check.severity or "high",
                "endpoint accessible without valid auth (status " .. status .. ")"))
        end
        throttle(cfg)
    end
    return r
end

-- ── flood mode ─────────────────────────────────────────────────────────────

-- A flood is one check, judged on the whole run: with expect_throttle it must
-- see a 429; without, the target must stay available. A target that answers
-- almost nothing proves neither, so it is unknown.
local function run_flood(cfg)
    local r = new_results()
    local path      = cfg.path     or "/"
    local method    = cfg.method   or "GET"
    local rate      = cfg.rate     or 20
    local duration  = cfg.duration or 5
    local throttled = false
    local errors, server_errors, first_error = 0, 0, nil
    local deadline  = ngx.now() + duration

    while ngx.now() < deadline do
        local status, _, _, err = send(cfg, method, path, nil, {})
        r.requests = r.requests + 1
        if err then
            errors = errors + 1
            first_error = first_error or err
        elseif status == 429 then
            throttled = true
        elseif status >= 500 then
            server_errors = server_errors + 1
        end
        local interval = 1.0 / math.max(rate, 1)
        if interval > 0.001 then ngx.sleep(interval) end
    end

    if errors * 2 > r.requests then
        unknown(r, method, path, string.format("%d of %d requests got no answer (%s)",
            errors, r.requests, tostring(first_error)))
    elseif cfg.expect_throttle then
        if throttled then
            pass(r)
        else
            fail(r, finding(cfg, "rate-limit", method, path, "", 0, cfg.severity or "medium",
                "no 429 observed after " .. r.requests .. " requests in " .. duration .. "s"))
        end
    else
        local bad = errors + server_errors
        if r.requests > 0 and bad / r.requests > 0.01 then
            fail(r, finding(cfg, "availability", method, path, "", 0, cfg.severity or "medium",
                string.format("%d of %d requests failed under load", bad, r.requests)))
        else
            pass(r)
        end
    end
    return r
end

-- ── headers mode ───────────────────────────────────────────────────────────

local SECURITY_HEADERS = {
    {name="strict-transport-security", display="Strict-Transport-Security", severity="high",
     validate=function(v) return v ~= "" end,
     detail="HSTS not set; connections may be downgraded to HTTP"},
    {name="x-content-type-options", display="X-Content-Type-Options", severity="medium",
     validate=function(v) return string.lower((string.gsub(v, "%s+", ""))) == "nosniff" end,
     detail="must be 'nosniff' to prevent MIME-type sniffing attacks"},
    {name="x-frame-options", display="X-Frame-Options", severity="medium",
     validate=function(v) local l = string.lower(v); return l == "deny" or string.sub(l,1,10) == "sameorigin" end,
     detail="must be DENY or SAMEORIGIN to prevent clickjacking"},
    {name="content-security-policy", display="Content-Security-Policy", severity="high",
     validate=function(v) return v ~= "" end,
     detail="CSP not set; XSS and injection vectors are unmitigated"},
    {name="referrer-policy", display="Referrer-Policy", severity="low",
     validate=function(v) return v ~= "" end,
     detail="Referrer-Policy absent; referrer headers may leak sensitive paths"},
    {name="permissions-policy", display="Permissions-Policy", severity="low",
     validate=function(v) return v ~= "" end,
     detail="Permissions-Policy absent; browser feature access is unrestricted"},
}

local DEFAULT_HEADER_PATHS = {"/", "/api", "/api/v1"}

local function run_headers(cfg)
    local r = new_results()
    local paths = {}
    for _, check in ipairs(cfg.checks or {}) do paths[#paths + 1] = check.path or "/" end
    if #paths == 0 then paths = DEFAULT_HEADER_PATHS end

    for _, path in ipairs(paths) do
        local status, _, hdrs, err = send(cfg, "GET", path, nil, {})
        r.requests = r.requests + 1
        if err then
            unknown(r, "GET", path, err)
        else
            for _, hdr in ipairs(SECURITY_HEADERS) do
                local val = hdrs[hdr.name] or ""
                if type(val) == "table" then val = table.concat(val, ", ") end
                if val == "" then
                    fail(r, finding(cfg, hdr.display, "GET", path, "", status, hdr.severity, hdr.detail))
                elseif not hdr.validate(val) then
                    fail(r, finding(cfg, hdr.display, "GET", path, "", status, hdr.severity,
                        "weak value '" .. val .. "': " .. hdr.detail))
                else
                    pass(r)
                end
            end
        end
        throttle(cfg)
    end
    return r
end

-- ── verbs mode ──────────────────────────────────────────────────────────────

local DANGEROUS_VERBS  = {"PUT", "DELETE", "TRACE", "TRACK"}
local DEFAULT_VERB_PATHS = {"/admin", "/api/v1/users", "/api/v1/admin", "/settings"}

local function run_verbs(cfg)
    local r = new_results()
    local paths = {}
    for _, check in ipairs(cfg.checks or {}) do paths[#paths + 1] = check.path or "/" end
    if #paths == 0 then paths = DEFAULT_VERB_PATHS end

    for _, path in ipairs(paths) do
        -- The plain GET only sets how serious a bypass is; it is not a check.
        local base_status = send(cfg, "GET", path, nil, {})

        for _, verb in ipairs(DANGEROUS_VERBS) do
            local status, _, _, err = send(cfg, verb, path, nil, {})
            r.requests = r.requests + 1
            if err then
                unknown(r, verb, path, err)
            elseif verb == "TRACE" or verb == "TRACK" then
                if status ~= 405 and status ~= 501 and status ~= 404 then
                    fail(r, finding(cfg, verb .. "-enabled", verb, path, "", status, "medium",
                        verb .. " method enabled; cross-site tracing is possible"))
                else
                    pass(r)
                end
            elseif status >= 200 and status < 300 then
                local sev = "high"
                if base_status and (base_status == 401 or base_status == 403) then
                    sev = "critical"
                end
                fail(r, finding(cfg, verb .. "-bypass", verb, path, "", status, sev,
                    verb .. " returns " .. status .. " on a protected resource"))
            else
                pass(r)
            end
        end
        throttle(cfg)
    end
    return r
end

-- ── graphql mode ────────────────────────────────────────────────────────────

local GRAPHQL_PATHS       = {"/graphql", "/api/graphql", "/query", "/api/query", "/gql", "/api/v1/graphql"}
local GRAPHQL_PROBE_BODY  = '{"query":"{ __schema { queryType { name } } }"}'
local GRAPHQL_CONTENT_HDR = {["Content-Type"] = "application/json"}

-- A path with no GraphQL endpoint (404, 405) is a pass: nothing is exposed
-- there. Only a path that answers with the schema is a finding.
local function run_graphql(cfg)
    local r = new_results()
    local paths = {}
    for _, check in ipairs(cfg.checks or {}) do paths[#paths + 1] = check.path or "/graphql" end
    if #paths == 0 then paths = GRAPHQL_PATHS end

    for _, path in ipairs(paths) do
        local status, body, _, err = send(cfg, "POST", path, GRAPHQL_PROBE_BODY, GRAPHQL_CONTENT_HDR)
        r.requests = r.requests + 1
        if err then
            unknown(r, "POST", path, err)
        elseif string.find(body, "__schema", 1, true) or string.find(body, "queryType", 1, true) then
            fail(r, finding(cfg, "graphql-introspection", "POST", path, "", status, "medium",
                "GraphQL introspection is enabled; schema is publicly accessible"))
        else
            pass(r)
        end
        throttle(cfg)
    end
    return r
end

-- ── cors mode ───────────────────────────────────────────────────────────────

local CORS_TEST_ORIGINS = {"https://evil.com", "null", "https://evil.sub.target.com"}
local DEFAULT_CORS_PATHS = {"/api", "/api/v1", "/"}

local function run_cors(cfg)
    local r = new_results()
    local paths = {}
    for _, check in ipairs(cfg.checks or {}) do paths[#paths + 1] = check.path or "/" end
    if #paths == 0 then paths = DEFAULT_CORS_PATHS end

    for _, path in ipairs(paths) do
        for _, origin in ipairs(CORS_TEST_ORIGINS) do
            local status, _, hdrs, err = send(cfg, "GET", path, nil, {["Origin"] = origin})
            r.requests = r.requests + 1
            if err then
                unknown(r, "GET", path, err)
            else
                local allow = hdrs["access-control-allow-origin"] or ""
                local creds = string.lower(hdrs["access-control-allow-credentials"] or "")
                if allow == "*" and creds == "true" then
                    fail(r, finding(cfg, "cors-wildcard-credentials", "GET", path, origin, status, "critical",
                        "Access-Control-Allow-Origin: * with Allow-Credentials: true"))
                elseif allow == origin and origin ~= "" then
                    fail(r, finding(cfg, "cors-reflect-origin", "GET", path, origin, status, "high",
                        "server reflects arbitrary Origin '" .. origin .. "' verbatim"))
                elseif allow == "*" then
                    fail(r, finding(cfg, "cors-wildcard", "GET", path, origin, status, "medium",
                        "Access-Control-Allow-Origin: * permits any cross-origin caller"))
                else
                    pass(r)
                end
            end
            throttle(cfg)
        end
    end
    return r
end

-- ── plugin entry point ─────────────────────────────────────────────────────

local MODES = {
    probe = run_probe, fuzz = run_fuzz, auth = run_auth, flood = run_flood,
    headers = run_headers, verbs = run_verbs, graphql = run_graphql, cors = run_cors,
}

local _M = {}
_M.version  = 0.2
_M.priority = 11
_M.name     = "babelsuite-attack-scanner"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function reply(status, body)
    ngx.status = status
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(body))
    return ngx.exit(status)
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/attack/start" then
        return ngx.exit(404)
    end
    if ngx.req.get_method() ~= "POST" then
        return ngx.exit(405)
    end

    ngx.req.read_body()
    local ok, cfg = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok or type(cfg) ~= "table" then
        return reply(400, {error = "invalid JSON"})
    end
    if type(cfg.target) ~= "string" or cfg.target == "" then
        return reply(400, {error = "target is required"})
    end

    local mode = cfg.mode or "probe"
    local run = MODES[mode]
    if not run then
        return reply(400, {error = "unknown mode " .. tostring(mode) .. "; supported: probe, fuzz, auth, flood, headers, verbs, graphql, cors"})
    end
    if cfg.technique and cfg.technique ~= "" and not TECHNIQUES[cfg.technique] then
        return reply(400, {error = "unknown technique " .. tostring(cfg.technique) .. "; supported: sqli, xss, traversal, header-injection"})
    end

    local results = run(cfg)
    results.findings = as_array(results.findings)
    return reply(200, results)
end

return _M
`
