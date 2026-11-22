local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, not [], so a step that found
-- nothing — the passing case — would otherwise emit a findings object.
local function as_array(t) return #t == 0 and json.empty_array or t end

-- A finding blocks the step when its severity is critical (high and error mean
-- the same). warn and info are advisory. The verdict is computed here, once, so
-- a severity setting decides whether a check gates, whatever the operation.
local BLOCKING = {critical = true, high = true, error = true}
local function verdict(findings)
    for _, f in ipairs(findings) do
        if BLOCKING[f.severity] then return false end
    end
    return true
end

local _M = {}
_M.version  = 0.2
_M.priority = 10
_M.name     = "pii-scanner"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local DEFAULT_PATTERNS = {
    {name = "credit_card", pattern = "%d%d%d%d[%- ]%d%d%d%d[%- ]%d%d%d%d[%- ]%d%d%d%d"},
    {name = "ssn",         pattern = "%d%d%d%-%d%d%-%d%d%d%d"},
    {name = "email",       pattern = "[a-zA-Z0-9._%+%-]+@[a-zA-Z0-9%.%-]+%.[a-zA-Z][a-zA-Z]+"},
    {name = "private_key", pattern = "%-%-%-%-%-BEGIN"},
}

local function fetch_body(target)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(target, {method = "GET"})
    if err then return nil, "upstream failed: " .. err end
    return res.body or "", nil
end

local function match_patterns(body, extra_patterns)
    local active = {}
    for _, p in ipairs(DEFAULT_PATTERNS) do active[#active + 1] = p end
    for _, raw in ipairs(extra_patterns or {}) do
        active[#active + 1] = {name = raw, pattern = raw}
    end
    local hits = {}
    for _, p in ipairs(active) do
        if body:find(p.pattern) then
            hits[#hits + 1] = p.name
        end
    end
    return hits
end

local function run_scan(cfg)
    local body, err = fetch_body(cfg.target or "")
    if err then return nil, err end
    local hits     = match_patterns(body, cfg.patterns)
    local severity = cfg.severity or "critical"
    local findings = {}
    for _, name in ipairs(hits) do
        table.insert(findings, {
            label    = "pii." .. name,
            severity = severity,
            detail   = "PII pattern '" .. name .. "' found in response body",
        })
    end
    return {passed = verdict(findings), findings = as_array(findings)}, nil
end

local function run_probe(cfg)
    local body, err = fetch_body(cfg.target or "")
    if err then return nil, err end
    local hits     = match_patterns(body, cfg.patterns)
    -- probe: always warn, passive observation only
    local findings = {}
    for _, name in ipairs(hits) do
        table.insert(findings, {
            label    = "pii." .. name,
            severity = "warn",
            detail   = "PII pattern '" .. name .. "' found in response body",
        })
    end
    return {passed = verdict(findings), findings = as_array(findings)}, nil
end

-- scan_headers: PII leaks through headers as often as bodies — an email in a
-- Set-Cookie, a card number echoed in a debug header, a token in Location.
-- Each header is scanned on its own so the finding names where the leak is.
local function run_scan_headers(cfg)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(cfg.target or "", {method = "GET"})
    if err then return nil, "upstream failed: " .. err end

    local severity = cfg.severity or "critical"
    local findings, scanned = {}, 0
    for header, value in pairs(res.headers or {}) do
        -- A repeated header, such as several Set-Cookie lines, arrives as a list.
        local text = type(value) == "table" and table.concat(value, "\n") or tostring(value)
        scanned = scanned + 1
        for _, name in ipairs(match_patterns(text, cfg.patterns)) do
            table.insert(findings, {
                label    = "pii." .. name,
                severity = severity,
                detail   = "PII pattern '" .. name .. "' found in response header " .. header,
            })
        end
    end
    return {passed = verdict(findings), findings = as_array(findings), headers_scanned = scanned}, nil
end

-- scan_many: one step over a list of endpoints, so a suite can sweep every
-- read path of an API instead of declaring a scan per URL.
local function run_scan_many(cfg)
    local severity = cfg.severity or "critical"
    local findings = {}
    for _, target in ipairs(cfg.targets or {}) do
        local body, err = fetch_body(target)
        if err then
            table.insert(findings, {
                label    = "pii.unreachable",
                severity = "critical",
                detail   = target .. " — " .. err,
            })
        else
            for _, name in ipairs(match_patterns(body, cfg.patterns)) do
                table.insert(findings, {
                    label    = "pii." .. name,
                    severity = severity,
                    detail   = "PII pattern '" .. name .. "' found in response body of " .. target,
                })
            end
        end
    end
    local leaks = 0
    for _, f in ipairs(findings) do
        if f.label ~= "pii.unreachable" then leaks = leaks + 1 end
    end
    return {passed = verdict(findings), findings = as_array(findings),
            summary = string.format("scan_many: %d targets, %d leaks", #(cfg.targets or {}), leaks)}, nil
end

local OPS = {
    scan         = run_scan,
    probe        = run_probe,
    scan_headers = run_scan_headers,
    scan_many    = run_scan_many,
}

local function reply(status, body)
    ngx.status = status
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(body))
    return ngx.exit(status)
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "scan"
    local cfg = payload.config or {}

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: scan, probe, scan_headers, scan_many"})
    end

    if op == "scan_many" then
        if type(cfg.targets) ~= "table" or #cfg.targets == 0 then
            ngx.status = 400; ngx.say(json.encode({error = "targets must be a non-empty list"})); return ngx.exit(400)
        end
    elseif (cfg.target or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "target is required"})); return ngx.exit(400)
    end

    local result, err = handler(cfg)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
