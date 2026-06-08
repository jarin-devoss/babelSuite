local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
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
    return {passed = #findings == 0, findings = findings}, nil
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
    return {passed = #findings == 0, findings = findings}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/pii-scanner/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "scan"
    local cfg = payload.config or {}

    if (cfg.target or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "target is required"})); return ngx.exit(400)
    end

    local result, err
    if op == "probe" then
        result, err = run_probe(cfg)
    else
        result, err = run_scan(cfg)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
