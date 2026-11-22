local json = require("cjson")
local http = require("resty.http")

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
_M.name     = "spice-sim"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function sim_request(sim_url, body, timeout_ms)
    local httpc = http.new()
    httpc:set_timeout(timeout_ms or 60000)
    local res, err = httpc:request_uri(sim_url .. "/simulate", {
        method  = "POST",
        body    = json.encode(body),
        headers = {["Content-Type"] = "application/json"},
    })
    if err then return nil, "simulation service error: " .. err end
    if res.status ~= 200 then return nil, "simulation failed: " .. (res.body or "no response") end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "parse response failed" end
    return data, nil
end

local function measure_rise_time(times, voltages, lo_pct, hi_pct)
    local peak = 0
    for _, v in ipairs(voltages) do if v > peak then peak = v end end
    local lo, hi = peak * lo_pct, peak * hi_pct
    local t_lo, t_hi
    for i, v in ipairs(voltages) do
        if not t_lo and v >= lo then t_lo = times[i] end
        if t_lo and not t_hi and v >= hi then t_hi = times[i] end
    end
    if t_lo and t_hi then return (t_hi - t_lo) * 1000 end
end

local function check_voltage_thresholds(cfg, times, voltages, findings)
    local probe   = cfg.probe_node or "out"
    local severity = cfg.severity or "critical"

    if cfg.max_voltage then
        local peak = 0
        for _, v in ipairs(voltages) do if v > peak then peak = v end end
        local max_v = tonumber(cfg.max_voltage)
        if max_v and peak > max_v then
            table.insert(findings, {
                label    = "spice.voltage.peak " .. probe,
                severity = severity,
                detail   = string.format("peak=%.4fV max=%.4fV node=%s", peak, max_v, probe),
            })
        end
    end

    if cfg.min_voltage then
        local floor = math.huge
        for _, v in ipairs(voltages) do if v < floor then floor = v end end
        local min_v = tonumber(cfg.min_voltage)
        if min_v and floor < min_v then
            table.insert(findings, {
                label    = "spice.voltage.min " .. probe,
                severity = severity,
                detail   = string.format("min=%.4fV threshold=%.4fV node=%s", floor, min_v, probe),
            })
        end
    end

    if cfg.max_rise_ms and #times > 0 then
        local rise = measure_rise_time(times, voltages, 0.1, 0.9)
        local max_rise = tonumber(cfg.max_rise_ms)
        if rise and max_rise and rise > max_rise then
            table.insert(findings, {
                label    = "spice.rise_time " .. probe,
                severity = severity,
                detail   = string.format("rise=%.3fms max=%.3fms node=%s", rise, max_rise, probe),
            })
        end
    end
end

local function run_transient(cfg, sim_url)
    local body = {
        netlist    = cfg.netlist or "",
        analysis   = "transient",
        step_ms    = tonumber(cfg.step_time_ms) or 0.1,
        end_ms     = tonumber(cfg.end_time_ms) or 10,
        probe_node = cfg.probe_node or "out",
    }
    local data, err = sim_request(sim_url, body, tonumber(cfg.timeout_ms))
    if err then return nil, err end
    local findings = {}
    check_voltage_thresholds(cfg, data.times or {}, data.voltages or {}, findings)
    local times = data.times or {}
    local volts = data.voltages or {}
    local peak = 0
    for _, v in ipairs(volts) do if v > peak then peak = v end end
    local summary = string.format("transient: samples=%d peak=%.3fV probe=%s",
        #times, peak, cfg.probe_node or "out")
    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings), sample_count = #times, summary = summary, level = ok and "info" or "warn"}, nil
end

local function run_dc_sweep(cfg, sim_url)
    local body = {
        netlist    = cfg.netlist or "",
        analysis   = "dc",
        probe_node = cfg.probe_node or "out",
    }
    local data, err = sim_request(sim_url, body, tonumber(cfg.timeout_ms))
    if err then return nil, err end
    local findings = {}
    check_voltage_thresholds(cfg, data.sweep or {}, data.voltages or {}, findings)
    local volts = data.voltages or {}
    local peak = 0
    for _, v in ipairs(volts) do if v > peak then peak = v end end
    local summary = string.format("dc_sweep: samples=%d peak=%.3fV probe=%s",
        #volts, peak, cfg.probe_node or "out")
    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings), sample_count = #volts, summary = summary, level = ok and "info" or "warn"}, nil
end

local function run_ac_analysis(cfg, sim_url)
    local body = {
        netlist    = cfg.netlist or "",
        analysis   = "ac",
        probe_node = cfg.probe_node or "out",
    }
    local data, err = sim_request(sim_url, body, tonumber(cfg.timeout_ms))
    if err then return nil, err end
    local findings = {}
    local gain = tonumber(data.gain_db)
    if cfg.min_gain_db then
        local min_g = tonumber(cfg.min_gain_db)
        if gain and min_g and gain < min_g then
            table.insert(findings, {
                label    = "spice.ac.gain " .. (cfg.probe_node or "out"),
                severity = cfg.severity or "critical",
                detail   = string.format("gain=%.2fdB min=%.2fdB", gain, min_g),
            })
        end
    end
    local gain_str = gain and string.format("%.2fdB", gain) or "n/a"
    local summary = string.format("ac_analysis: gain=%s probe=%s", gain_str, cfg.probe_node or "out")
    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings), summary = summary, level = ok and "info" or "warn"}, nil
end

local OPS = {
    transient   = run_transient,
    dc_sweep    = run_dc_sweep,
    ac_analysis = run_ac_analysis,
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
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op      = payload.op or "transient"
    local cfg     = payload.config or {}
    local sim_url = (cfg.sim_url or ""):gsub("/$", "")

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: transient, dc_sweep, ac_analysis"})
    end

    if sim_url == "" then ngx.status = 400; ngx.say(json.encode({error = "sim_url is required"})); return ngx.exit(400) end
    if (cfg.netlist or "") == "" then ngx.status = 400; ngx.say(json.encode({error = "netlist is required"})); return ngx.exit(400) end

    local result, err = handler(cfg, sim_url)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
