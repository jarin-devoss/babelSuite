local json = require("cjson")
local http = require("resty.http")

local function as_array(t) return #t == 0 and json.empty_array or t end

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
    local compile_ok = #(data.compile_errors or {}) == 0
    local sim_errors = tonumber(data.sim_errors) or 0
    local summary = string.format("simulate: module=%s compile_ok=%s sim_errors=%d",
        cfg.top_module or "?", tostring(compile_ok), sim_errors)
    local ok = #findings == 0
    return {passed = ok, findings = as_array(findings),
            compile_ok = compile_ok, sim_errors = sim_errors,
            stdout = data.stdout or "", summary = summary, level = ok and "info" or "warn"}, nil
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
    local compile_ok = #(data.compile_errors or {}) == 0
    local sim_errors = tonumber(data.sim_errors) or 0
    local summary = string.format("strict: module=%s compile_ok=%s sim_errors=%d",
        cfg.top_module or "?", tostring(compile_ok), sim_errors)
    local ok = #findings == 0
    return {passed = ok, findings = as_array(findings),
            compile_ok = compile_ok, sim_errors = sim_errors,
            stdout = data.stdout or "", summary = summary, level = ok and "info" or "warn"}, nil
end

-- lint: compiles the design without running a testbench. Catches syntax and
-- elaboration errors fast, as a gate before the slower simulate step, and
-- needs no testbench — useful for a module that has none yet.
local function run_lint(cfg, sim_url)
    local body = {
        top_module   = cfg.top_module or "",
        verilog      = cfg.verilog    or "",
        testbench    = cfg.testbench  or "",
        timeout_ns   = tonumber(cfg.timeout_ns) or 200,
        compile_only = true,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local severity = cfg.severity or "critical"
    local findings = {}

    if data.compile_errors and #data.compile_errors > 0 then
        table.insert(findings, {
            label    = "verilog.compile_error " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = table.concat(data.compile_errors, "; "),
        })
    end

    -- Warnings are advisory by default; fail_on_warnings turns them into a gate.
    local warnings = data.warnings or {}
    if #warnings > 0 then
        table.insert(findings, {
            label    = "verilog.warnings " .. (cfg.top_module or "?"),
            severity = cfg.fail_on_warnings and severity or "warn",
            detail   = table.concat(warnings, "; "),
        })
    end

    local compile_ok = #(data.compile_errors or {}) == 0
    local blocking = 0
    for _, f in ipairs(findings) do
        if f.severity ~= "warn" then blocking = blocking + 1 end
    end
    local ok = blocking == 0
    return {passed = ok, findings = as_array(findings),
            compile_ok = compile_ok,
            summary = string.format("lint: module=%s compile_ok=%s warnings=%d",
                cfg.top_module or "?", tostring(compile_ok), #warnings),
            level = ok and "info" or "warn"}, nil
end

-- check_coverage: runs the testbench and checks the reported toggle coverage
-- against a floor. A suite can pass every assertion while leaving whole
-- branches of a module unexercised; this makes that gap a finding.
local function run_check_coverage(cfg, sim_url)
    local body = {
        top_module = cfg.top_module or "",
        verilog    = cfg.verilog    or "",
        testbench  = cfg.testbench  or "",
        timeout_ns = tonumber(cfg.timeout_ns) or 200,
        coverage   = true,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local severity = cfg.severity or "critical"
    local findings = {}

    if data.compile_errors and #data.compile_errors > 0 then
        table.insert(findings, {
            label    = "verilog.compile_error " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = table.concat(data.compile_errors, "; "),
        })
    end

    local coverage = tonumber(data.toggle_coverage_pct)
    local min_cov  = tonumber(cfg.min_coverage_pct) or 0
    if coverage == nil then
        table.insert(findings, {
            label    = "verilog.coverage.unreported",
            severity = "warn",
            detail   = "the simulation service returned no toggle_coverage_pct",
        })
    elseif coverage < min_cov then
        table.insert(findings, {
            label    = "verilog.coverage.low " .. (cfg.top_module or "?"),
            severity = severity,
            detail   = string.format("toggle coverage=%.1f%% min=%.1f%%", coverage, min_cov),
        })
    end

    local ok = #findings == 0
    return {passed = ok, findings = as_array(findings),
            toggle_coverage_pct = coverage,
            summary = string.format("check_coverage: module=%s coverage=%.1f%% min=%.1f%%",
                cfg.top_module or "?", coverage or 0, min_cov),
            level = ok and "info" or "warn"}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
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
    elseif op == "lint" then
        result, err = run_lint(cfg, sim_url)
    elseif op == "check_coverage" then
        result, err = run_check_coverage(cfg, sim_url)
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
