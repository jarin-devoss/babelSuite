local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, not [], so a step that found
-- nothing — the passing case — would otherwise emit a findings object.
local function as_array(t) return #t == 0 and json.empty_array or t end

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "canary-validator"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- sample sends sample_size requests and records, for each answer, whether it
-- carried the canary header and which status it returned. A request that
-- failed outright is recorded with status 0 so error rates still count it.
local function sample(target, canary_header, sample_size, headers)
    local responses = {}
    for _ = 1, sample_size do
        local httpc = http.new()
        httpc:set_timeout(5000)
        local res, err = httpc:request_uri(target, {method = "GET", headers = headers})
        if not err and res then
            local h = res.headers or {}
            responses[#responses + 1] = {
                canary = (h[canary_header] or h[canary_header:lower()]) ~= nil,
                status = res.status,
            }
        else
            responses[#responses + 1] = {canary = false, status = 0}
        end
        ngx.sleep(0.01)
    end
    return responses
end

local function canary_ratio(responses, sample_size)
    local hits = 0
    for _, r in ipairs(responses) do
        if r.canary then hits = hits + 1 end
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

    local actual   = canary_ratio(sample(target, header, sample_size), sample_size)
    local findings = check_ratio(actual, expected, tolerance, severity)
    return {passed = #findings == 0, findings = as_array(findings),
            actual_ratio = actual, sample_size = sample_size}, nil
end

local function run_watch_ratio(cfg)
    local target      = cfg.target or ""
    local header      = cfg.canary_header or "X-Canary"
    local expected    = tonumber(cfg.expected_ratio) or 0.1
    local tolerance   = tonumber(cfg.tolerance) or 0.15  -- looser tolerance for monitoring
    local sample_size = tonumber(cfg.sample_size) or 100

    local actual   = canary_ratio(sample(target, header, sample_size), sample_size)
    local findings = check_ratio(actual, expected, tolerance, "warn")
    return {passed = #findings == 0, findings = as_array(findings),
            actual_ratio = actual, sample_size = sample_size}, nil
end

-- sticky: every request carries the same session key, so a router that pins
-- sessions must send all of them to one side. A mix means a user would bounce
-- between the canary and stable versions mid-session.
local function run_sticky(cfg)
    local target         = cfg.target or ""
    local header         = cfg.canary_header or "X-Canary"
    local session_header = cfg.session_header or "X-Session-Id"
    local session_value  = cfg.session_value or "babelsuite-sticky-check"
    local sample_size    = tonumber(cfg.sample_size) or 20
    local severity       = cfg.severity or "critical"

    local responses = sample(target, header, sample_size, {[session_header] = session_value})
    local canary, stable = 0, 0
    for _, r in ipairs(responses) do
        if r.status ~= 0 then
            if r.canary then canary = canary + 1 else stable = stable + 1 end
        end
    end

    local findings = {}
    if canary > 0 and stable > 0 then
        table.insert(findings, {
            label    = "canary.sticky.split",
            severity = severity,
            detail   = string.format("one session (%s=%s) reached canary %d times and stable %d times",
                session_header, session_value, canary, stable),
        })
    end
    return {passed = #findings == 0, findings = as_array(findings),
            canary_hits = canary, stable_hits = stable, sample_size = sample_size}, nil
end

-- compare_errors: splits the sample by which side answered and compares the
-- server-error rate of each. A canary is only safe to promote when it fails no
-- more often than the version it replaces.
local function run_compare_errors(cfg)
    local target      = cfg.target or ""
    local header      = cfg.canary_header or "X-Canary"
    local sample_size = tonumber(cfg.sample_size) or 100
    local max_delta   = tonumber(cfg.max_error_delta) or 0.01
    local severity    = cfg.severity or "critical"

    local responses = sample(target, header, sample_size)
    local counts = {canary = {n = 0, errors = 0}, stable = {n = 0, errors = 0}}
    for _, r in ipairs(responses) do
        local side = r.canary and counts.canary or counts.stable
        side.n = side.n + 1
        if r.status == 0 or r.status >= 500 then side.errors = side.errors + 1 end
    end

    local function rate(side) return side.n > 0 and side.errors / side.n or 0 end
    local canary_rate, stable_rate = rate(counts.canary), rate(counts.stable)

    local findings = {}
    if counts.canary.n == 0 then
        table.insert(findings, {
            label    = "canary.errors.no_canary_traffic",
            severity = severity,
            detail   = string.format("none of %d responses carried %s, so the canary was never measured",
                sample_size, header),
        })
    elseif canary_rate - stable_rate > max_delta then
        table.insert(findings, {
            label    = "canary.errors.regression",
            severity = severity,
            detail   = string.format("canary error rate %.3f vs stable %.3f exceeds allowed delta %.3f",
                canary_rate, stable_rate, max_delta),
        })
    end
    return {passed = #findings == 0, findings = as_array(findings),
            canary_error_rate = canary_rate, stable_error_rate = stable_rate,
            canary_samples = counts.canary.n, stable_samples = counts.stable.n}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
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
    elseif op == "sticky" then
        result, err = run_sticky(cfg)
    elseif op == "compare_errors" then
        result, err = run_compare_errors(cfg)
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
