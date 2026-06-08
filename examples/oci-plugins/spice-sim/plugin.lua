local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
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
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local findings = {}
    check_voltage_thresholds(cfg, data.times or {}, data.voltages or {}, findings)
    return {passed = #findings == 0, findings = findings, sample_count = #(data.times or {})}, nil
end

local function run_dc_sweep(cfg, sim_url)
    local body = {
        netlist    = cfg.netlist or "",
        analysis   = "dc",
        probe_node = cfg.probe_node or "out",
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local findings = {}
    check_voltage_thresholds(cfg, data.sweep or {}, data.voltages or {}, findings)
    return {passed = #findings == 0, findings = findings, sample_count = #(data.voltages or {})}, nil
end

local function run_ac_analysis(cfg, sim_url)
    local body = {
        netlist    = cfg.netlist or "",
        analysis   = "ac",
        probe_node = cfg.probe_node or "out",
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local findings = {}
    if cfg.min_gain_db then
        local gain = tonumber(data.gain_db)
        local min_g = tonumber(cfg.min_gain_db)
        if gain and min_g and gain < min_g then
            table.insert(findings, {
                label    = "spice.ac.gain " .. (cfg.probe_node or "out"),
                severity = cfg.severity or "critical",
                detail   = string.format("gain=%.2fdB min=%.2fdB", gain, min_g),
            })
        end
    end
    return {passed = #findings == 0, findings = findings}, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/spice-sim/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op      = payload.op or "transient"
    local cfg     = payload.config or {}
    local sim_url = (cfg.sim_url or ""):gsub("/$", "")

    if sim_url == "" then ngx.status = 400; ngx.say(json.encode({error = "sim_url is required"})); return ngx.exit(400) end
    if (cfg.netlist or "") == "" then ngx.status = 400; ngx.say(json.encode({error = "netlist is required"})); return ngx.exit(400) end

    local result, err
    if op == "dc_sweep" then
        result, err = run_dc_sweep(cfg, sim_url)
    elseif op == "ac_analysis" then
        result, err = run_ac_analysis(cfg, sim_url)
    else
        result, err = run_transient(cfg, sim_url)
    end

    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
