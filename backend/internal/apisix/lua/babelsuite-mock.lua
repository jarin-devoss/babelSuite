-- babelsuite-mock: answers a suite's mock operations from inside APISIX.
--
-- For an HTTP-family operation (REST, SOAP, GraphQL, webhooks, SSE) it asks the
-- control plane's mock engine which example matches the request and writes the
-- engine's answer: the gateway is the visible responder, the engine the single
-- source of answers.
--
-- For a gRPC route it only checks the call (mode "grpc-check"). A gRPC success
-- needs an HTTP/2 trailer, which no Lua in APISIX can write, so the answer comes
-- from the control plane's gRPC front end, the route's upstream; this plugin
-- turns an upstream that is down into a real gRPC error instead of an HTTP one.
local core  = require("apisix.core")
local http  = require("resty.http")
local cjson = require("cjson.safe")

local plugin_name = "babelsuite-mock"

local schema = {
    type = "object",
    properties = {
        suite       = {type = "string"},
        surface     = {type = "string"},
        operation   = {type = "string"},
        -- host:port of the control plane's mock engine
        engine      = {type = "string"},
        -- the secret the engine requires on every call
        token       = {type = "string"},
        mode        = {type = "string", enum = {"answer", "grpc-check"}, default = "answer"},
        -- host:port of the gRPC front end, for grpc-check
        grpc_upstream = {type = "string"},
    },
    required = {"suite", "engine", "token"},
}

local _M = {
    version = 0.1,
    priority = 12000,
    name = plugin_name,
    schema = schema,
}

function _M.check_schema(conf)
    return core.schema.check(schema, conf)
end

-- A gRPC refusal is "trailers-only": the status travels in the headers of a
-- response that ends with them. It has to be a header-only response; a plain
-- `return 200` makes gRPC clients report "closed the stream without sending
-- trailers".
local function grpc_refuse(code, message)
    ngx.status = 200
    ngx.header["Content-Type"] = "application/grpc"
    ngx.header["grpc-status"] = tostring(code)
    ngx.header["grpc-message"] = message
    ngx.header["Content-Length"] = "0"
    ngx.send_headers()
    return ngx.exit(ngx.OK)
end

local GRPC_UNAVAILABLE = 14
local GRPC_FAILED_PRECONDITION = 9

local last_reachable = {}

-- reachable reports whether something listens at host:port, remembering the
-- answer for two seconds so a burst of calls does not open a socket each.
local function reachable(address)
    local now = ngx.now()
    local seen = last_reachable[address]
    if seen and now - seen.at < 2 then
        return seen.ok
    end
    local host, port = string.match(address, "^(.-):(%d+)$")
    local ok = false
    if host then
        local sock = ngx.socket.tcp()
        sock:settimeout(300)
        ok = sock:connect(host, tonumber(port)) and true or false
        sock:close()
    end
    last_reachable[address] = {ok = ok, at = now}
    return ok
end

local function check_grpc(conf)
    if conf.token == "" or string.find(conf.token, "${{", 1, true) then
        return grpc_refuse(GRPC_FAILED_PRECONDITION, "the suite's gateway has no mock token")
    end
    if conf.grpc_upstream and conf.grpc_upstream ~= "" and not reachable(conf.grpc_upstream) then
        return grpc_refuse(GRPC_UNAVAILABLE, "the gRPC mock front end at " .. conf.grpc_upstream .. " is not reachable")
    end
end

local function answer(conf, ctx)
    ngx.req.read_body()
    local body = ngx.req.get_body_data() or ""

    local path = "/internal/mock-data/" .. conf.suite .. "/" .. conf.surface .. "/" .. conf.operation
    local args = ngx.var.args
    if args and args ~= "" then
        path = path .. "?" .. args
    end

    local headers = core.request.headers(ctx)
    headers["authorization"] = "Bearer " .. conf.token
    headers["x-babelsuite-original-method"] = ngx.req.get_method()
    headers["x-babelsuite-original-path"] = ngx.var.uri
    -- The address the request arrived on: the sidecar has a different one on every
    -- run's network, so the engine can tell which run is asking. Set here, never
    -- taken from the client.
    headers["x-babelsuite-ingress"] = ngx.var.server_addr
    headers["host"] = nil
    headers["content-length"] = nil
    headers["connection"] = nil
    headers["transfer-encoding"] = nil

    local client = http.new()
    client:set_timeout(10000)
    local res, err = client:request_uri("http://" .. conf.engine .. path, {
        method = "POST",
        body = body,
        headers = headers,
    })
    if not res then
        return 502, {
            error = "the mock engine at " .. conf.engine .. " did not answer: " .. tostring(err),
            operation = conf.operation,
        }
    end
    if res.status == 401 then
        return 502, {
            error = "the mock engine refused the gateway's token; the sidecar was started with another secret",
            operation = conf.operation,
        }
    end
    if res.status ~= 200 then
        return 502, {
            error = "the mock engine could not resolve the operation (status " .. res.status .. ")",
            operation = conf.operation,
        }
    end

    local envelope = cjson.decode(res.body)
    if type(envelope) ~= "table" then
        return 502, {
            error = "the mock engine answered something that is not a resolver envelope",
            operation = conf.operation,
        }
    end

    for name, values in pairs(envelope.headers or {}) do
        if type(values) == "table" then
            for _, value in ipairs(values) do
                core.response.add_header(name, value)
            end
        else
            core.response.set_header(name, values)
        end
    end
    if envelope.mediaType and envelope.mediaType ~= "" then
        core.response.set_header("Content-Type", envelope.mediaType)
    end
    if envelope.stateScope and envelope.stateScope ~= "" then
        core.response.set_header("X-Babelsuite-State-Scope", envelope.stateScope)
    end
    return envelope.status or 200, envelope.body or ""
end

function _M.access(conf, ctx)
    if conf.mode == "grpc-check" then
        return check_grpc(conf)
    end
    return answer(conf, ctx)
end

return _M
