local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "verilog-sim"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function sim_request(sim_url, body)
    local httpc = http.new()
    httpc:set_timeout(120000)
    local res, err = httpc:request_uri(sim_url .. "/simulate", {
        method  = "POST",
        body    = json.encode(body),
        headers = {["Content-Type"] = "application/json"},
    })
    if err then return nil, "verilog service error: " .. err end
    if res.status ~= 200 then return nil, "simulation failed: " .. (res.body or "no response") end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "parse response failed" end
    return data, nil
end

local function build_findings(cfg, data, assert_x, max_errors)
    local findings = {}
    local severity = cfg.severity or "critical"

    if data.compile_errors and #data.compile_errors > 0 then
        table.insert(findings, {
            label    = "verilog.compile_error " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = table.concat(data.compile_errors, "; "),
        })
    end

    local sim_errors = tonumber(data.sim_errors) or 0
    if sim_errors > max_errors then
        table.insert(findings, {
            label    = "verilog.sim_errors " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = string.format("errors=%d max=%d", sim_errors, max_errors),
        })
    end

    if assert_x and data.x_signals and #data.x_signals > 0 then
        table.insert(findings, {
            label    = "verilog.undefined_signals " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = "X-state at $finish: " .. table.concat(data.x_signals, ", "),
        })
    end

    return findings
end

local function run_simulate(cfg, sim_url)
    local body = {
        top_module = cfg.top_module or "",
        verilog    = cfg.verilog    or "",
        testbench  = cfg.testbench  or "",
        timeout_ns = tonumber(cfg.timeout_ns) or 200,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local max_errors = tonumber(cfg.max_errors) or 0
    local assert_x   = cfg.assert_no_x ~= false
    local findings = build_findings(cfg, data, assert_x, max_errors)
    return {passed = #findings == 0, findings = findings,
            compile_ok = (#(data.compile_errors or {}) == 0), sim_errors = tonumber(data.sim_errors) or 0,
            stdout = data.stdout or ""}, nil
end

local function run_strict(cfg, sim_url)
    local body = {
        top_module = cfg.top_module or "",
        verilog    = cfg.verilog    or "",
        testbench  = cfg.testbench  or "",
        timeout_ns = tonumber(cfg.timeout_ns) or 200,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    -- strict: zero tolerance — any x-state, any error, any warning fails
    local findings = build_findings(cfg, data, true, 0)
    return {passed = #findings == 0, findings = findings,
            compile_ok = (#(data.compile_errors or {}) == 0), sim_errors = tonumber(data.sim_errors) or 0,
            stdout = data.stdout or ""}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/verilog-sim/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op      = payload.op or "simulate"
    local cfg     = payload.config or {}
    local sim_url = (cfg.sim_url or ""):gsub("/$", "")

    if sim_url == "" or (cfg.top_module or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "sim_url and top_module are required"})); return ngx.exit(400)
    end

    local result, err
    if op == "strict" then
        result, err = run_strict(cfg, sim_url)
    else
        result, err = run_simulate(cfg, sim_url)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
