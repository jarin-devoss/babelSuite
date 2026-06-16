local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "dlq-inspector"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function fetch_latest_offset(rest_url, topic)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(
        rest_url .. "/topics/" .. topic .. "/partitions/0/offsets/latest", {
        method  = "GET",
        headers = {["Accept"] = "application/vnd.kafka.v2+json"},
    })
    if err then return nil, "kafka rest proxy error: " .. err end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "parse offset failed" end
    return data, nil
end

local function run_inspect(cfg, rest_url)
    local topic       = cfg.topic or ""
    local max_msgs    = tonumber(cfg.max_messages) or 0
    local severity    = cfg.severity or "critical"
    local data, err = fetch_latest_offset(rest_url, topic)
    if err then return nil, err end
    local count    = tonumber(data and data.offset) or 0
    local findings = {}
    if count > max_msgs then
        table.insert(findings, {
            label    = "dlq.messages " .. topic,
            severity = severity,
            detail   = string.format("found=%d max=%d topic=%s", count, max_msgs, topic),
        })
    end
    return {passed = #findings == 0, findings = findings, message_count = count}, nil
end

local function run_watch_dlq(cfg, rest_url)
    local topic    = cfg.topic or ""
    local max_msgs = tonumber(cfg.max_messages) or 0
    local data, err = fetch_latest_offset(rest_url, topic)
    if err then return nil, err end
    local count    = tonumber(data and data.offset) or 0
    local findings = {}
    -- watch_dlq: always warn, never blocks the suite
    if count > max_msgs then
        table.insert(findings, {
            label    = "dlq.messages " .. topic,
            severity = "warn",
            detail   = string.format("found=%d max=%d topic=%s", count, max_msgs, topic),
        })
    end
    return {passed = #findings == 0, findings = findings, message_count = count}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/dlq-inspector/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op       = payload.op or "inspect"
    local cfg      = payload.config or {}
    local rest_url = (cfg.kafka_rest_url or ""):gsub("/$", "")

    if rest_url == "" or (cfg.topic or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "kafka_rest_url and topic are required"})); return ngx.exit(400)
    end

    local result, err
    if op == "watch_dlq" then
        result, err = run_watch_dlq(cfg, rest_url)
    else
        result, err = run_inspect(cfg, rest_url)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
