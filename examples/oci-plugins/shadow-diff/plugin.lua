local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, which the runner cannot decode into
-- a findings array — so two identical responses, the whole point of a passing
-- shadow comparison, is exactly the case that breaks without this.
local function as_array(t) return #t == 0 and json.empty_array or t end

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "shadow-diff"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function fetch(url)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {method = "GET"})
    if err then return nil, nil, err end
    return res.status, res.body or "", nil
end

local function deep_diff(a, b, path, ignored)
    local diffs = {}
    if type(a) ~= type(b) then
        table.insert(diffs, {path = path, primary = tostring(a), shadow = tostring(b)})
        return diffs
    end
    if type(a) == "table" then
        for k, v in pairs(a) do
            local child_path = path .. "." .. tostring(k)
            local skip = false
            for _, f in ipairs(ignored or {}) do
                if child_path == f or tostring(k) == f then skip = true; break end
            end
            if not skip then
                for _, d in ipairs(deep_diff(v, b[k], child_path, ignored)) do
                    table.insert(diffs, d)
                end
            end
        end
    elseif a ~= b then
        table.insert(diffs, {path = path, primary = tostring(a), shadow = tostring(b)})
    end
    return diffs
end

local function compare(primary_url, shadow_url, ignored, threshold, severity)
    local status_p, body_p, err_p = fetch(primary_url)
    local status_s, body_s, err_s = fetch(shadow_url)

    if err_p or err_s then
        return nil, "upstream request failed: " .. tostring(err_p or err_s)
    end

    local findings = {}

    if status_p ~= status_s then
        table.insert(findings, {
            label    = "shadow.status_mismatch",
            severity = "critical",
            detail   = string.format("primary=%d shadow=%d", status_p, status_s),
        })
    else
        local ok_p, data_p = pcall(json.decode, body_p)
        local ok_s, data_s = pcall(json.decode, body_s)
        if ok_p and ok_s then
            for _, d in ipairs(deep_diff(data_p, data_s, "$", ignored)) do
                table.insert(findings, {
                    label    = "shadow.diff " .. d.path,
                    severity = severity,
                    detail   = "primary=" .. tostring(d.primary) .. " shadow=" .. tostring(d.shadow),
                })
            end
        end
    end

    local passed = #findings <= (threshold or 0)
    return {
        passed   = passed,
        findings = as_array(findings),
        -- Added to this step's runner.run trace span. Counts and statuses are
        -- bounded; the differing field paths are not, so they stay in findings.
        attributes = {
            ["shadow.diffs"]     = tostring(#findings),
            ["shadow.threshold"] = tostring(threshold or 0),
            ["shadow.status"]    = string.format("%d/%d", status_p, status_s),
        },
    }, nil
end

local function run_diff(cfg)
    return compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, tonumber(cfg.threshold) or 0,
        cfg.severity or "warn")
end

local function run_check(cfg)
    -- check: same logic as diff but findings are always warn
    return compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, tonumber(cfg.threshold) or 0, "warn")
end

-- compare_paths: primary and shadow are base URLs and each path is diffed on
-- both. A migration is rarely one endpoint, and one step per path would bury
-- the pattern of which routes disagree.
local function run_compare_paths(cfg)
    local base_p   = (cfg.primary or ""):gsub("/$", "")
    local base_s   = (cfg.shadow or ""):gsub("/$", "")
    local ignored  = cfg.ignore_fields or {}
    local severity = cfg.severity or "warn"
    local findings, differing = {}, 0

    for _, path in ipairs(cfg.paths or {}) do
        local result, err = compare(base_p .. path, base_s .. path, ignored, math.huge, severity)
        if err then return nil, path .. ": " .. err end
        -- as_array hands back cjson's empty_array sentinel, not a table, when
        -- the path matched.
        local diffs = type(result.findings) == "table" and result.findings or {}
        if #diffs > 0 then differing = differing + 1 end
        for _, f in ipairs(diffs) do
            table.insert(findings, {label = f.label, severity = f.severity, detail = path .. " — " .. f.detail})
        end
    end

    local threshold = tonumber(cfg.threshold) or 0
    return {
        passed   = #findings <= threshold,
        findings = as_array(findings),
        summary  = string.format("compare_paths: %d of %d paths differ, %d differences",
            differing, #(cfg.paths or {}), #findings),
        attributes = {
            ["shadow.diffs"]     = tostring(#findings),
            ["shadow.threshold"] = tostring(threshold),
            ["shadow.paths"]     = tostring(#(cfg.paths or {})),
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
    local severity  = cfg.severity or "warn"

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
        passed   = #findings == 0,
        findings = as_array(findings),
        attributes = {
            ["shadow.primary_ms"]    = string.format("%.1f", primary_ms),
            ["shadow.shadow_ms"]     = string.format("%.1f", shadow_ms),
            ["shadow.latency_ratio"] = string.format("%.2f", ratio),
        },
    }, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "diff"
    local cfg = payload.config or {}

    if (cfg.primary or "") == "" or (cfg.shadow or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "primary and shadow urls are required"})); return ngx.exit(400)
    end

    local result, err
    if op == "check" then
        result, err = run_check(cfg)
    elseif op == "compare_paths" then
        result, err = run_compare_paths(cfg)
    elseif op == "compare_latency" then
        result, err = run_compare_latency(cfg)
    else
        result, err = run_diff(cfg)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
