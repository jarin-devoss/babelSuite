-- babelsuite-tcp-mock: a stream plugin that answers a suite's TCP mock itself.
--
-- It runs in the stream subsystem's preread phase, where APISIX's ext-plugin
-- does not exist. Each frame the client sends is put to the mock engine, and the
-- engine's payload is written back in the same framing, until the client closes.
local core  = require("apisix.core")
local http  = require("resty.http")
local cjson = require("cjson.safe")

local plugin_name = "babelsuite-tcp-mock"

local schema = {
    type = "object",
    properties = {
        suite     = {type = "string"},
        surface   = {type = "string"},
        operation = {type = "string"},
        engine    = {type = "string"},
        token     = {type = "string"},
        -- "line": frames end with a newline. "length-prefixed": a 4-byte
        -- big-endian length precedes each frame.
        framing   = {type = "string", enum = {"line", "length-prefixed"}, default = "line"},
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

local function read_frame(sock, framing)
    if framing == "length-prefixed" then
        local header, err = sock:receive(4)
        if not header then
            return nil, err
        end
        local b1, b2, b3, b4 = string.byte(header, 1, 4)
        local length = ((b1 * 256 + b2) * 256 + b3) * 256 + b4
        if length == 0 then
            return ""
        end
        return sock:receive(length)
    end
    return sock:receive("*l")
end

local function write_frame(sock, framing, payload)
    if framing == "length-prefixed" then
        local length = #payload
        local header = string.char(
            math.floor(length / 16777216) % 256,
            math.floor(length / 65536) % 256,
            math.floor(length / 256) % 256,
            length % 256)
        return sock:send(header .. payload)
    end
    return sock:send(payload .. "\n")
end

-- ask puts one frame to the engine and returns the payload to write back.
local function ask(conf, frame)
    local client = http.new()
    client:set_timeout(10000)
    local res, err = client:request_uri("http://" .. conf.engine .. "/mocks/async/"
        .. conf.suite .. "/" .. conf.surface .. "/" .. conf.operation, {
        method = "POST",
        body = frame,
        headers = {
            ["Authorization"] = "Bearer " .. conf.token,
            ["Content-Type"] = "text/plain",
            ["X-Babelsuite-Ingress"] = ngx.var.server_addr,
        },
    })
    if not res then
        return "error: the mock engine at " .. conf.engine .. " did not answer: " .. tostring(err)
    end
    if res.status == 401 then
        return "error: the mock engine refused the gateway's token"
    end
    if res.status ~= 200 then
        return "error: the mock engine could not answer (status " .. res.status .. ")"
    end
    return res.body
end

function _M.preread(conf, ctx)
    local sock, err = ngx.req.socket()
    if not sock then
        core.log.error("babelsuite-tcp-mock: no downstream socket: ", err)
        return ngx.exit(ngx.ERROR)
    end
    sock:settimeout(30000)

    local framing = conf.framing or "line"
    while true do
        local frame = read_frame(sock, framing)
        if not frame then
            break
        end
        local ok = write_frame(sock, framing, ask(conf, frame))
        if not ok then
            break
        end
    end
    -- ngx.ERROR ends the session; ngx.OK would fall through to the balancer.
    return ngx.exit(ngx.ERROR)
end

return _M
