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
_M.name     = "dlq-inspector"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function shorten(body)
    body = tostring(body or "")
    if #body > 200 then return body:sub(1, 200) .. "…" end
    return body
end

-- get_json fetches one URL and checks what came back. It returns the decoded
-- body, or nil, a message, and the HTTP status (0 when nothing answered).
local function get_json(url)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {
        method  = "GET",
        headers = {["Accept"] = "application/vnd.kafka.v2+json"},
    })
    if not res then return nil, "GET " .. url .. " failed: " .. tostring(err), 0 end
    if res.status ~= 200 then
        return nil, string.format("GET %s returned %d: %s", url, res.status, shorten(res.body)), res.status, res.body
    end
    local ok, data = pcall(json.decode, res.body or "")
    if not ok or type(data) ~= "table" then
        return nil, "GET " .. url .. " returned a body that is not JSON: " .. shorten(res.body), res.status
    end
    return data, nil, 200
end

-- message_count adds up what is waiting in a topic: for every partition, the
-- end offset minus the beginning offset. The Confluent REST Proxy's v2 API
-- serves both. A proxy that serves neither (Redpanda's HTTP proxy) cannot say
-- how many messages a topic holds, and the check says that rather than "none".
local function message_count(rest_url, topic)
    local base = rest_url .. "/topics/" .. ngx.escape_uri(topic) .. "/partitions"
    local parts, err, status, body = get_json(base)
    if err then
        if status == 404 then
            local ok, reply = pcall(json.decode, body or "")
            -- 40401: unknown topic; 40403: the topic-partition is not hosted.
            if ok and type(reply) == "table" and (reply.error_code == 40401 or reply.error_code == 40403) then
                return nil, "topic " .. topic .. " does not exist: " .. err
            end
            return nil, err .. " — this REST proxy does not serve partition offsets; the Confluent REST Proxy v2 API does"
        end
        return nil, err
    end
    if #parts == 0 then
        return nil, "unexpected response shape from " .. base .. ": no partitions listed"
    end

    local total, per_partition = 0, {}
    for _, part in ipairs(parts) do
        local id = tonumber(part.partition)
        if id == nil then
            return nil, "unexpected response shape from " .. base .. ": a partition entry has no partition number"
        end
        local url = base .. "/" .. tostring(id) .. "/offsets"
        local offsets, oerr, ostatus = get_json(url)
        if oerr then
            if ostatus == 404 then
                return nil, oerr .. " — this REST proxy does not serve partition offsets; the Confluent REST Proxy v2 API does"
            end
            return nil, oerr
        end
        local first, last = tonumber(offsets.beginning_offset), tonumber(offsets.end_offset)
        if first == nil or last == nil then
            return nil, "unexpected response shape from " .. url .. ": no beginning_offset and end_offset"
        end
        local count = last - first
        per_partition[tostring(id)] = count
        total = total + count
    end
    return total, nil, per_partition
end

local function dlq_finding(topic, count, max_msgs, severity)
    return {
        label    = "dlq.messages " .. topic,
        severity = severity,
        detail   = string.format("found=%d max=%d topic=%s", count, max_msgs, topic),
    }
end

local function run_inspect(cfg, rest_url)
    local topic    = cfg.topic or ""
    local max_msgs = tonumber(cfg.max_messages) or 0
    local severity = cfg.severity or "critical"
    local count, err, partitions = message_count(rest_url, topic)
    if err then return nil, err end
    local findings = {}
    if count > max_msgs then table.insert(findings, dlq_finding(topic, count, max_msgs, severity)) end
    return {passed = verdict(findings), findings = as_array(findings), message_count = count, partitions = partitions}, nil
end

-- watch_dlq: always warn. It reports, and never blocks.
local function run_watch_dlq(cfg, rest_url)
    local topic    = cfg.topic or ""
    local max_msgs = tonumber(cfg.max_messages) or 0
    local count, err, partitions = message_count(rest_url, topic)
    if err then return nil, err end
    local findings = {}
    if count > max_msgs then table.insert(findings, dlq_finding(topic, count, max_msgs, "warn")) end
    return {passed = verdict(findings), findings = as_array(findings), message_count = count, partitions = partitions}, nil
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
    return {passed = verdict(findings), findings = as_array(findings),
            message_count = second, growth = growth}, nil
end

-- inspect_topics: one step over every DLQ a service owns, so a suite does not
-- need an inspect step per topic. Each topic over the limit is its own finding.
local function run_inspect_topics(cfg, rest_url)
    local max_msgs = tonumber(cfg.max_messages) or 0
    local severity = cfg.severity or "critical"
    local topics   = cfg.topics or {}
    if #topics == 0 then return nil, "topics must be a non-empty list for inspect_topics" end

    local findings, counts, total = {}, {}, 0
    for _, topic in ipairs(topics) do
        local count, err = message_count(rest_url, topic)
        if err then return nil, err end
        counts[topic] = count
        total = total + count
        if count > max_msgs then table.insert(findings, dlq_finding(topic, count, max_msgs, severity)) end
    end
    return {passed = verdict(findings), findings = as_array(findings),
            message_count = total, counts = counts,
            summary = string.format("inspect_topics: %d topics, %d messages, %d over the limit",
                #topics, total, #findings)}, nil
end

local OPS = {
    inspect        = run_inspect,
    watch_dlq      = run_watch_dlq,
    check_growth   = run_check_growth,
    inspect_topics = run_inspect_topics,
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

    local op       = payload.op or "inspect"
    local cfg      = payload.config or {}
    local rest_url = (cfg.kafka_rest_url or ""):gsub("/$", "")

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: inspect, watch_dlq, check_growth, inspect_topics"})
    end
    if rest_url == "" then return reply(400, {error = "kafka_rest_url is required"}) end
    if op ~= "inspect_topics" and (cfg.topic or "") == "" then
        return reply(400, {error = "topic is required for " .. op})
    end

    local result, err = handler(cfg, rest_url)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
