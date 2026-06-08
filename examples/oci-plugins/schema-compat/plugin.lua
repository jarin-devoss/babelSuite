local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "schema-compat"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function check_registry(registry_url, subject, new_schema, mode, severity)
    local url  = registry_url .. "/compatibility/subjects/" .. subject .. "/versions/latest"
    local body = json.encode({schema = new_schema, schemaType = "AVRO"})

    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {
        method  = "POST",
        body    = body,
        headers = {["Content-Type"] = "application/vnd.schemaregistry.v1+json",
                   ["X-Compatibility-Type"] = mode},
    })

    if err then return nil, "registry request failed: " .. err end

    local ok, result = pcall(json.decode, res.body or "{}")
    local findings   = {}

    if not ok or res.status ~= 200 then
        table.insert(findings, {
            label    = "schema.registry_error",
            severity = severity,
            detail   = "registry returned HTTP " .. tostring(res.status) .. ": " .. (res.body or ""),
        })
    elseif result and result.is_compatible == false then
        table.insert(findings, {
            label    = "schema.incompatible",
            severity = severity,
            detail   = string.format("schema is not %s compatible with subject %s", mode, subject),
        })
    end

    return {passed = #findings == 0, findings = findings, mode = mode}, nil
end

local function run_check_compat(cfg)
    local registry_url = (cfg.registry_url or ""):gsub("/$", "")
    local subject      = cfg.subject or ""
    local new_schema   = cfg.new_schema or ""
    local mode         = cfg.mode or "BACKWARD"
    local severity     = cfg.severity or "critical"
    return check_registry(registry_url, subject, new_schema, mode, severity)
end

local function run_enforce(cfg)
    local registry_url = (cfg.registry_url or ""):gsub("/$", "")
    local subject      = cfg.subject or ""
    local new_schema   = cfg.new_schema or ""
    local severity     = cfg.severity or "critical"
    -- enforce always uses FULL compatibility (both forward and backward)
    return check_registry(registry_url, subject, new_schema, "FULL", severity)
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/schema-compat/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "check_compat"
    local cfg = payload.config or {}

    if (cfg.registry_url or "") == "" or (cfg.subject or "") == "" or (cfg.new_schema or "") == "" then
        ngx.status = 400
        ngx.say(json.encode({error = "registry_url, subject, and new_schema are required"}))
        return ngx.exit(400)
    end

    local result, err
    if op == "enforce" then
        result, err = run_enforce(cfg)
    else
        result, err = run_check_compat(cfg)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
