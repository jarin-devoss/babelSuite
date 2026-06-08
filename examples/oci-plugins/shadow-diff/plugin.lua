local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "shadow-diff"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function fetch(url)
    local httpc = http.new()
    httpc:set_timeout(10000)
    local res, err = httpc:request_uri(url, {method = "GET"})
    if err then return nil, nil, err end
    return res.status, res.body or "", nil
end

local function deep_diff(a, b, path, ignored)
    local diffs = {}
    if type(a) ~= type(b) then
        table.insert(diffs, {path = path, primary = tostring(a), shadow = tostring(b)})
        return diffs
    end
    if type(a) == "table" then
        for k, v in pairs(a) do
            local child_path = path .. "." .. tostring(k)
            local skip = false
            for _, f in ipairs(ignored or {}) do
                if child_path == f or tostring(k) == f then skip = true; break end
            end
            if not skip then
                for _, d in ipairs(deep_diff(v, b[k], child_path, ignored)) do
                    table.insert(diffs, d)
                end
            end
        end
    elseif a ~= b then
        table.insert(diffs, {path = path, primary = tostring(a), shadow = tostring(b)})
    end
    return diffs
end

local function compare(primary_url, shadow_url, ignored, threshold, severity)
    local status_p, body_p, err_p = fetch(primary_url)
    local status_s, body_s, err_s = fetch(shadow_url)

    if err_p or err_s then
        return nil, "upstream request failed: " .. tostring(err_p or err_s)
    end

    local findings = {}

    if status_p ~= status_s then
        table.insert(findings, {
            label    = "shadow.status_mismatch",
            severity = "critical",
            detail   = string.format("primary=%d shadow=%d", status_p, status_s),
        })
    else
        local ok_p, data_p = pcall(json.decode, body_p)
        local ok_s, data_s = pcall(json.decode, body_s)
        if ok_p and ok_s then
            for _, d in ipairs(deep_diff(data_p, data_s, "$", ignored)) do
                table.insert(findings, {
                    label    = "shadow.diff " .. d.path,
                    severity = severity,
                    detail   = "primary=" .. tostring(d.primary) .. " shadow=" .. tostring(d.shadow),
                })
            end
        end
    end

    local passed = #findings <= (threshold or 0)
    return {passed = passed, findings = findings}, nil
end

local function run_diff(cfg)
    return compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, tonumber(cfg.threshold) or 0,
        cfg.severity or "warn")
end

local function run_check(cfg)
    -- check: same logic as diff but findings are always warn
    return compare(cfg.primary or "", cfg.shadow or "",
        cfg.ignore_fields or {}, tonumber(cfg.threshold) or 0, "warn")
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/shadow-diff/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "diff"
    local cfg = payload.config or {}

    if (cfg.primary or "") == "" or (cfg.shadow or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "primary and shadow urls are required"})); return ngx.exit(400)
    end

    local result, err
    if op == "check" then
        result, err = run_check(cfg)
    else
        result, err = run_diff(cfg)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
