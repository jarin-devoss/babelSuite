local json = require("cjson")
local http = require("resty.http")

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
_M.name     = "consumer-lag"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function shorten(body)
    body = tostring(body or "")
    if #body > 200 then return body:sub(1, 200) .. "…" end
    return body
end

-- get_json fetches one URL and checks what came back. It returns the decoded
-- body, or nil, a message, and the HTTP status (0 when nothing answered). A
-- check that cannot read what it measures must say so; it must never read an
-- error body as "nothing to report".
local function get_json(url)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {
        method  = "GET",
        headers = {["Accept"] = "application/json"},
    })
    if not res then return nil, "GET " .. url .. " failed: " .. tostring(err), 0 end
    if res.status ~= 200 then
        return nil, string.format("GET %s returned %d: %s", url, res.status, shorten(res.body)), res.status
    end
    local ok, data = pcall(json.decode, res.body or "")
    if not ok or type(data) ~= "table" then
        return nil, "GET " .. url .. " returned a body that is not JSON: " .. shorten(res.body), res.status
    end
    return data, nil, 200
end

-- cluster_id: the Confluent REST Proxy's v3 API addresses a cluster by id. The
-- id is read from the proxy unless the step names one.
local function cluster_id(cfg, rest_url)
    if (cfg.cluster_id or "") ~= "" then return cfg.cluster_id, nil end
    local data, err, status = get_json(rest_url .. "/v3/clusters")
    if err then
        if status == 404 then
            return nil, err .. " — this REST proxy does not serve consumer-group lag; the Confluent REST Proxy v3 API does"
        end
        return nil, err
    end
    local first = type(data.data) == "table" and data.data[1] or nil
    if type(first) ~= "table" or type(first.cluster_id) ~= "string" or first.cluster_id == "" then
        return nil, "unexpected response shape from " .. rest_url .. "/v3/clusters: no data[1].cluster_id"
    end
    return first.cluster_id, nil
end

-- partition_lags returns one entry per partition the group has committed to:
-- {topic, partition, current, log_end, lag}.
local function partition_lags(cfg, rest_url, group)
    local cluster, err = cluster_id(cfg, rest_url)
    if not cluster then return nil, err end

    local url = string.format("%s/v3/clusters/%s/consumer-groups/%s/lags",
        rest_url, ngx.escape_uri(cluster), ngx.escape_uri(group))
    local data, gerr, status = get_json(url)
    if gerr then
        if status == 404 then
            return nil, gerr .. " — group " .. group .. " is not known to the cluster, or this REST proxy does not serve consumer-group lag"
        end
        return nil, gerr
    end
    if type(data.data) ~= "table" then
        return nil, "unexpected response shape from " .. url .. ": no data array"
    end

    local lags = {}
    for _, item in ipairs(data.data) do
        local lag = tonumber(item.lag)
        if lag == nil or type(item.topic_name) ~= "string" or tonumber(item.partition_id) == nil then
            return nil, "unexpected response shape from " .. url .. ": a lag entry lacks topic_name, partition_id or lag"
        end
        lags[#lags + 1] = {
            topic     = item.topic_name,
            partition = tonumber(item.partition_id),
            current   = tonumber(item.current_offset) or 0,
            log_end   = tonumber(item.log_end_offset) or 0,
            lag       = lag,
        }
    end
    if #lags == 0 then
        -- A group that has never committed an offset is not caught up. It has
        -- not started.
        return nil, "group " .. group .. " has no committed offsets on any partition"
    end
    return lags, nil
end

local function total_of(lags)
    local total = 0
    for _, l in ipairs(lags) do total = total + l.lag end
    return total
end

local function lag_findings(lags, group, max_lag, severity)
    local findings = {}
    for _, l in ipairs(lags) do
        if l.lag > max_lag then
            table.insert(findings, {
                label    = "consumer.lag " .. l.topic .. "/" .. tostring(l.partition),
                severity = severity,
                detail   = string.format("lag=%d max=%d group=%s", l.lag, max_lag, group),
            })
        end
    end
    return findings
end

local function run_check_lag(cfg, rest_url)
    local group    = cfg.group or ""
    local max_lag  = tonumber(cfg.max_lag) or 1000
    local severity = cfg.severity or "critical"
    local lags, err = partition_lags(cfg, rest_url, group)
    if err then return nil, err end
    local findings = lag_findings(lags, group, max_lag, severity)
    local total = total_of(lags)
    return {
        passed    = verdict(findings),
        findings  = as_array(findings),
        total_lag = total,
        -- Added to this step's runner.run trace span. Group and thresholds are
        -- bounded; partition offsets would not be, so they stay out.
        attributes = {
            ["lag.group"]    = group,
            ["lag.total"]    = tostring(total),
            ["lag.max"]      = tostring(max_lag),
            ["lag.breached"] = tostring(#findings),
        },
    }, nil
end

-- watch_lag: always warn. It reports, and never blocks.
local function run_watch_lag(cfg, rest_url)
    local group   = cfg.group or ""
    local max_lag = tonumber(cfg.max_lag) or 1000
    local lags, err = partition_lags(cfg, rest_url, group)
    if err then return nil, err end
    local findings = lag_findings(lags, group, max_lag, "warn")
    local total = total_of(lags)
    return {
        passed    = verdict(findings),
        findings  = as_array(findings),
        total_lag = total,
        attributes = {
            ["lag.group"]    = group,
            ["lag.total"]    = tostring(total),
            ["lag.max"]      = tostring(max_lag),
            ["lag.breached"] = tostring(#findings),
            ["lag.mode"]     = "watch",
        },
    }, nil
end

local function total_lag_of(cfg, rest_url, group)
    local lags, err = partition_lags(cfg, rest_url, group)
    if err then return nil, err end
    return total_of(lags), nil
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
        total, err = total_lag_of(cfg, rest_url, group)
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
        passed    = verdict(findings),
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

    local first, err = total_lag_of(cfg, rest_url, group)
    if err then return nil, err end
    ngx.sleep(interval)
    local second
    second, err = total_lag_of(cfg, rest_url, group)
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
        passed     = verdict(findings),
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

local OPS = {
    check_lag      = run_check_lag,
    watch_lag      = run_watch_lag,
    wait_caught_up = run_wait_caught_up,
    check_trend    = run_check_trend,
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

    local op       = payload.op or "check_lag"
    local cfg      = payload.config or {}
    local rest_url = (cfg.kafka_rest_url or ""):gsub("/$", "")

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: check_lag, watch_lag, wait_caught_up, check_trend"})
    end
    if rest_url == "" or (cfg.group or "") == "" then
        return reply(400, {error = "kafka_rest_url and group are required"})
    end

    local result, err = handler(cfg, rest_url)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
