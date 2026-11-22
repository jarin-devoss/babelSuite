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
_M.name     = "control-sim"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local function sim_request(sim_url, body)
    local httpc = http.new()
    httpc:set_timeout(30000)
    local res, err = httpc:request_uri(sim_url .. "/simulate", {
        method  = "POST",
        body    = json.encode(body),
        headers = {["Content-Type"] = "application/json"},
    })
    if err then return nil, "control service error: " .. err end
    if res.status ~= 200 then return nil, "simulation failed: " .. (res.body or "no response") end
    local ok, data = pcall(json.decode, res.body or "{}")
    if not ok then return nil, "parse response failed" end
    return data, nil
end

local function check_stability_thresholds(cfg, data, severity, findings)
    if not data.stable then
        table.insert(findings, {
            label    = "control.unstable",
            severity = severity,
            detail   = "system is unstable (poles in right-half plane)",
        })
    end

    local settling = tonumber(data.settling_time)
    if cfg.max_settling_time and settling then
        local max_s = tonumber(cfg.max_settling_time)
        if max_s and settling > max_s then
            table.insert(findings, {
                label    = "control.settling_time",
                severity = severity,
                detail   = string.format("settling=%.3fs max=%.3fs", settling, max_s),
            })
        end
    end

    local overshoot = tonumber(data.overshoot_pct)
    if cfg.max_overshoot_pct and overshoot then
        local max_os = tonumber(cfg.max_overshoot_pct)
        if max_os and overshoot > max_os then
            table.insert(findings, {
                label    = "control.overshoot",
                severity = severity,
                detail   = string.format("overshoot=%.2f%% max=%.2f%%", overshoot, max_os),
            })
        end
    end

    local dc_gain = tonumber(data.dc_gain)
    if dc_gain then
        if cfg.min_dc_gain then
            local min_g = tonumber(cfg.min_dc_gain)
            if min_g and dc_gain < min_g then
                table.insert(findings, {
                    label    = "control.dc_gain.low",
                    severity = severity,
                    detail   = string.format("dc_gain=%.4f min=%.4f", dc_gain, min_g),
                })
            end
        end
        if cfg.max_dc_gain then
            local max_g = tonumber(cfg.max_dc_gain)
            if max_g and dc_gain > max_g then
                table.insert(findings, {
                    label    = "control.dc_gain.high",
                    severity = severity,
                    detail   = string.format("dc_gain=%.4f max=%.4f", dc_gain, max_g),
                })
            end
        end
    end
end

local function run_step_response(cfg, sim_url)
    local body = {
        numerator   = cfg.numerator   or {},
        denominator = cfg.denominator or {},
        analysis    = cfg.analysis    or "step",
        time_end    = tonumber(cfg.time_end) or 10,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local findings = {}
    check_stability_thresholds(cfg, data, cfg.severity or "critical", findings)
    local settling  = tonumber(data.settling_time)
    local overshoot = tonumber(data.overshoot_pct)
    local dc_gain   = tonumber(data.dc_gain)
    local summary = string.format("step_response: stable=%s settling=%.3fs overshoot=%.2f%% dc_gain=%.4f",
        tostring(data.stable or false),
        settling or 0, overshoot or 0, dc_gain or 0)
    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings),
            stable = data.stable or false, settling_s = settling,
            overshoot_pct = overshoot, dc_gain = dc_gain, summary = summary, level = ok and "info" or "warn"}, nil
end

local function run_monitor(cfg, sim_url)
    local body = {
        numerator   = cfg.numerator   or {},
        denominator = cfg.denominator or {},
        analysis    = "step",
        time_end    = tonumber(cfg.time_end) or 10,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    -- monitor: always warn severity, never blocks the suite
    local findings = {}
    check_stability_thresholds(cfg, data, "warn", findings)
    local settling  = tonumber(data.settling_time)
    local overshoot = tonumber(data.overshoot_pct)
    local dc_gain   = tonumber(data.dc_gain)
    local summary = string.format("monitor: stable=%s settling=%.3fs overshoot=%.2f%% dc_gain=%.4f",
        tostring(data.stable or false),
        settling or 0, overshoot or 0, dc_gain or 0)
    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings),
            stable = data.stable or false, settling_s = settling,
            overshoot_pct = overshoot, dc_gain = dc_gain, summary = summary, level = ok and "info" or "warn"}, nil
end

-- impulse_response: the system's answer to a unit impulse. A stable plant must
-- return to rest; a response that does not decay is a sign of marginal or
-- unstable dynamics the step test can miss when it only watches the final
-- value. Reuses the stability and settling thresholds.
local function run_impulse_response(cfg, sim_url)
    local body = {
        numerator   = cfg.numerator   or {},
        denominator = cfg.denominator or {},
        analysis    = "impulse",
        time_end    = tonumber(cfg.time_end) or 10,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local severity = cfg.severity or "critical"
    local findings = {}

    if not data.stable then
        table.insert(findings, {
            label    = "control.unstable",
            severity = severity,
            detail   = "system is unstable (poles in right-half plane)",
        })
    end

    -- A decayed impulse response ends near zero. final_value is the response at
    -- time_end; well-damped systems leave it close to zero.
    local final = tonumber(data.final_value)
    local max_residual = tonumber(cfg.max_residual)
    if max_residual and final and math.abs(final) > max_residual then
        table.insert(findings, {
            label    = "control.impulse.residual",
            severity = severity,
            detail   = string.format("response has not decayed: |final|=%.4f max=%.4f", math.abs(final), max_residual),
        })
    end

    local settling = tonumber(data.settling_time)
    if cfg.max_settling_time and settling then
        local max_s = tonumber(cfg.max_settling_time)
        if max_s and settling > max_s then
            table.insert(findings, {
                label    = "control.settling_time",
                severity = severity,
                detail   = string.format("settling=%.3fs max=%.3fs", settling, max_s),
            })
        end
    end

    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings),
            stable = data.stable or false, settling_s = settling, final_value = final,
            summary = string.format("impulse_response: stable=%s settling=%.3fs final=%.4f",
                tostring(data.stable or false), settling or 0, final or 0),
            level = ok and "info" or "warn"}, nil
end

-- frequency_response: a Bode analysis, checking the stability margins. Gain and
-- phase margins say how much loop gain or delay the plant tolerates before it
-- oscillates — the headroom a time-domain step response never reports.
local function run_frequency_response(cfg, sim_url)
    local body = {
        numerator   = cfg.numerator   or {},
        denominator = cfg.denominator or {},
        analysis    = "bode",
        time_end    = tonumber(cfg.time_end) or 10,
    }
    local data, err = sim_request(sim_url, body)
    if err then return nil, err end
    local severity = cfg.severity or "critical"
    local findings = {}

    local gain_margin  = tonumber(data.gain_margin_db)
    local phase_margin = tonumber(data.phase_margin_deg)

    local min_gm = tonumber(cfg.min_gain_margin_db)
    if min_gm and gain_margin and gain_margin < min_gm then
        table.insert(findings, {
            label    = "control.gain_margin.low",
            severity = severity,
            detail   = string.format("gain margin=%.2fdB min=%.2fdB", gain_margin, min_gm),
        })
    end

    local min_pm = tonumber(cfg.min_phase_margin_deg)
    if min_pm and phase_margin and phase_margin < min_pm then
        table.insert(findings, {
            label    = "control.phase_margin.low",
            severity = severity,
            detail   = string.format("phase margin=%.2f° min=%.2f°", phase_margin, min_pm),
        })
    end

    local ok = verdict(findings)
    return {passed = ok, findings = as_array(findings),
            gain_margin_db = gain_margin, phase_margin_deg = phase_margin,
            summary = string.format("frequency_response: gain_margin=%.2fdB phase_margin=%.2f°",
                gain_margin or 0, phase_margin or 0),
            level = ok and "info" or "warn"}, nil
end

local OPS = {
    step_response      = run_step_response,
    monitor            = run_monitor,
    impulse_response   = run_impulse_response,
    frequency_response = run_frequency_response,
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

    local op      = payload.op or "step_response"
    local cfg     = payload.config or {}
    local sim_url = (cfg.sim_url or ""):gsub("/$", "")

    local handler = OPS[op]
    if not handler then
        return reply(400, {error = "unknown op " .. tostring(op) .. "; supported: step_response, monitor, impulse_response, frequency_response"})
    end

    if sim_url == "" or not cfg.numerator or not cfg.denominator then
        ngx.status = 400; ngx.say(json.encode({error = "sim_url, numerator, and denominator are required"})); return ngx.exit(400)
    end

    local result, err = handler(cfg, sim_url)
    if err then return reply(500, {error = err}) end
    return reply(200, result)
end

return _M
