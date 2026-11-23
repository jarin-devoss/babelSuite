-- babelsuite-async-mock: publishes a suite's async mock messages to its broker.
--
-- A broker-style mock (Kafka, RocketMQ, MQTT, NATS) is not a server a client
-- connects to: APISIX cannot be one. It is a publisher, as in every mock server
-- that supports events: the example messages of the mock operation appear on the
-- topic the service under test consumes.
--
-- The control plane POSTs to the trigger route with the operation and the
-- broker's address (resolved on the run's network, which the sidecar's own
-- resolver does not know). The trigger must carry the mock token, so nothing
-- else on the host can make the sidecar publish; the plugin takes the payload
-- from the mock engine and publishes it.
local core  = require("apisix.core")
local http  = require("resty.http")
local cjson = require("cjson.safe")

local plugin_name = "babelsuite-async-mock"

local schema = {
    type = "object",
    properties = {
        engine = {type = "string"},
        token = {type = "string"},
    },
    required = {"engine", "token"},
}

local _M = {
    version = 0.1,
    priority = 1000,
    name = plugin_name,
    schema = schema,
}

function _M.check_schema(conf)
    return core.schema.check(schema, conf)
end

-- payload_of asks the engine for the message of the operation.
local function payload_of(conf, request)
    local client = http.new()
    client:set_timeout(10000)
    local res, err = client:request_uri("http://" .. conf.engine .. "/mocks/async/"
        .. request.suite .. "/" .. request.surface .. "/" .. request.operation, {
        method = "POST",
        body = request.trigger or "",
        headers = {
            ["Authorization"] = "Bearer " .. conf.token,
            ["Content-Type"] = "application/json",
            ["X-Babelsuite-Ingress"] = ngx.var.server_addr,
        },
    })
    if not res then
        return nil, "the mock engine at " .. conf.engine .. " did not answer: " .. tostring(err)
    end
    if res.status ~= 200 then
        return nil, "the mock engine could not answer (status " .. res.status .. ")"
    end
    return res.body
end

local function publish_kafka(request, payload)
    local producer = require("resty.kafka.producer")
    local brokers = {{host = request.host, port = request.port}}
    -- The first send to a topic the broker has just created fails with "not found
    -- topic"; the second works.
    local last_err
    for attempt = 1, 2 do
        local p = producer:new(brokers, {producer_type = "sync", request_timeout = 5000})
        local ok, err = p:send(request.topic, request.key, payload)
        if ok then
            return true
        end
        last_err = err
        ngx.sleep(0.5)
    end
    return nil, "kafka: " .. tostring(last_err)
end

local function publish_rocketmq(request, payload)
    local producer = require("resty.rocketmq.producer")
    local p = producer.new({request.host .. ":" .. request.port}, "babelsuite-mock")
    local started, err = p:start()
    if not started then
        return nil, "rocketmq: " .. tostring(err)
    end
    local result, send_err = p:send(request.topic, payload, request.tags, request.key)
    p:stop()
    if not result then
        return nil, "rocketmq: " .. tostring(send_err)
    end
    return true
end

-- MQTT 3.1.1 at QoS 0: CONNECT, wait for CONNACK, PUBLISH.
local function mqtt_length(n)
    local out = {}
    repeat
        local digit = n % 128
        n = math.floor(n / 128)
        if n > 0 then
            digit = digit + 128
        end
        out[#out + 1] = string.char(digit)
    until n == 0
    return table.concat(out)
end

local function mqtt_string(text)
    return string.char(math.floor(#text / 256), #text % 256) .. text
end

local function publish_mqtt(request, payload)
    local sock = ngx.socket.tcp()
    sock:settimeout(5000)
    local ok, err = sock:connect(request.host, request.port)
    if not ok then
        return nil, "mqtt: could not connect to " .. request.host .. ":" .. request.port .. ": " .. tostring(err)
    end
    local variable = mqtt_string("MQTT") .. string.char(4, 2, 0, 60) .. mqtt_string("babelsuite-mock")
    sock:send(string.char(0x10) .. mqtt_length(#variable) .. variable)
    local connack = sock:receive(4)
    if not connack or string.byte(connack, 1) ~= 0x20 or string.byte(connack, 4) ~= 0 then
        sock:close()
        return nil, "mqtt: the broker did not accept the connection"
    end
    local body = mqtt_string(request.topic) .. payload
    sock:send(string.char(0x30) .. mqtt_length(#body) .. body)
    sock:send(string.char(0xE0, 0x00))
    sock:close()
    return true
end

local function publish_nats(request, payload)
    local sock = ngx.socket.tcp()
    sock:settimeout(5000)
    local ok, err = sock:connect(request.host, request.port)
    if not ok then
        return nil, "nats: could not connect to " .. request.host .. ":" .. request.port .. ": " .. tostring(err)
    end
    local info = sock:receive("*l")
    if not info or string.sub(info, 1, 4) ~= "INFO" then
        sock:close()
        return nil, "nats: the server did not greet with INFO"
    end
    sock:send("CONNECT {\"verbose\":false}\r\n")
    sock:send("PUB " .. request.topic .. " " .. #payload .. "\r\n" .. payload .. "\r\n")
    sock:send("PING\r\n")
    local pong = sock:receive("*l")
    sock:close()
    if pong ~= "PONG" then
        return nil, "nats: the server did not confirm the message (" .. tostring(pong) .. ")"
    end
    return true
end

local publishers = {
    kafka    = publish_kafka,
    rocketmq = publish_rocketmq,
    mqtt     = publish_mqtt,
    nats     = publish_nats,
}

function _M.access(conf, ctx)
    if ngx.req.get_method() ~= "POST" then
        return 405, {error = "post the operation to publish"}
    end
    if (ngx.req.get_headers()["authorization"] or "") ~= "Bearer " .. conf.token then
        return 401, {error = "the trigger must carry the mock token"}
    end
    ngx.req.read_body()
    local request = cjson.decode(ngx.req.get_body_data() or "")
    if type(request) ~= "table" then
        return 400, {error = "the body must be the JSON description of the message to publish"}
    end
    for _, field in ipairs({"suite", "surface", "operation", "kind", "host", "port", "topic"}) do
        if request[field] == nil or request[field] == "" then
            return 400, {error = "missing " .. field}
        end
    end

    local publish = publishers[request.kind]
    if not publish then
        return 400, {error = "cannot publish to " .. tostring(request.kind) .. "; supported: kafka, rocketmq, mqtt, nats"}
    end

    local payload, err = payload_of(conf, request)
    if not payload then
        return 502, {error = err, operation = request.operation}
    end
    local ok, publish_err = publish(request, payload)
    if not ok then
        return 502, {error = publish_err, operation = request.operation}
    end
    return 200, {published = true, kind = request.kind, topic = request.topic, bytes = #payload}
end

return _M
