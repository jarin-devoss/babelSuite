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
_M.name     = "canary-validator"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- sample sends sample_size requests and records, for each answer, whether it
-- carried the canary header and which status it returned. A request that
-- failed outright has no answer to classify: it is recorded as unknown (status
-- 0), counted apart from both sides, and never read as a stable response.
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
            responses[#responses + 1] = {unknown = true, status = 0, error = tostring(err)}
        end
        ngx.sleep(0.01)
    end
    return responses
end

-- answered: the responses that got an answer, and how many did not.
local function answered(responses)
    local ok, unknown, first_error = {}, 0, nil
    for _, r in ipairs(responses) do
        if r.unknown then
            unknown = unknown + 1
            first_error = first_error or r.error
        else
            ok[#ok + 1] = r
        end
    end
    return ok, unknown, first_error
end

local function unreachable(target, sample_size, first_error)
    return string.format("target %s is unreachable: %d of %d requests failed (%s)",
        target, sample_size, sample_size, tostring(first_error))
end

-- canary_ratio is the share of answered responses that came from the canary. A
-- request that never got an answer is not a stable one, so it does not dilute
-- the ratio.
local function canary_ratio(responses)
    local hits = 0
    for _, r in ipairs(responses) do
        if r.canary then hits = hits + 1 end
    end
    return hits / #responses
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

-- ratio_result: validate and watch_ratio differ only in tolerance and severity.
local function ratio_result(cfg, tolerance, severity)
    local target      = cfg.target or ""
    local header      = cfg.canary_header or "X-Canary"
    local expected    = tonumber(cfg.expected_ratio) or 0.1
    local sample_size = tonumber(cfg.sample_size) or 100

    local ok, unknown, first_error = answered(sample(target, header, sample_size))
    if #ok == 0 then return nil, unreachable(target, sample_size, first_error) end

    local findings = check_ratio(canary_ratio(ok), expected, tolerance, severity)
    if unknown > 0 then
        table.insert(findings, {
            label    = "canary.requests.failed",
            severity = "warn",
            detail   = string.format("%d of %d requests got no answer and were left out of the ratio", unknown, sample_size),
        })
    end
    return {passed = verdict(findings), findings = as_array(findings),
            actual_ratio = canary_ratio(ok), sample_size = sample_size, unanswered = unknown}, nil
end

local function run_validate(cfg)
    return ratio_result(cfg, tonumber(cfg.tolerance) or 0.05, cfg.severity or "critical")
end

-- watch_ratio: a wider default tolerance, and always warn. It reports, and
-- never blocks.
local function run_watch_ratio(cfg)
    return ratio_result(cfg, tonumber(cfg.tolerance) or 0.15, "warn")
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

    local ok, _, first_error = answered(sample(target, header, sample_size, {[session_header] = session_value}))
    if #ok == 0 then return nil, unreachable(target, sample_size, first_error) end

    local canary, stable = 0, 0
    for _, r in ipairs(ok) do
        if r.canary then canary = canary + 1 else stable = stable + 1 end
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
    return {passed = verdict(findings), findings = as_array(findings),
            canary_hits = canary, stable_hits = stable, sample_size = sample_size}, nil
end

-- compare_errors: splits the sample by which side answered and compares the
-- server-error rate of each. A canary is only safe to promote when it fails no
-- more often than the version it replaces. A request that got no answer cannot
-- be assigned to either side, so it is counted on its own: a canary that drops
-- its own connections leaves no header behind, and counting those as stable
-- hides exactly the failure this check exists to find.
local function run_compare_errors(cfg)
    local target      = cfg.target or ""
    local header      = cfg.canary_header or "X-Canary"
    local sample_size = tonumber(cfg.sample_size) or 100
    local max_delta   = tonumber(cfg.max_error_delta) or 0.01
    local severity    = cfg.severity or "critical"

    local ok, unknown, first_error = answered(sample(target, header, sample_size))
    if #ok == 0 then return nil, unreachable(target, sample_size, first_error) end

    local counts = {canary = {n = 0, errors = 0}, stable = {n = 0, errors = 0}}
    for _, r in ipairs(ok) do
        local side = r.canary and counts.canary or counts.stable
        side.n = side.n + 1
        if r.status >= 500 then side.errors = side.errors + 1 end
    end

    local function rate(side) return side.n > 0 and side.errors / side.n or 0 end
    local canary_rate, stable_rate = rate(counts.canary), rate(counts.stable)
    local unknown_rate = unknown / sample_size

    local findings = {}
    if counts.canary.n == 0 then
        table.insert(findings, {
            label    = "canary.errors.no_canary_traffic",
            severity = severity,
            detail   = string.format("none of %d answered responses carried %s, so the canary was never measured",
                #ok, header),
        })
    elseif canary_rate - stable_rate > max_delta then
        table.insert(findings, {
            label    = "canary.errors.regression",
            severity = severity,
            detail   = string.format("canary error rate %.3f vs stable %.3f exceeds allowed delta %.3f",
                canary_rate, stable_rate, max_delta),
        })
    end
    if unknown_rate > max_delta then
        table.insert(findings, {
            label    = "canary.errors.unanswered",
            severity = severity,
            detail   = string.format("%d of %d requests got no answer (%.3f of the sample, allowed %.3f); they cannot be assigned to the canary or to stable",
                unknown, sample_size, unknown_rate, max_delta),
        })
    end
    return {passed = verdict(findings), findings = as_array(findings),
            canary_error_rate = canary_rate, stable_error_rate = stable_rate,
            canary_samples = counts.canary.n, stable_samples = counts.stable.n,
            unanswered = unknown}, nil
end

local OPS = {
    validate       = run_validate,
    watch_ratio    = run_watch_ratio,
    sticky         = run_sticky,
    compare_errors = run_compare_errors,
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
    if not ok then return reply(400, {error = "invalid json"}) end

    local op  = payload.op or "validate"
    local cfg = payload.config or {}

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: validate, watch_ratio, sticky, compare_errors"})
    end
    if (cfg.target or "") == "" then return reply(400, {error = "target is required"}) end

    local result, err = handler(cfg)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
