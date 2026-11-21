package apisix

// TrafficCannonPluginName is the APISIX plugin name registered in apisix.yaml.
const TrafficCannonPluginName = "babelsuite-traffic-cannon"

// TrafficCannonTriggerRoute is the URI that starts a load run.
const TrafficCannonTriggerRoute = "/_babelsuite/traffic/start"

// TrafficCannonRoutePrefix is the prefix of every cannon URI: start, status,
// result and stop all live under it.
const TrafficCannonRoutePrefix = "/_babelsuite/traffic/"

// TrafficSharedDictName is the nginx shared dictionary the cannon counts into.
// The sidecar's config.yaml must declare it, or the plugin cannot load.
const TrafficSharedDictName = "babelsuite_traffic"

// TrafficCannonLua is the OpenResty/Lua plugin embedded verbatim into the
// generated apisix.yaml. It generates the load inside the APISIX sidecar, so no
// extra process or container is needed.
//
// It only counts. It never computes a percentile or an average: it returns
// exact counters and latency buckets, and the control plane derives every
// statistic from them. The buckets use the same boundaries as the control
// plane's (exact below 100 ms, then 10 ms, 100 ms and 1 s steps), so the two
// sides agree on every number.
//
// Protocol, all under /_babelsuite/traffic/:
//
//	POST start        body: run configuration         -> 202 {"run_id": "..."}
//	GET  status/<id>                                  -> 200 progress counters
//	GET  result/<id>                                  -> 200 the counts, once the run is over
//	POST stop/<id>                                    -> 200
//
// Everything a run records is keyed by its run id, so runs never see each
// other's data, and every key carries a time-to-live, so nothing outlives a run
// by more than 15 minutes.
//
// Execution models:
//   - closed: virtual users, ramped up and down at the stage's spawn rate; each
//     user picks a weighted task, sends it, then waits as its wait mode says;
//   - open: arrivals at a fixed rate whatever the response time; an arrival that
//     would exceed max_in_flight is counted as dropped instead of queued.
const TrafficCannonLua = `
local json     = require("cjson")
local http_lib = require("resty.http")
local shared   = ngx.shared.babelsuite_traffic

local _M = {}
_M.version  = 0.2
_M.priority = 10
_M.name     = "babelsuite-traffic-cannon"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

local PREFIX      = "/_babelsuite/traffic/"
local MAX_USERS   = 32
local MAX_RATE    = 100
local MAX_SECONDS = 300
local MAX_STAGES  = 12
local MAX_RUNS    = 4
local KEEP_SECS   = 900

-- ── helpers ──────────────────────────────────────────────────────────────────

local function reply(status, body)
    ngx.status = status
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(body))
    return ngx.exit(status)
end

-- Same boundaries as the control plane's bucketLatencyMs.
local function bucket_ms(ms)
    ms = math.floor(ms)
    if ms < 0 then ms = 0 end
    if ms < 100 then return ms end
    if ms < 1000 then return math.floor(ms / 10) * 10 end
    if ms < 10000 then return math.floor(ms / 100) * 100 end
    return math.floor(ms / 1000) * 1000
end

local function clean(name)
    return (tostring(name):gsub("|", "_"))
end

local function key(id, ...)
    return id .. "|" .. table.concat({...}, "|")
end

local function split(text, sep)
    local out = {}
    for piece in (text .. sep):gmatch("(.-)" .. sep:gsub("%p", "%%%0")) do
        out[#out + 1] = piece
    end
    return out
end

local function classify_error(err)
    local e = tostring(err or ""):lower()
    if e:find("timeout", 1, true) then return "timeout" end
    if e:find("refused", 1, true) or e:find("no route", 1, true) or e:find("unreachable", 1, true) or e:find("reset", 1, true) then return "connect" end
    if e:find("resolve", 1, true) or e:find("name error", 1, true) or e:find("no resolver", 1, true) then return "dns" end
    return "error"
end

local function classify_status(code)
    if code >= 500 then return "http_5xx:" .. code end
    if code >= 400 then return "http_4xx:" .. code end
    return "status_mismatch:" .. code
end

local function status_ok(code, expected)
    if expected and #expected > 0 then
        for _, want in ipairs(expected) do
            if code == want then return true end
        end
        return false
    end
    return code >= 200 and code < 400
end

local function pick(items, total)
    local roll = math.random() * total
    local acc = 0
    for _, item in ipairs(items) do
        acc = acc + item.weight
        if roll <= acc then return item end
    end
    return items[#items]
end

-- ── recording ────────────────────────────────────────────────────────────────

local function stopped(run)
    return shared:get(key(run.id, "stop")) ~= nil
end

local function record(run, sampler, latency_ms, ok, category)
    local id, ttl = run.id, run.ttl
    local bucket = bucket_ms(latency_ms)
    local stage = run.stage
    shared:incr(key(id, "tc"), 1, 0, ttl)
    shared:incr(key(id, "c", sampler), 1, 0, ttl)
    shared:incr(key(id, "s", sampler), latency_ms, 0, ttl)
    shared:incr(key(id, "b", sampler, bucket), 1, 0, ttl)
    shared:incr(key(id, "sc", stage), 1, 0, ttl)
    shared:incr(key(id, "ss", stage), latency_ms, 0, ttl)
    shared:incr(key(id, "sb", stage, bucket), 1, 0, ttl)
    shared:incr(key(id, "ps", math.floor(ngx.now() - run.t0)), 1, 0, ttl)
    -- Min and max: every light thread of a run lives in one worker and these
    -- reads and writes never yield, so the update is effectively atomic.
    local lo = shared:get(key(id, "mn", sampler))
    if not lo or latency_ms < lo then shared:set(key(id, "mn", sampler), latency_ms, ttl) end
    local hi = shared:get(key(id, "mx", sampler))
    if not hi or latency_ms > hi then shared:set(key(id, "mx", sampler), latency_ms, ttl) end
    if not ok then
        shared:incr(key(id, "tf"), 1, 0, ttl)
        shared:incr(key(id, "f", sampler), 1, 0, ttl)
        shared:incr(key(id, "sf", stage), 1, 0, ttl)
        shared:incr(key(id, "e", sampler, category or "error"), 1, 0, ttl)
    end
end

local function send(run, cfg, task)
    local httpc = http_lib.new()
    httpc:set_timeout(cfg.timeout_ms or 10000)
    local headers = {}
    for k, v in pairs(task.headers or {}) do headers[k] = v end
    if cfg.host_header and cfg.host_header ~= "" then headers["Host"] = cfg.host_header end
    local body = task.body
    if body == "" then body = nil end

    local started = ngx.now()
    local res, err = httpc:request_uri(cfg.target .. (task.path or "/"), {
        method     = task.method or "GET",
        body       = body,
        headers    = headers,
        ssl_verify = false,
    })
    local latency = (ngx.now() - started) * 1000
    if not res then
        record(run, task.sampler, latency, false, classify_error(err))
    elseif status_ok(res.status, task.expect_status) then
        record(run, task.sampler, latency, true)
    else
        record(run, task.sampler, latency, false, classify_status(res.status))
    end
end

-- ── models ───────────────────────────────────────────────────────────────────

local function user_loop(run, cfg, class, flag)
    while not flag.stop and not stopped(run) and ngx.now() < run.deadline do
        local began = ngx.now()
        send(run, cfg, pick(class.tasks, class.task_total))
        local wait = class.wait or {}
        if wait.mode == "pacing" then
            ngx.sleep(math.max(0, (wait.s or 1) - (ngx.now() - began)))
        elseif wait.mode == "between" then
            local lo, hi = wait.min_s or 0, wait.max_s or 0
            ngx.sleep(lo + math.random() * math.max(0, hi - lo))
        else
            ngx.sleep(wait.s or 0)
        end
    end
end

local function hold(run, until_ts)
    while ngx.now() < until_ts and not stopped(run) do
        ngx.sleep(math.min(0.1, math.max(0, until_ts - ngx.now())))
    end
end

local function stage_begin(run, index)
    run.stage = index
    shared:set(key(run.id, "st0", index), ngx.now(), run.ttl)
end

local function stage_end(run, index)
    shared:set(key(run.id, "st1", index), ngx.now(), run.ttl)
end

local function run_closed(run, cfg)
    local active, retired = {}, {}
    for index, stage in ipairs(cfg.stages) do
        if stopped(run) then break end
        stage_begin(run, index - 1)
        local ends = ngx.now() + stage.duration_s
        local target = stage.users or 1
        local rate = stage.spawn_rate or 0

        while #active < target and ngx.now() < ends and not stopped(run) do
            local flag = {stop = false}
            local class = pick(cfg.users, cfg.user_total)
            active[#active + 1] = {flag = flag, thread = ngx.thread.spawn(user_loop, run, cfg, class, flag)}
            if rate > 0 then ngx.sleep(1 / rate) end
        end
        while #active > target do
            local user = table.remove(active)
            user.flag.stop = true
            retired[#retired + 1] = user
        end
        if #active > run.peak then run.peak = #active end
        shared:set(key(run.id, "users"), #active, run.ttl)

        hold(run, ends)
        stage_end(run, index - 1)
        if stage.stop then break end
    end
    for _, user in ipairs(active) do user.flag.stop = true end
    for _, user in ipairs(active) do ngx.thread.wait(user.thread) end
    for _, user in ipairs(retired) do ngx.thread.wait(user.thread) end
    shared:set(key(run.id, "users"), 0, run.ttl)
end

local function run_open(run, cfg)
    local in_flight = 0
    for index, stage in ipairs(cfg.stages) do
        if stopped(run) then break end
        stage_begin(run, index - 1)
        local rate = math.min(stage.rate or cfg.rate or 1, MAX_RATE)
        if rate <= 0 then rate = 1 end
        local interval = 1 / rate
        local ends = ngx.now() + stage.duration_s
        local due = ngx.now()
        while due < ends and not stopped(run) do
            local now = ngx.now()
            if due > now then ngx.sleep(due - now) end
            if in_flight < (cfg.max_in_flight or 256) then
                in_flight = in_flight + 1
                local class = pick(cfg.users, cfg.user_total)
                ngx.thread.spawn(function()
                    pcall(send, run, cfg, pick(class.tasks, class.task_total))
                    in_flight = in_flight - 1
                end)
            else
                shared:incr(key(run.id, "dropped"), 1, 0, run.ttl)
            end
            due = due + interval
        end
        stage_end(run, index - 1)
        if stage.stop then break end
    end
    -- Let the arrivals in flight finish.
    local give_up = ngx.now() + (cfg.timeout_ms or 10000) / 1000 + 1
    while in_flight > 0 and ngx.now() < give_up do ngx.sleep(0.05) end
end

local function driver(premature, cfg)
    if premature then return end
    local run = cfg.run
    run.t0 = ngx.now()
    run.stage = 0
    run.peak = 0
    math.randomseed(math.floor(ngx.now() * 1000) + ngx.worker.pid())
    shared:set(key(run.id, "t0"), run.t0, run.ttl)

    local ok, err = pcall(function()
        if cfg.model == "open" then run_open(run, cfg) else run_closed(run, cfg) end
    end)

    shared:set(key(run.id, "t1"), ngx.now(), run.ttl)
    shared:set(key(run.id, "peak"), run.peak, run.ttl)
    local state = "done"
    if not ok then
        state = "error"
        shared:set(key(run.id, "error"), tostring(err), run.ttl)
    elseif stopped(run) then
        state = "stopped"
    end
    shared:set(key(run.id, "state"), state, KEEP_SECS)
end

-- ── endpoints ────────────────────────────────────────────────────────────────

local function count_running()
    local running = 0
    for _, k in ipairs(shared:get_keys(0)) do
        if k:sub(-6) == "|state" and shared:get(k) == "running" then running = running + 1 end
    end
    return running
end

local function normalise(cfg)
    if type(cfg.target) ~= "string" or cfg.target == "" then return nil, "target is required" end
    if type(cfg.run_id) ~= "string" or cfg.run_id == "" then return nil, "run_id is required" end
    if type(cfg.stages) ~= "table" or #cfg.stages == 0 then return nil, "at least one stage is required" end
    if #cfg.stages > MAX_STAGES then return nil, "at most " .. MAX_STAGES .. " stages" end

    local total = 0
    for _, stage in ipairs(cfg.stages) do
        stage.duration_s = tonumber(stage.duration_s) or 0
        if stage.duration_s <= 0 then return nil, "every stage needs a positive duration_s" end
        if (stage.users or 0) > MAX_USERS then return nil, "at most " .. MAX_USERS .. " users" end
        if (stage.rate or 0) > MAX_RATE or (cfg.rate or 0) > MAX_RATE then return nil, "at most " .. MAX_RATE .. " requests per second" end
        total = total + stage.duration_s
    end
    if total > MAX_SECONDS then return nil, "a run lasts at most " .. MAX_SECONDS .. " seconds" end

    if type(cfg.users) ~= "table" or #cfg.users == 0 then return nil, "at least one user is required" end
    cfg.user_total = 0
    for _, class in ipairs(cfg.users) do
        class.weight = math.max(1, tonumber(class.weight) or 1)
        cfg.user_total = cfg.user_total + class.weight
        if type(class.tasks) ~= "table" or #class.tasks == 0 then return nil, "every user needs a task" end
        class.task_total = 0
        for _, task in ipairs(class.tasks) do
            task.weight = math.max(1, tonumber(task.weight) or 1)
            task.sampler = clean(task.sampler or ((task.method or "GET") .. " " .. (task.path or "/")))
            class.task_total = class.task_total + task.weight
        end
    end
    return total
end

local function handle_start()
    if not shared then
        return reply(503, {error = "shared dict babelsuite_traffic is not declared in the sidecar config"})
    end
    ngx.req.read_body()
    local ok, cfg = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok or type(cfg) ~= "table" then return reply(400, {error = "invalid config JSON"}) end

    local total, problem = normalise(cfg)
    if not total then return reply(400, {error = problem}) end
    if shared:get(key(cfg.run_id, "state")) ~= nil then return reply(409, {error = "run id already used"}) end
    if count_running() >= MAX_RUNS then return reply(429, {error = "too many load runs at once"}) end

    cfg.run = {id = cfg.run_id, ttl = total + KEEP_SECS, deadline = ngx.now() + total + 5}
    shared:set(key(cfg.run_id, "state"), "running", cfg.run.ttl)
    local timer_ok, timer_err = ngx.timer.at(0, driver, cfg)
    if not timer_ok then
        shared:delete(key(cfg.run_id, "state"))
        return reply(500, {error = "could not start the run: " .. tostring(timer_err)})
    end
    return reply(202, {run_id = cfg.run_id})
end

local function number(id, ...)
    return tonumber(shared:get(key(id, ...))) or 0
end

local function handle_status(id)
    local state = shared:get(key(id, "state"))
    if not state then return reply(404, {error = "unknown run"}) end
    local t0 = number(id, "t0")
    return reply(200, {
        state            = state,
        elapsed_s        = t0 > 0 and (ngx.now() - t0) or 0,
        active_users     = number(id, "users"),
        requests         = number(id, "tc"),
        failures         = number(id, "tf"),
        dropped          = number(id, "dropped"),
        dict_free_bytes  = shared:free_space(),
    })
end

local function handle_result(id)
    local state = shared:get(key(id, "state"))
    if not state then return reply(404, {error = "unknown run"}) end
    if state == "running" then return reply(409, {error = "the run is still going"}) end

    local prefix = id .. "|"
    local samplers, stages, per_second = {}, {}, {}
    local function sampler(name)
        local s = samplers[name]
        if not s then
            s = {count = 0, failures = 0, sum_ms = 0, min_ms = 0, max_ms = 0, buckets = {}, errors = {}}
            samplers[name] = s
        end
        return s
    end
    local function stage(index)
        local s = stages[index]
        if not s then
            s = {index = index, count = 0, failures = 0, sum_ms = 0, started_at = 0, finished_at = 0, buckets = {}}
            stages[index] = s
        end
        return s
    end

    for _, k in ipairs(shared:get_keys(0)) do
        if k:sub(1, #prefix) == prefix then
            local parts = split(k:sub(#prefix + 1), "|")
            local kind, a, b = parts[1], parts[2], parts[3]
            local value = shared:get(k)
            if kind == "c" then sampler(a).count = value
            elseif kind == "f" then sampler(a).failures = value
            elseif kind == "s" then sampler(a).sum_ms = value
            elseif kind == "mn" then sampler(a).min_ms = value
            elseif kind == "mx" then sampler(a).max_ms = value
            elseif kind == "b" then sampler(a).buckets[b] = value
            elseif kind == "e" then sampler(a).errors[b] = value
            elseif kind == "sc" then stage(tonumber(a)).count = value
            elseif kind == "sf" then stage(tonumber(a)).failures = value
            elseif kind == "ss" then stage(tonumber(a)).sum_ms = value
            elseif kind == "sb" then stage(tonumber(a)).buckets[b] = value
            elseif kind == "st0" then stage(tonumber(a)).started_at = value
            elseif kind == "st1" then stage(tonumber(a)).finished_at = value
            elseif kind == "ps" then per_second[a] = value
            end
        end
    end

    local ordered = {}
    for _, value in pairs(stages) do ordered[#ordered + 1] = value end
    table.sort(ordered, function(x, y) return x.index < y.index end)
    if #ordered == 0 then ordered = json.empty_array end

    return reply(200, {
        state       = state,
        error       = shared:get(key(id, "error")),
        started_at  = number(id, "t0"),
        finished_at = number(id, "t1"),
        peak_users  = number(id, "peak"),
        dropped     = number(id, "dropped"),
        samplers    = samplers,
        stages      = ordered,
        per_second  = per_second,
    })
end

local function handle_stop(id)
    if not shared:get(key(id, "state")) then return reply(404, {error = "unknown run"}) end
    shared:set(key(id, "stop"), true, KEEP_SECS)
    return reply(200, {run_id = id, stopping = true})
end

function _M.access(conf, ctx)
    local uri = ngx.var.uri
    if uri:sub(1, #PREFIX) ~= PREFIX then return ngx.exit(404) end
    local action, id = uri:sub(#PREFIX + 1):match("^(%a+)/?(.*)$")
    local method = ngx.req.get_method()

    if action == "start" then
        if method ~= "POST" then return ngx.exit(405) end
        return handle_start()
    end
    if not shared then
        return reply(503, {error = "shared dict babelsuite_traffic is not declared in the sidecar config"})
    end
    if action == "status" and method == "GET" then return handle_status(id) end
    if action == "result" and method == "GET" then return handle_result(id) end
    if action == "stop" and method == "POST" then return handle_stop(id) end
    return ngx.exit(404)
end

return _M
`
