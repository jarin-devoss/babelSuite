-- A minimal plugin for the UI end-to-end tests. It answers every request with a
-- fixed verdict so a test can see the plugin was registered, loaded by the
-- sidecar and dispatched to. A fixture for the tests, not an example.
local json = require("cjson")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "e2e-echo"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then
        ngx.status = 400
        ngx.say(json.encode({error = "invalid json"}))
        return ngx.exit(400)
    end

    local cfg = payload.config or {}
    local message = cfg.message or "hello"
    local findings = json.empty_array
    local passed = true
    if cfg.fail == true then
        passed = false
        findings = {{label = "e2e.rejected", severity = "critical", detail = "rejected on request: " .. message}}
    end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode({
        passed = passed,
        findings = findings,
        summary = "e2e-echo: " .. message,
    }))
    return ngx.exit(200)
end

return _M
