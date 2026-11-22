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
_M.name     = "schema-compat"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local MODES = {
    BACKWARD = true, BACKWARD_TRANSITIVE = true,
    FORWARD  = true, FORWARD_TRANSITIVE  = true,
    FULL     = true, FULL_TRANSITIVE     = true,
    NONE     = true,
}

local CONTENT_TYPE = "application/vnd.schemaregistry.v1+json"

local function shorten(body)
    body = tostring(body or "")
    if #body > 200 then return body:sub(1, 200) .. "…" end
    return body
end

-- call sends one request to the registry. It returns the response, or nil and a
-- message when nothing answered.
local function call(method, url, body)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {
        method  = method,
        body    = body,
        headers = {["Content-Type"] = CONTENT_TYPE, ["Accept"] = CONTENT_TYPE},
    })
    if not res then return nil, method .. " " .. url .. " failed: " .. tostring(err) end
    return res, nil
end

local function decode(res)
    local ok, data = pcall(json.decode, res.body or "")
    if ok and type(data) == "table" then return data end
    return nil
end

-- expect_json: the response must be a 200 carrying a JSON object. Anything else
-- is a check that could not run, never a pass.
local function expect_json(res, what)
    if res.status ~= 200 then
        return nil, string.format("%s returned HTTP %d: %s", what, res.status, shorten(res.body))
    end
    local data = decode(res)
    if not data then return nil, what .. " returned a body that is not JSON: " .. shorten(res.body) end
    return data, nil
end

local function trim_slash(url) return (url:gsub("/$", "")) end

local function schema_body(schema, schema_type, references)
    local body = {schema = schema, schemaType = schema_type or "AVRO"}
    if references and #references > 0 then body.references = references end
    return json.encode(body)
end

-- compatible asks the registry whether a schema is compatible with the latest
-- version of a subject, under the compatibility mode the registry holds for
-- that subject. The answer must be a boolean; a reply without one says nothing.
local function compatible(registry_url, subject, schema, schema_type)
    local url = registry_url .. "/compatibility/subjects/" .. ngx.escape_uri(subject) .. "/versions/latest"
    local res, err = call("POST", url, schema_body(schema, schema_type))
    if not res then return nil, err end
    local data, derr = expect_json(res, "the compatibility check")
    if not data then return nil, derr end
    if type(data.is_compatible) ~= "boolean" then
        return nil, "unexpected response shape from the compatibility check: no boolean is_compatible: " .. shorten(res.body)
    end
    return data.is_compatible, nil
end

local scratch_counter = 0
local function scratch_name(subject)
    scratch_counter = scratch_counter + 1
    return string.format("__babelsuite_%d_%d_%d", math.floor(ngx.now()), ngx.worker.pid(), scratch_counter)
end

-- The registry picks the mode from its own configuration for the subject; a
-- request header does not change it. To test a schema under a mode the subject
-- is not set to, the check copies the subject's versions to a scratch subject,
-- sets the mode on that subject, asks there, and deletes it again.
local function versions_of(registry_url, subject, transitive)
    local base = registry_url .. "/subjects/" .. ngx.escape_uri(subject) .. "/versions"
    if not transitive then
        local res, err = call("GET", base .. "/latest")
        if not res then return nil, err end
        if res.status == 404 then
            return nil, "subject " .. subject .. " has no registered version to check against: " .. shorten(res.body)
        end
        local data, derr = expect_json(res, "reading the latest version of " .. subject)
        if not data then return nil, derr end
        if type(data.schema) ~= "string" then
            return nil, "unexpected response shape from " .. base .. "/latest: no schema"
        end
        return {data}, nil
    end

    local res, err = call("GET", base)
    if not res then return nil, err end
    if res.status == 404 then
        return nil, "subject " .. subject .. " has no registered version to check against: " .. shorten(res.body)
    end
    local ok, list = pcall(json.decode, res.body or "")
    if res.status ~= 200 or not ok or type(list) ~= "table" or #list == 0 then
        return nil, string.format("listing the versions of %s returned HTTP %d: %s", subject, res.status, shorten(res.body))
    end
    local out = {}
    for _, version in ipairs(list) do
        local vres, verr = call("GET", base .. "/" .. tostring(version))
        if not vres then return nil, verr end
        local data, derr = expect_json(vres, "reading version " .. tostring(version) .. " of " .. subject)
        if not data then return nil, derr end
        if type(data.schema) ~= "string" then
            return nil, "unexpected response shape for version " .. tostring(version) .. " of " .. subject .. ": no schema"
        end
        out[#out + 1] = data
    end
    return out, nil
end

local function delete_scratch(registry_url, scratch)
    local base = registry_url .. "/subjects/" .. ngx.escape_uri(scratch)
    local soft = call("DELETE", base)
    local hard = call("DELETE", base .. "?permanent=true")
    call("DELETE", registry_url .. "/config/" .. ngx.escape_uri(scratch))
    -- The subject must be gone, or something was left behind in the registry.
    local check = call("GET", registry_url .. "/subjects")
    if check and check.status == 200 then
        local ok, subjects = pcall(json.decode, check.body or "")
        if ok and type(subjects) == "table" then
            for _, name in ipairs(subjects) do
                if name == scratch then return false end
            end
        end
    end
    return (soft ~= nil) or (hard ~= nil)
end

local function check_in_scratch(registry_url, subject, new_schema, schema_type, mode)
    local transitive = mode:sub(-11) == "_TRANSITIVE"
    local versions, err = versions_of(registry_url, subject, transitive)
    if not versions then return nil, err end

    local scratch = scratch_name(subject)
    local base    = registry_url .. "/subjects/" .. ngx.escape_uri(scratch)
    local result, rerr

    local function setup_and_ask()
        -- NONE while the copies are registered: they are history, and the
        -- registry's own default mode could refuse a version that was valid
        -- when it was written.
        local res, err1 = call("PUT", registry_url .. "/config/" .. ngx.escape_uri(scratch), json.encode({compatibility = "NONE"}))
        if not res then return nil, err1 end
        if res.status ~= 200 then
            return nil, string.format("setting a mode on the scratch subject %s returned HTTP %d: %s — the registry must allow per-subject compatibility to test a mode the subject is not set to", scratch, res.status, shorten(res.body))
        end
        for _, version in ipairs(versions) do
            local rres, rerr1 = call("POST", base .. "/versions", schema_body(version.schema, version.schemaType or schema_type, version.references))
            if not rres then return nil, rerr1 end
            if rres.status ~= 200 then
                return nil, string.format("copying a version of %s to the scratch subject returned HTTP %d: %s", subject, rres.status, shorten(rres.body))
            end
        end
        local mres, merr = call("PUT", registry_url .. "/config/" .. ngx.escape_uri(scratch), json.encode({compatibility = mode}))
        if not mres then return nil, merr end
        if mres.status ~= 200 then
            return nil, string.format("setting mode %s on the scratch subject returned HTTP %d: %s", mode, mres.status, shorten(mres.body))
        end
        return compatible(registry_url, scratch, new_schema, schema_type)
    end

    local ok, a, b = pcall(setup_and_ask)
    if ok then result, rerr = a, b else result, rerr = nil, "the compatibility check failed: " .. tostring(a) end

    -- Always remove the scratch subject, whatever happened above.
    local removed = delete_scratch(registry_url, scratch)
    return result, rerr, removed and nil or scratch
end

-- configured_mode: the mode the registry holds for the subject, the global one
-- when the subject has none of its own.
local function configured_mode(registry_url, subject)
    local res, err = call("GET", registry_url .. "/config/" .. ngx.escape_uri(subject) .. "?defaultToGlobal=true")
    if not res then return nil, nil, err end
    if res.status == 404 then
        res, err = call("GET", registry_url .. "/config")
        if not res then return nil, nil, err end
        local data, derr = expect_json(res, "reading the global compatibility level")
        if not data then return nil, nil, derr end
        return data.compatibilityLevel or data.compatibility, "global", nil
    end
    local data, derr = expect_json(res, "reading the compatibility level of " .. subject)
    if not data then return nil, nil, derr end
    return data.compatibilityLevel or data.compatibility, "subject", nil
end

local function check_registry(registry_url, subject, new_schema, schema_type, mode, severity)
    if not MODES[mode] then
        return nil, "unsupported mode " .. tostring(mode) .. "; supported: BACKWARD, BACKWARD_TRANSITIVE, FORWARD, FORWARD_TRANSITIVE, FULL, FULL_TRANSITIVE, NONE"
    end

    local findings = {}
    local is_compatible, err
    local held, _, merr = configured_mode(registry_url, subject)
    if merr then return nil, merr end

    if held == mode then
        -- The subject already enforces this mode: ask directly, and write nothing.
        is_compatible, err = compatible(registry_url, subject, new_schema, schema_type)
    else
        local leftover
        is_compatible, err, leftover = check_in_scratch(registry_url, subject, new_schema, schema_type, mode)
        if leftover then
            table.insert(findings, {
                label    = "schema.scratch_left_behind",
                severity = "warn",
                detail   = "the scratch subject " .. leftover .. " could not be removed from the registry; delete it by hand",
            })
        end
    end
    if err then return nil, err end

    if not is_compatible then
        table.insert(findings, {
            label    = "schema.incompatible",
            severity = severity,
            detail   = string.format("schema is not %s compatible with subject %s", mode, subject),
        })
    end
    return {passed = verdict(findings), findings = as_array(findings), mode = mode}, nil
end

local function run_check_compat(cfg)
    return check_registry(trim_slash(cfg.registry_url or ""), cfg.subject or "", cfg.new_schema or "",
        cfg.schema_type, cfg.mode or "BACKWARD", cfg.severity or "critical")
end

local function run_enforce(cfg)
    -- enforce always uses FULL compatibility (both forward and backward)
    return check_registry(trim_slash(cfg.registry_url or ""), cfg.subject or "", cfg.new_schema or "",
        cfg.schema_type, "FULL", cfg.severity or "critical")
end

-- check_registered: asks the registry whether new_schema is already a version
-- of the subject. Use it after the step that publishes a schema, to prove the
-- schema the service will look up by id is really there.
local function run_check_registered(cfg)
    local registry_url = trim_slash(cfg.registry_url or "")
    local subject      = cfg.subject or ""
    local severity     = cfg.severity or "critical"

    local res, err = call("POST", registry_url .. "/subjects/" .. ngx.escape_uri(subject),
        schema_body(cfg.new_schema or "", cfg.schema_type))
    if not res then return nil, err end

    local findings, version, id = {}, nil, nil
    if res.status == 404 then
        table.insert(findings, {
            label    = "schema.not_registered",
            severity = severity,
            detail   = "subject " .. subject .. " has no version matching this schema: " .. shorten(res.body),
        })
    else
        local data, derr = expect_json(res, "looking up the schema")
        if not data then return nil, derr end
        version, id = data.version, data.id
        if version == nil or id == nil then
            return nil, "unexpected response shape from the schema lookup: no version and id: " .. shorten(res.body)
        end
    end
    return {passed = verdict(findings), findings = as_array(findings),
            version = version, schema_id = id,
            summary = string.format("check_registered: subject=%s version=%s id=%s",
                subject, tostring(version or "none"), tostring(id or "none"))}, nil
end

-- check_mode: reads the compatibility level the registry enforces for the
-- subject, falling back to the global level when the subject has none. A
-- subject quietly set to NONE accepts any change, which makes every
-- check_compat against it meaningless.
local function run_check_mode(cfg)
    local registry_url = trim_slash(cfg.registry_url or "")
    local subject      = cfg.subject or ""
    local expected     = cfg.mode or "BACKWARD"
    local severity     = cfg.severity or "critical"

    local level, source, err = configured_mode(registry_url, subject)
    if err then return nil, err end
    if not level then
        return nil, "the registry reported no compatibility level for " .. subject
    end

    local findings = {}
    if level ~= expected then
        table.insert(findings, {
            label    = "schema.mode_mismatch",
            severity = severity,
            detail   = string.format("subject %s enforces %s (%s setting), expected %s",
                subject, level, source, expected),
        })
    end
    return {passed = verdict(findings), findings = as_array(findings),
            mode = level, mode_source = source}, nil
end

local OPS = {
    check_compat     = run_check_compat,
    enforce          = run_enforce,
    check_registered = run_check_registered,
    check_mode       = run_check_mode,
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

    local op  = payload.op or "check_compat"
    local cfg = payload.config or {}

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: check_compat, enforce, check_registered, check_mode"})
    end
    if (cfg.registry_url or "") == "" or (cfg.subject or "") == "" then
        return reply(400, {error = "registry_url and subject are required"})
    end
    -- check_mode reads the registry's settings; every other op sends a schema.
    if op ~= "check_mode" and (cfg.new_schema or "") == "" then
        return reply(400, {error = "new_schema is required for " .. op})
    end

    local result, err = handler(cfg)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
