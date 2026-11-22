local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, which the runner cannot decode into
-- a findings array — so two identical responses, the whole point of a passing
-- shadow comparison, is exactly the case that breaks without this.
local function as_array(t) return #t == 0 and json.empty_array or t end

-- A finding blocks the step when its severity is critical (high and error mean
-- the same). warn and info are advisory. threshold is how many blocking
-- findings a run may have and still pass.
local BLOCKING = {critical = true, high = true, error = true}
local function blocking_count(findings)
    local n = 0
    for _, f in ipairs(findings) do
        if BLOCKING[f.severity] then n = n + 1 end
    end
    return n
end

local _M = {}
_M.version  = 0.2
_M.priority = 10
_M.name     = "shadow-diff"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function fetch(url)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {method = "GET"})
    if not res then return nil, nil, tostring(err) end
    return res.status, res.body or "", nil
end

-- is_array: cjson decodes both JSON arrays and objects to Lua tables. A table
-- whose keys are exactly 1..n is an array.
local function is_array(t)
    local n = 0
    for _ in pairs(t) do n = n + 1 end
    for i = 1, n do
        if t[i] == nil then return false end
    end
    return true
end

local function show(v)
    if v == nil then return "<absent>" end
    if type(v) == "table" then return json.encode(v) end
    return tostring(v)
end

local function ignored_path(path, key, ignored)
    for _, f in ipairs(ignored or {}) do
        if path == f or tostring(key) == f then return true end
    end
    return false
end

-- deep_diff walks the union of both sides, so a field that exists only in the
-- shadow is a difference exactly like one that exists only in the primary, and
-- an array that is longer on one side is a difference at the array.
local function deep_diff(a, b, path, ignored, diffs)
    if type(a) ~= type(b) then
        diffs[#diffs + 1] = {path = path, primary = show(a), shadow = show(b)}
        return
    end
    if type(a) ~= "table" then
        if a ~= b then diffs[#diffs + 1] = {path = path, primary = show(a), shadow = show(b)} end
        return
    end

    if is_array(a) and is_array(b) then
        local la, lb = #a, #b
        if la ~= lb then
            diffs[#diffs + 1] = {path = path, primary = "length " .. la, shadow = "length " .. lb}
        end
        for i = 1, math.min(la, lb) do
            deep_diff(a[i], b[i], path .. "[" .. (i - 1) .. "]", ignored, diffs)
        end
        return
    end

    local keys, seen = {}, {}
    for k in pairs(a) do if not seen[k] then seen[k] = true; keys[#keys + 1] = k end end
    for k in pairs(b) do if not seen[k] then seen[k] = true; keys[#keys + 1] = k end end
    table.sort(keys, function(x, y) return tostring(x) < tostring(y) end)
    for _, k in ipairs(keys) do
        local child = path .. "." .. tostring(k)
        if not ignored_path(child, k, ignored) then
            deep_diff(a[k], b[k], child, ignored, diffs)
        end
    end
end

-- first_difference: where two raw bodies first part ways.
local function first_difference(x, y)
    local n = math.min(#x, #y)
    for i = 1, n do
        if x:byte(i) ~= y:byte(i) then return i - 1 end
    end
    return n
end

local function shorten(text)
    text = tostring(text or "")
    if #text > 60 then return text:sub(1, 60) .. "…" end
    return text
end

local function compare(primary_url, shadow_url, ignored, severity)
    local status_p, body_p, err_p = fetch(primary_url)
    local status_s, body_s, err_s = fetch(shadow_url)
    if err_p then return nil, "primary request failed: " .. err_p end
    if err_s then return nil, "shadow request failed: " .. err_s end

    local findings = {}
    if status_p ~= status_s then
        table.insert(findings, {
            label    = "shadow.status_mismatch",
            severity = "critical",
            detail   = string.format("primary=%d shadow=%d", status_p, status_s),
        })
        return findings, status_p, status_s
    end

    local ok_p, data_p = pcall(json.decode, body_p)
    local ok_s, data_s = pcall(json.decode, body_s)
    if ok_p and ok_s then
        local diffs = {}
        deep_diff(data_p, data_s, "$", ignored, diffs)
        for _, d in ipairs(diffs) do
            table.insert(findings, {
                label    = "shadow.diff " .. d.path,
                severity = severity,
                detail   = "primary=" .. d.primary .. " shadow=" .. d.shadow,
            })
        end
    elseif body_p ~= body_s then
        -- Not JSON on at least one side: compare the bodies as they are.
        local at = first_difference(body_p, body_s)
        table.insert(findings, {
            label    = "shadow.body_mismatch",
            severity = severity,
            detail   = string.format("the bodies differ at byte %d: primary=%q shadow=%q (%d vs %d bytes)",
                at, shorten(body_p:sub(at + 1)), shorten(body_s:sub(at + 1)), #body_p, #body_s),
        })
    end
    return findings, status_p, status_s
end

local function result_of(findings, threshold, status_p, status_s)
    local blocking = blocking_count(findings)
    return {
        passed   = blocking <= threshold,
        findings = as_array(findings),
        -- Added to this step's runner.run trace span. Counts and statuses are
        -- bounded; the differing field paths are not, so they stay in findings.
        attributes = {
            ["shadow.diffs"]     = tostring(#findings),
            ["shadow.blocking"]  = tostring(blocking),
            ["shadow.threshold"] = tostring(threshold),
            ["shadow.status"]    = string.format("%s/%s", tostring(status_p), tostring(status_s)),
        },
    }
end

local function run_diff(cfg)
    local findings, status_p, status_s = compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, cfg.severity or "critical")
    if not findings then return nil, status_p end
    return result_of(findings, tonumber(cfg.threshold) or 0, status_p, status_s), nil
end

-- check: the same comparison, reported at warn level and never blocking.
local function run_check(cfg)
    local findings, status_p, status_s = compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, "warn")
    if not findings then return nil, status_p end
    -- status_mismatch is critical in diff; in a passive check it is a warning too.
    for _, f in ipairs(findings) do f.severity = "warn" end
    return result_of(findings, tonumber(cfg.threshold) or 0, status_p, status_s), nil
end

-- compare_paths: primary and shadow are base URLs and each path is diffed on
-- both. A migration is rarely one endpoint, and one step per path would bury
-- the pattern of which routes disagree.
local function run_compare_paths(cfg)
    local base_p   = (cfg.primary or ""):gsub("/$", "")
    local base_s   = (cfg.shadow or ""):gsub("/$", "")
    local ignored  = cfg.ignore_fields or {}
    local severity = cfg.severity or "critical"
    local paths    = cfg.paths or {}
    if #paths == 0 then return nil, "paths must be a non-empty list for compare_paths" end

    local findings, differing = {}, 0
    for _, path in ipairs(paths) do
        local diffs, err = compare(base_p .. path, base_s .. path, ignored, severity)
        if not diffs then return nil, path .. ": " .. err end
        if #diffs > 0 then differing = differing + 1 end
        for _, f in ipairs(diffs) do
            table.insert(findings, {label = f.label, severity = f.severity, detail = path .. " — " .. f.detail})
        end
    end

    local threshold = tonumber(cfg.threshold) or 0
    local blocking  = blocking_count(findings)
    return {
        passed   = blocking <= threshold,
        findings = as_array(findings),
        summary  = string.format("compare_paths: %d of %d paths differ, %d differences",
            differing, #paths, #findings),
        attributes = {
            ["shadow.diffs"]     = tostring(#findings),
            ["shadow.blocking"]  = tostring(blocking),
            ["shadow.threshold"] = tostring(threshold),
            ["shadow.paths"]     = tostring(#paths),
            ["shadow.differing"] = tostring(differing),
        },
    }, nil
end

local function median_latency_ms(url, samples)
    local times = {}
    for _ = 1, samples do
        local httpc = http.new()
        httpc:set_timeout(10000)
        ngx.update_time()
        local started = ngx.now()
        local _, err = httpc:request_uri(url, {method = "GET"})
        if err then return nil, err end
        ngx.update_time()
        times[#times + 1] = (ngx.now() - started) * 1000
    end
    table.sort(times)
    return times[math.ceil(#times / 2)], nil
end

-- compare_latency: a shadow that returns identical payloads twice as slowly is
-- not ready to take traffic. Medians, not means, so one cold request does not
-- decide the step.
local function run_compare_latency(cfg)
    local samples   = tonumber(cfg.samples) or 10
    local max_ratio = tonumber(cfg.max_ratio) or 1.5
    local severity  = cfg.severity or "critical"

    local primary_ms, err = median_latency_ms(cfg.primary or "", samples)
    if err then return nil, "primary request failed: " .. err end
    local shadow_ms
    shadow_ms, err = median_latency_ms(cfg.shadow or "", samples)
    if err then return nil, "shadow request failed: " .. err end

    -- Below a millisecond the ratio is clock noise, not a regression.
    local ratio = shadow_ms / math.max(primary_ms, 1)
    local findings = {}
    if ratio > max_ratio then
        table.insert(findings, {
            label    = "shadow.latency_regression",
            severity = severity,
            detail   = string.format("shadow median %.1fms is %.2fx primary median %.1fms (max %.2fx)",
                shadow_ms, ratio, primary_ms, max_ratio),
        })
    end
    return {
        passed   = blocking_count(findings) == 0,
        findings = as_array(findings),
        attributes = {
            ["shadow.primary_ms"]    = string.format("%.1f", primary_ms),
            ["shadow.shadow_ms"]     = string.format("%.1f", shadow_ms),
            ["shadow.latency_ratio"] = string.format("%.2f", ratio),
        },
    }, nil
end

local OPS = {
    diff            = run_diff,
    check           = run_check,
    compare_paths   = run_compare_paths,
    compare_latency = run_compare_latency,
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

    local op  = payload.op or "diff"
    local cfg = payload.config or {}

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: diff, check, compare_paths, compare_latency"})
    end
    if (cfg.primary or "") == "" or (cfg.shadow or "") == "" then
        return reply(400, {error = "primary and shadow urls are required"})
    end

    local result, err = handler(cfg)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
