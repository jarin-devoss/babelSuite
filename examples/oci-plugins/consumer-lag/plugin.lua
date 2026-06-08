local json = require("cjson")
local http = require("resty.http")

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
    return {passed = #findings == 0, findings = findings, total_lag = total_lag}, nil
end

local function run_watch_lag(cfg, rest_url)
    local group    = cfg.group or ""
    local max_lag  = tonumber(cfg.max_lag) or 1000
    local data, err = fetch_offsets(rest_url, group)
    if err then return nil, err end
    -- watch_lag: always warn, never fails the suite
    local findings, total_lag = build_lag_findings(data, group, max_lag, "warn")
    return {passed = #findings == 0, findings = findings, total_lag = total_lag}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/consumer-lag/start" then return ngx.exit(404) end
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
