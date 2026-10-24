local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, not [], so a step that found
-- nothing — the passing case — would otherwise emit a findings object.
local function as_array(t) return #t == 0 and json.empty_array or t end

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

    return {passed = #findings == 0, findings = as_array(findings), mode = mode}, nil
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

local function registry_call(method, url, body)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {
        method  = method,
        body    = body,
        headers = {["Content-Type"] = "application/vnd.schemaregistry.v1+json"},
    })
    if err then return nil, "registry request failed: " .. err end
    return res, nil
end

-- check_registered: asks the registry whether new_schema is already a version
-- of the subject. Use it after the step that publishes a schema, to prove the
-- schema the service will look up by id is really there.
local function run_check_registered(cfg)
    local registry_url = (cfg.registry_url or ""):gsub("/$", "")
    local subject      = cfg.subject or ""
    local severity     = cfg.severity or "critical"

    local res, err = registry_call("POST", registry_url .. "/subjects/" .. subject,
        json.encode({schema = cfg.new_schema or "", schemaType = "AVRO"}))
    if err then return nil, err end

    local findings = {}
    local ok, data = pcall(json.decode, res.body or "{}")
    if res.status == 404 then
        table.insert(findings, {
            label    = "schema.not_registered",
            severity = severity,
            detail   = "subject " .. subject .. " has no version matching this schema: " .. (res.body or ""),
        })
    elseif res.status ~= 200 or not ok then
        table.insert(findings, {
            label    = "schema.registry_error",
            severity = severity,
            detail   = "registry returned HTTP " .. tostring(res.status) .. ": " .. (res.body or ""),
        })
    end

    local version = ok and type(data) == "table" and data.version or nil
    local id      = ok and type(data) == "table" and data.id or nil
    return {passed = #findings == 0, findings = as_array(findings),
            version = version, schema_id = id,
            summary = string.format("check_registered: subject=%s version=%s id=%s",
                subject, tostring(version or "none"), tostring(id or "none"))}, nil
end

-- check_mode: reads the compatibility level the registry enforces for the
-- subject, falling back to the global level when the subject has none. A
-- subject quietly set to NONE accepts any change, which makes every
-- check_compat against it meaningless.
local function run_check_mode(cfg)
    local registry_url = (cfg.registry_url or ""):gsub("/$", "")
    local subject      = cfg.subject or ""
    local expected     = cfg.mode or "BACKWARD"
    local severity     = cfg.severity or "critical"

    local level, source = nil, "subject"
    local res, err = registry_call("GET", registry_url .. "/config/" .. subject)
    if err then return nil, err end
    if res.status == 404 then
        source = "global"
        res, err = registry_call("GET", registry_url .. "/config")
        if err then return nil, err end
    end
    local ok, data = pcall(json.decode, res.body or "{}")
    if res.status == 200 and ok and type(data) == "table" then
        level = data.compatibilityLevel or data.compatibility
    end

    local findings = {}
    if not level then
        table.insert(findings, {
            label    = "schema.registry_error",
            severity = severity,
            detail   = "could not read the compatibility level: HTTP " .. tostring(res.status) .. ": " .. (res.body or ""),
        })
    elseif level ~= expected then
        table.insert(findings, {
            label    = "schema.mode_mismatch",
            severity = severity,
            detail   = string.format("subject %s enforces %s (%s setting), expected %s",
                subject, level, source, expected),
        })
    end
    return {passed = #findings == 0, findings = as_array(findings),
            mode = level, mode_source = source}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "check_compat"
    local cfg = payload.config or {}

    if (cfg.registry_url or "") == "" or (cfg.subject or "") == "" then
        ngx.status = 400
        ngx.say(json.encode({error = "registry_url and subject are required"}))
        return ngx.exit(400)
    end
    -- check_mode reads the registry's settings; every other op sends a schema.
    if op ~= "check_mode" and (cfg.new_schema or "") == "" then
        ngx.status = 400
        ngx.say(json.encode({error = "new_schema is required for " .. op}))
        return ngx.exit(400)
    end

    local result, err
    if op == "enforce" then
        result, err = run_enforce(cfg)
    elseif op == "check_registered" then
        result, err = run_check_registered(cfg)
    elseif op == "check_mode" then
        result, err = run_check_mode(cfg)
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
