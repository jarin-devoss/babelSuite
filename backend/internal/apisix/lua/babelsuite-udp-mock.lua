-- babelsuite-udp-mock: a stream plugin that answers a suite's UDP mock itself.
-- One datagram in, one datagram out, both through the mock engine.
local core = require("apisix.core")
local http = require("resty.http")

local plugin_name = "babelsuite-udp-mock"

local schema = {
    type = "object",
    properties = {
        suite     = {type = "string"},
        surface   = {type = "string"},
        operation = {type = "string"},
        engine    = {type = "string"},
        token     = {type = "string"},
    },
    required = {"suite", "surface", "operation", "engine", "token"},
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

local function ask(conf, datagram)
    local client = http.new()
    client:set_timeout(10000)
    local res, err = client:request_uri("http://" .. conf.engine .. "/mocks/async/"
        .. conf.suite .. "/" .. conf.surface .. "/" .. conf.operation, {
        method = "POST",
        body = datagram,
        headers = {
            ["Authorization"] = "Bearer " .. conf.token,
            ["Content-Type"] = "application/octet-stream",
            ["X-Babelsuite-Ingress"] = ngx.var.server_addr,
        },
    })
    if not res then
        return "error: the mock engine at " .. conf.engine .. " did not answer: " .. tostring(err)
    end
    if res.status ~= 200 then
        return "error: the mock engine could not answer (status " .. res.status .. ")"
    end
    return res.body
end

function _M.preread(conf, ctx)
    local sock, err = ngx.req.socket()
    if not sock then
        core.log.error("babelsuite-udp-mock: no downstream socket: ", err)
        return ngx.exit(ngx.ERROR)
    end
    local datagram = sock:receive()
    if datagram then
        sock:send(ask(conf, datagram))
    end
    return ngx.exit(ngx.ERROR)
end

return _M
