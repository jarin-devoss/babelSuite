local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, not [], so a step that found
-- nothing — the passing case — would otherwise emit a findings object.
local function as_array(t) return #t == 0 and json.empty_array or t end

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
    return {passed = #findings == 0, findings = as_array(findings), message_count = count}, nil
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
    return {passed = #findings == 0, findings = as_array(findings), message_count = count}, nil
end

local function message_count(rest_url, topic)
    local data, err = fetch_latest_offset(rest_url, topic)
    if err then return nil, err end
    return tonumber(data and data.offset) or 0, nil
end

-- check_growth: samples the DLQ twice and fails when it grew in between. The
-- absolute count includes everything dead-lettered before this run; growth
-- isolates what is being dead-lettered right now.
local function run_check_growth(cfg, rest_url)
    local topic      = cfg.topic or ""
    local interval   = tonumber(cfg.interval_s) or 10
    local max_growth = tonumber(cfg.max_growth) or 0
    local severity   = cfg.severity or "critical"

    local first, err = message_count(rest_url, topic)
    if err then return nil, err end
    ngx.sleep(interval)
    local second
    second, err = message_count(rest_url, topic)
    if err then return nil, err end

    local growth   = second - first
    local findings = {}
    if growth > max_growth then
        table.insert(findings, {
            label    = "dlq.growing " .. topic,
            severity = severity,
            detail   = string.format("grew %d -> %d in %gs (max growth=%d) topic=%s",
                first, second, interval, max_growth, topic),
        })
    end
    return {passed = #findings == 0, findings = as_array(findings),
            message_count = second, growth = growth}, nil
end

-- inspect_topics: one step over every DLQ a service owns, so a suite does not
-- need an inspect step per topic. Each topic over the limit is its own finding.
local function run_inspect_topics(cfg, rest_url)
    local max_msgs = tonumber(cfg.max_messages) or 0
    local severity = cfg.severity or "critical"
    local findings, counts, total = {}, {}, 0
    for _, topic in ipairs(cfg.topics or {}) do
        local count, err = message_count(rest_url, topic)
        if err then return nil, err end
        counts[topic] = count
        total = total + count
        if count > max_msgs then
            table.insert(findings, {
                label    = "dlq.messages " .. topic,
                severity = severity,
                detail   = string.format("found=%d max=%d topic=%s", count, max_msgs, topic),
            })
        end
    end
    return {passed = #findings == 0, findings = as_array(findings),
            message_count = total, counts = counts,
            summary = string.format("inspect_topics: %d topics, %d messages, %d over the limit",
                #(cfg.topics or {}), total, #findings)}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op       = payload.op or "inspect"
    local cfg      = payload.config or {}
    local rest_url = (cfg.kafka_rest_url or ""):gsub("/$", "")

    if rest_url == "" then
        ngx.status = 400; ngx.say(json.encode({error = "kafka_rest_url is required"})); return ngx.exit(400)
    end
    if op == "inspect_topics" then
        if type(cfg.topics) ~= "table" or #cfg.topics == 0 then
            ngx.status = 400; ngx.say(json.encode({error = "topics must be a non-empty list"})); return ngx.exit(400)
        end
    elseif (cfg.topic or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "topic is required"})); return ngx.exit(400)
    end

    local result, err
    if op == "watch_dlq" then
        result, err = run_watch_dlq(cfg, rest_url)
    elseif op == "check_growth" then
        result, err = run_check_growth(cfg, rest_url)
    elseif op == "inspect_topics" then
        result, err = run_inspect_topics(cfg, rest_url)
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
