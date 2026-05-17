package apisix

import (
	"fmt"
	"os"
	"path/filepath"
)

// AttackScannerPluginName is the APISIX plugin name registered in apisix.yaml.
const AttackScannerPluginName = "babelsuite-attack-scanner"

// AttackScannerTriggerRoute is the URI the BabelSuite runner POSTs to.
const AttackScannerTriggerRoute = "/_babelsuite/attack/start"

// LuaPluginMountPath is the container path where custom Lua plugin files must
// be placed so APISIX can load them by name.
const LuaPluginMountPath = "/usr/local/openresty/site/lualib/apisix/plugins"

// WriteLuaPluginFiles writes the embedded Lua plugin sources to destDir so they
// can be bind-mounted into the APISIX sidecar container before it starts.
// Returns an error if any file cannot be written.
func WriteLuaPluginFiles(destDir string) error {
	plugins := map[string]string{
		TrafficCannonPluginName + ".lua": TrafficCannonLua,
		AttackScannerPluginName + ".lua": AttackScannerLua,
	}
	for name, code := range plugins {
		p := filepath.Join(destDir, name)
		if err := os.WriteFile(p, []byte(code), 0600); err != nil {
			return fmt.Errorf("write lua plugin %s: %w", name, err)
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
//   - flood:   Verifies rate-limiting (HTTP 429) under sustained traffic.
//   - headers: Audits security response headers (HSTS, CSP, X-Frame, …).
//   - verbs:   Probes dangerous HTTP methods (PUT, DELETE, TRACE, TRACK).
//   - graphql: Detects exposed GraphQL introspection endpoints.
//   - cors:    Finds CORS misconfigurations (reflected origin, wildcard).
//
// All modes POST to /_babelsuite/attack/start and return a synchronous JSON
// findings report once the run completes:
//
//	{"total":N,"passed":N,"failed":N,"findings":[{...}]}
const AttackScannerLua = `
local json     = require("cjson")
local http_lib = require("resty.http")

-- ── helpers ────────────────────────────────────────────────────────────────

local function send(target, method, path, body, headers, timeout_ms)
    local httpc = http_lib.new()
    httpc:set_timeout(timeout_ms or 10000)
    local res, err = httpc:request_uri(target .. path, {
        method  = method  or "GET",
        body    = body    or nil,
        headers = headers or {},
    })
    if err or not res then
        return nil, nil, err or "no response"
    end
    return res.status, res.body or "", nil
end

-- send_full also returns normalized (lowercase-keyed) response headers.
local function send_full(target, method, path, body, headers, timeout_ms)
    local httpc = http_lib.new()
    httpc:set_timeout(timeout_ms or 10000)
    local res, err = httpc:request_uri(target .. path, {
        method  = method  or "GET",
        body    = body    or nil,
        headers = headers or {},
    })
    if err or not res then
        return nil, nil, {}, err or "no response"
    end
    local norm = {}
    for k, v in pairs(res.headers or {}) do norm[string.lower(k)] = v end
    return res.status, res.body or "", norm, nil
end

local _technique = ""

local function finding(label, method, path, payload, status, severity, detail)
    local lbl = label or path
    if _technique ~= "" then lbl = _technique .. "/" .. lbl end
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

local function new_results()
    return {total = 0, passed = 0, failed = 0, findings = {}}
end

local function pass(r)  r.total = r.total + 1; r.passed = r.passed + 1 end
local function fail(r, f) r.total = r.total + 1; r.failed = r.failed + 1; r.findings[#r.findings + 1] = f end

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
    return (string.gsub(template, "{{payload}}", payload))
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

local DEFAULT_FUZZ_CHECKS = {
    {
        label="sqli-query",  method="GET", path="/?id={{payload}}",
        payloads={"'", "' OR '1'='1", "' OR '1'='1'--", "1; DROP TABLE users--", "' UNION SELECT NULL--"},
        error_markers={"SQL syntax", "mysql_fetch", "ORA-", "syntax error", "Warning: mysql"},
        expect_status={200,301,302}, severity="critical",
    },
    {
        label="sqli-body",   method="POST", path="/",
        body='{"id":"{{payload}}"}',
        payloads={"'", "' OR '1'='1", "1; DROP TABLE users--"},
        error_markers={"SQL syntax", "mysql_fetch", "ORA-", "syntax error"},
        expect_status={200,400}, severity="critical",
    },
    {
        label="xss-reflect",  method="GET", path="/?q={{payload}}",
        payloads={"<script>alert(1)</script>", '"><img src=x onerror=alert(1)>', "javascript:alert(1)"},
        error_markers={"<script>alert", "onerror=alert"},
        expect_status={200,301,302}, severity="high",
    },
    {
        label="path-traversal", method="GET", path="/{{payload}}",
        payloads={"../../../etc/passwd", "..%2F..%2F..%2Fetc%2Fpasswd", "....//....//etc/passwd"},
        error_markers={"root:x:", "[boot loader]", "\\[extensions\\]"},
        expect_status={200,301,302}, severity="critical",
    },
    {
        label="header-inject",  method="GET", path="/",
        headers={["X-Custom-Header"]="{{payload}}"},
        payloads={"test\r\nX-Injected: evil", "test\nX-Injected: evil"},
        error_markers={"X-Injected"},
        expect_status={200,400}, severity="high",
    },
}

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
        local status, _, err = send(cfg.target, check.method or "GET", check.path or "/",
            nil, check.headers, cfg.timeout_ms)
        if err then
            fail(r, finding(check.label, check.method, check.path, "", 0, check.severity or "medium",
                "request failed: " .. err))
        elseif status_unexpected(status, check.expect_status) then
            fail(r, finding(check.label, check.method, check.path, "", status, check.severity or "medium",
                "unexpected status " .. status))
        else
            pass(r)
        end
        if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
    end
    return r
end

-- ── fuzz mode ──────────────────────────────────────────────────────────────

local function run_fuzz(cfg)
    local r = new_results()
    local checks = cfg.checks
    if not checks or #checks == 0 then checks = DEFAULT_FUZZ_CHECKS end
    for _, check in ipairs(checks) do
        for _, payload in ipairs(check.payloads or {""}) do
            local fuzz_path = inject(check.path or "/", payload)
            local fuzz_body = inject(check.body or "", payload)
            local fuzz_hdrs = {}
            for k, v in pairs(check.headers or {}) do
                fuzz_hdrs[k] = inject(v, payload)
            end
            local status, body, err = send(cfg.target, check.method or "GET", fuzz_path,
                fuzz_body ~= "" and fuzz_body or nil, fuzz_hdrs, cfg.timeout_ms)
            if err then
                fail(r, finding(check.label, check.method, fuzz_path, payload, 0,
                    check.severity or "medium", "request failed: " .. err))
            elseif body_has_marker(body, check.error_markers) then
                fail(r, finding(check.label, check.method, fuzz_path, payload, status,
                    check.severity or "high", "response contains error marker"))
            elseif status_unexpected(status, check.expect_status) then
                fail(r, finding(check.label, check.method, fuzz_path, payload, status,
                    check.severity or "medium", "unexpected status " .. status))
            else
                pass(r)
            end
            if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
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
        local hdrs = check.headers or {}
        local status, _, err = send(cfg.target, check.method or "GET", check.path or "/",
            check.body, hdrs, cfg.timeout_ms)
        if err then
            fail(r, finding(check.label, check.method, check.path, "", 0,
                check.severity or "high", "request failed: " .. err))
        elseif not status_unexpected(status, {401, 403}) then
            pass(r)
        else
            fail(r, finding(check.label, check.method, check.path, "", status,
                check.severity or "high",
                "endpoint accessible without valid auth (status " .. status .. ")"))
        end
        if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
    end
    return r
end

-- ── flood mode ─────────────────────────────────────────────────────────────

local function run_flood(cfg)
    local r = new_results()
    local path      = cfg.path     or "/"
    local method    = cfg.method   or "GET"
    local rate      = cfg.rate     or 20
    local duration  = cfg.duration or 5
    local throttled = false
    local deadline  = ngx.now() + duration

    while ngx.now() < deadline do
        local status, _, err = send(cfg.target, method, path, nil, {}, cfg.timeout_ms)
        r.total = r.total + 1
        if err then
            -- connection refused can mean the service gave up under load
        elseif status == 429 then
            throttled = true
        end
        local interval = 1.0 / math.max(rate, 1)
        if interval > 0.001 then ngx.sleep(interval) end
    end

    if cfg.expect_throttle then
        if throttled then
            pass(r)
        else
            fail(r, finding("rate-limit", method, path, "", 0, cfg.severity or "medium",
                "no 429 observed after " .. r.total .. " requests in " .. duration .. "s"))
        end
    else
        pass(r)
    end

    return r
end

-- ── headers mode ───────────────────────────────────────────────────────────

local SECURITY_HEADERS = {
    {name="strict-transport-security", display="Strict-Transport-Security", severity="high",
     validate=function(v) return v ~= "" end,
     detail="HSTS not set; connections may be downgraded to HTTP"},
    {name="x-content-type-options", display="X-Content-Type-Options", severity="medium",
     validate=function(v) return string.lower(string.gsub(v, "%s+", "")) == "nosniff" end,
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
        local status, _, hdrs, err = send_full(cfg.target, "GET", path, nil, {}, cfg.timeout_ms)
        if err then
            fail(r, finding("headers-check", "GET", path, "", 0, "medium", "request failed: " .. err))
        else
            for _, hdr in ipairs(SECURITY_HEADERS) do
                local val = hdrs[hdr.name] or ""
                if val == "" then
                    fail(r, finding(hdr.display, "GET", path, "", status, hdr.severity, hdr.detail))
                elseif not hdr.validate(val) then
                    fail(r, finding(hdr.display, "GET", path, "", status, hdr.severity,
                        "weak value '" .. val .. "': " .. hdr.detail))
                else
                    pass(r)
                end
            end
        end
        if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
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
        local base_status, _, _, _ = send_full(cfg.target, "GET", path, nil, {}, cfg.timeout_ms)

        for _, verb in ipairs(DANGEROUS_VERBS) do
            local status, _, _, err = send_full(cfg.target, verb, path, nil, {}, cfg.timeout_ms)
            if not err and status then
                if verb == "TRACE" or verb == "TRACK" then
                    if status ~= 405 and status ~= 501 and status ~= 404 then
                        fail(r, finding(verb .. "-enabled", verb, path, "", status, "medium",
                            verb .. " method enabled; cross-site tracing is possible"))
                    else
                        pass(r)
                    end
                elseif status >= 200 and status < 300 then
                    local sev = "high"
                    if base_status and (base_status == 401 or base_status == 403) then
                        sev = "critical"
                    end
                    fail(r, finding(verb .. "-bypass", verb, path, "", status, sev,
                        verb .. " returns " .. status .. " on a protected resource"))
                else
                    pass(r)
                end
            end
        end
        if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
    end
    return r
end

-- ── graphql mode ────────────────────────────────────────────────────────────

local GRAPHQL_PATHS       = {"/graphql", "/api/graphql", "/query", "/api/query", "/gql", "/api/v1/graphql"}
local GRAPHQL_PROBE_BODY  = '{"query":"{ __schema { queryType { name } } }"}'
local GRAPHQL_CONTENT_HDR = {["Content-Type"] = "application/json"}

local function run_graphql(cfg)
    local r = new_results()
    local paths = {}
    for _, check in ipairs(cfg.checks or {}) do paths[#paths + 1] = check.path or "/graphql" end
    if #paths == 0 then paths = GRAPHQL_PATHS end

    for _, path in ipairs(paths) do
        local status, body, _, err = send_full(cfg.target, "POST", path, GRAPHQL_PROBE_BODY,
            GRAPHQL_CONTENT_HDR, cfg.timeout_ms)
        if not err and status and status ~= 404 and status ~= 405 then
            if string.find(body, "__schema", 1, true) or string.find(body, "queryType", 1, true) then
                fail(r, finding("graphql-introspection", "POST", path, "", status, "medium",
                    "GraphQL introspection is enabled; schema is publicly accessible"))
            elseif status >= 200 and status < 300 then
                pass(r)
            end
        end
        if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
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
            local status, _, hdrs, err = send_full(cfg.target, "GET", path, nil,
                {["Origin"] = origin}, cfg.timeout_ms)
            if not err and status then
                local allow = hdrs["access-control-allow-origin"] or ""
                local creds = string.lower(hdrs["access-control-allow-credentials"] or "")
                if allow == "*" and creds == "true" then
                    fail(r, finding("cors-wildcard-credentials", "GET", path, origin, status, "critical",
                        "Access-Control-Allow-Origin: * with Allow-Credentials: true"))
                elseif allow == origin and origin ~= "" then
                    fail(r, finding("cors-reflect-origin", "GET", path, origin, status, "high",
                        "server reflects arbitrary Origin '" .. origin .. "' verbatim"))
                elseif allow == "*" then
                    fail(r, finding("cors-wildcard", "GET", path, origin, status, "medium",
                        "Access-Control-Allow-Origin: * permits any cross-origin caller"))
                else
                    pass(r)
                end
            end
            if cfg.rate_limit and cfg.rate_limit > 0 then ngx.sleep(1.0 / cfg.rate_limit) end
        end
    end
    return r
end

-- ── plugin entry point ─────────────────────────────────────────────────────

local _M = {}
_M.version  = 0.1
_M.priority = 11
_M.name     = "babelsuite-attack-scanner"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

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
        ngx.status = 400
        ngx.say(json.encode({error = "invalid JSON"}))
        return ngx.exit(400)
    end

    _technique = cfg.technique or ""

    local results
    local mode = cfg.mode or "probe"
    if mode == "fuzz" then
        results = run_fuzz(cfg)
    elseif mode == "auth" then
        results = run_auth(cfg)
    elseif mode == "flood" then
        results = run_flood(cfg)
    elseif mode == "headers" then
        results = run_headers(cfg)
    elseif mode == "verbs" then
        results = run_verbs(cfg)
    elseif mode == "graphql" then
        results = run_graphql(cfg)
    elseif mode == "cors" then
        results = run_cors(cfg)
    else
        results = run_probe(cfg)
    end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(results))
    return ngx.exit(200)
end

return _M
`
