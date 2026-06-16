local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "canary-validator"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function sample_canary_ratio(target, canary_header, sample_size)
    local hits = 0
    for _ = 1, sample_size do
        local httpc = http.new()
        httpc:set_timeout(5000)
        local res, err = httpc:request_uri(target, {method = "GET"})
        if not err and res then
            local h = res.headers or {}
            if h[canary_header] or h[canary_header:lower()] then
                hits = hits + 1
            end
        end
        ngx.sleep(0.01)
    end
    return hits / sample_size
end

local function check_ratio(actual, expected, tolerance, severity)
    local diff = math.abs(actual - expected)
    if diff <= tolerance then return {} end
    return {{
        label    = "canary.ratio.mismatch",
        severity = severity,
        detail   = string.format("expected=%.2f actual=%.2f diff=%.2f tolerance=%.2f",
            expected, actual, diff, tolerance),
    }}
end

local function run_validate(cfg)
    local target       = cfg.target or ""
    local header       = cfg.canary_header or "X-Canary"
    local expected     = tonumber(cfg.expected_ratio) or 0.1
    local tolerance    = tonumber(cfg.tolerance) or 0.05
    local sample_size  = tonumber(cfg.sample_size) or 100
    local severity     = cfg.severity or "critical"

    local actual   = sample_canary_ratio(target, header, sample_size)
    local findings = check_ratio(actual, expected, tolerance, severity)
    return {passed = #findings == 0, findings = findings,
            actual_ratio = actual, sample_size = sample_size}, nil
end

local function run_watch_ratio(cfg)
    local target      = cfg.target or ""
    local header      = cfg.canary_header or "X-Canary"
    local expected    = tonumber(cfg.expected_ratio) or 0.1
    local tolerance   = tonumber(cfg.tolerance) or 0.15  -- looser tolerance for monitoring
    local sample_size = tonumber(cfg.sample_size) or 100

    local actual   = sample_canary_ratio(target, header, sample_size)
    local findings = check_ratio(actual, expected, tolerance, "warn")
    return {passed = #findings == 0, findings = findings,
            actual_ratio = actual, sample_size = sample_size}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/canary-validator/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "validate"
    local cfg = payload.config or {}

    if (cfg.target or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "target is required"})); return ngx.exit(400)
    end

    local result, err
    if op == "watch_ratio" then
        result, err = run_watch_ratio(cfg)
    else
        result, err = run_validate(cfg)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
