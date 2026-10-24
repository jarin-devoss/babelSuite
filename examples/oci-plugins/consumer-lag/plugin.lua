local json = require("cjson")
local http = require("resty.http")

local function as_array(t) return #t == 0 and json.empty_array or t end

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "consumer-lag"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function fetch_offsets(rest_url, group)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(rest_url .. "/consumers/" .. group .. "/offsets", {
        method  = "GET",
        headers = {["Accept"] = "application/vnd.kafka.v2+json"},
    })
    if err then return nil, "kafka rest proxy error: " .. err end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "parse offsets failed" end
    return data, nil
end

local function build_lag_findings(data, group, max_lag, severity)
    local findings, total_lag = {}, 0
    if data and data.offsets then
        for _, offset in ipairs(data.offsets) do
            local lag = (offset.end_offset or 0) - (offset.offset or 0)
            total_lag = total_lag + lag
            if lag > max_lag then
                table.insert(findings, {
                    label    = "consumer.lag " .. (offset.topic or "?") .. "/" .. tostring(offset.partition or 0),
                    severity = severity,
                    detail   = string.format("lag=%d max=%d group=%s", lag, max_lag, group),
                })
            end
        end
    end
    return findings, total_lag
end

local function run_check_lag(cfg, rest_url)
    local group    = cfg.group or ""
    local max_lag  = tonumber(cfg.max_lag) or 1000
    local severity = cfg.severity or "critical"
    local data, err = fetch_offsets(rest_url, group)
    if err then return nil, err end
    local findings, total_lag = build_lag_findings(data, group, max_lag, severity)
    return {
        passed    = #findings == 0,
        findings  = as_array(findings),
        total_lag = total_lag,
        -- Added to this step's runner.run trace span. Group and thresholds are
        -- bounded; partition offsets would not be, so they stay out.
        attributes = {
            ["lag.group"]   = group,
            ["lag.total"]   = tostring(total_lag),
            ["lag.max"]     = tostring(max_lag),
            ["lag.breached"] = tostring(#findings),
        },
    }, nil
end

local function run_watch_lag(cfg, rest_url)
    local group    = cfg.group or ""
    local max_lag  = tonumber(cfg.max_lag) or 1000
    local data, err = fetch_offsets(rest_url, group)
    if err then return nil, err end
    -- watch_lag: always warn, never fails the suite
    local findings, total_lag = build_lag_findings(data, group, max_lag, "warn")
    return {
        passed    = #findings == 0,
        findings  = as_array(findings),
        total_lag = total_lag,
        attributes = {
            ["lag.group"]    = group,
            ["lag.total"]    = tostring(total_lag),
            ["lag.max"]      = tostring(max_lag),
            ["lag.breached"] = tostring(#findings),
            ["lag.mode"]     = "watch",
        },
    }, nil
end

local function total_lag_of(rest_url, group)
    local data, err = fetch_offsets(rest_url, group)
    if err then return nil, err end
    local _, total = build_lag_findings(data, group, math.huge, "warn")
    return total, nil
end

-- wait_caught_up: polls until the group's total lag drops to max_lag or the
-- timeout passes. A load test ends long before its consumers finish, so a
-- single check_lag right after it reports lag that would have drained.
local function run_wait_caught_up(cfg, rest_url)
    local group     = cfg.group or ""
    local max_lag   = tonumber(cfg.max_lag) or 0
    local timeout_s = tonumber(cfg.timeout_s) or 60
    local interval  = tonumber(cfg.interval_s) or 2
    local severity  = cfg.severity or "critical"

    ngx.update_time()
    local started, total, polls = ngx.now(), 0, 0
    while true do
        local err
        total, err = total_lag_of(rest_url, group)
        if err then return nil, err end
        polls = polls + 1
        ngx.update_time()
        if total <= max_lag or ngx.now() - started >= timeout_s then break end
        ngx.sleep(interval)
    end

    ngx.update_time()
    local waited   = ngx.now() - started
    local findings = {}
    if total > max_lag then
        table.insert(findings, {
            label    = "consumer.lag.not_caught_up",
            severity = severity,
            detail   = string.format("group=%s still %d behind after %.0fs (max=%d)", group, total, waited, max_lag),
        })
    end
    return {
        passed    = #findings == 0,
        findings  = as_array(findings),
        total_lag = total,
        summary   = string.format("wait_caught_up: group=%s lag=%d after %d polls in %.1fs",
            group, total, polls, waited),
        attributes = {
            ["lag.group"]  = group,
            ["lag.total"]  = tostring(total),
            ["lag.max"]    = tostring(max_lag),
            ["lag.waited"] = string.format("%.1f", waited),
            ["lag.mode"]   = "wait",
        },
    }, nil
end

-- check_trend: samples twice and fails when lag grew between the samples. A
-- high but shrinking lag is a consumer catching up; a growing one is a
-- consumer that cannot keep pace with its producers.
local function run_check_trend(cfg, rest_url)
    local group      = cfg.group or ""
    local interval   = tonumber(cfg.interval_s) or 10
    local max_growth = tonumber(cfg.max_growth) or 0
    local severity   = cfg.severity or "critical"

    local first, err = total_lag_of(rest_url, group)
    if err then return nil, err end
    ngx.sleep(interval)
    local second
    second, err = total_lag_of(rest_url, group)
    if err then return nil, err end

    local growth   = second - first
    local findings = {}
    if growth > max_growth then
        table.insert(findings, {
            label    = "consumer.lag.growing",
            severity = severity,
            detail   = string.format("group=%s lag grew %d -> %d in %gs (max growth=%d)",
                group, first, second, interval, max_growth),
        })
    end
    return {
        passed     = #findings == 0,
        findings   = as_array(findings),
        total_lag  = second,
        lag_growth = growth,
        attributes = {
            ["lag.group"]  = group,
            ["lag.total"]  = tostring(second),
            ["lag.growth"] = tostring(growth),
            ["lag.mode"]   = "trend",
        },
    }, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op       = payload.op or "check_lag"
    local cfg      = payload.config or {}
    local rest_url = (cfg.kafka_rest_url or ""):gsub("/$", "")

    if rest_url == "" or (cfg.group or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "kafka_rest_url and group are required"})); return ngx.exit(400)
    end

    local result, err
    if op == "watch_lag" then
        result, err = run_watch_lag(cfg, rest_url)
    elseif op == "wait_caught_up" then
        result, err = run_wait_caught_up(cfg, rest_url)
    elseif op == "check_trend" then
        result, err = run_check_trend(cfg, rest_url)
    else
        result, err = run_check_lag(cfg, rest_url)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
