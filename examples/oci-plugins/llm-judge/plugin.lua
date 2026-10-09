local json = require("cjson")
local http = require("resty.http")

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "llm-judge"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- The model answers each criterion independently rather than emitting one
-- overall score. Small yes/no judgements are far more repeatable than a 1-10
-- rating, and each "no" carries its own reason straight into a finding.
local SYSTEM_PROMPT = [[
You grade text against criteria. Answer each criterion independently.
Reply with JSON only, no prose and no markdown fences, in exactly this shape:
{"verdicts":[{"criterion":"<verbatim criterion>","verdict":"yes|no","reason":"<one sentence>"}]}
Answer "yes" only when the text clearly satisfies the criterion. When it is
ambiguous or unsupported, answer "no" and say why.
]]

local function fetch_body(target, timeout_ms)
    local httpc = http.new()
    httpc:set_timeout(timeout_ms)
    local res, err = httpc:request_uri(target, {method = "GET"})
    if err then return nil, "could not fetch target: " .. err end
    return res.body or "", nil
end

-- Models often wrap JSON in markdown fences despite instructions not to.
local function strip_fences(text)
    local inner = text:match("```%a*%s*(.-)%s*```")
    return inner or text
end

local function build_prompt(output, criteria)
    local lines = {"Criteria:"}
    for i, criterion in ipairs(criteria) do
        lines[#lines + 1] = i .. ". " .. criterion
    end
    lines[#lines + 1] = ""
    lines[#lines + 1] = "Text to grade:"
    lines[#lines + 1] = output
    return table.concat(lines, "\n")
end

local function call_model(cfg, prompt)
    local headers = {["Content-Type"] = "application/json"}
    if cfg.api_key and cfg.api_key ~= "" then
        headers["Authorization"] = "Bearer " .. cfg.api_key
    end

    local body = json.encode({
        model = cfg.model or "gpt-4o-mini",
        messages = {
            {role = "system", content = SYSTEM_PROMPT},
            {role = "user",   content = prompt},
        },
        temperature = 0,
    })

    local httpc = http.new()
    httpc:set_timeout(cfg.timeout_ms or 30000)
    local res, err = httpc:request_uri(cfg.model_url, {
        method  = "POST",
        body    = body,
        headers = headers,
    })
    if err then return nil, "judge model unreachable: " .. err end
    if res.status < 200 or res.status >= 300 then
        return nil, "judge model returned HTTP " .. res.status .. ": " .. (res.body or "")
    end

    local ok, decoded = pcall(json.decode, res.body or "")
    if not ok or type(decoded) ~= "table" then
        return nil, "judge model returned a non-JSON response"
    end

    local choice = decoded.choices and decoded.choices[1]
    local content = choice and choice.message and choice.message.content
    if type(content) ~= "string" or content == "" then
        return nil, "judge model response carried no message content"
    end

    local parsed_ok, parsed = pcall(json.decode, strip_fences(content))
    if not parsed_ok or type(parsed) ~= "table" or type(parsed.verdicts) ~= "table" then
        return nil, "judge model did not return the expected verdicts shape"
    end
    return parsed.verdicts, nil
end

-- score_verdicts turns the per-criterion answers into findings and a fraction,
-- so a partial pass is visible instead of collapsing to a single boolean.
local function score_verdicts(verdicts, criteria, severity)
    local findings, passed_count = {}, 0
    local seen = {}

    for _, verdict in ipairs(verdicts) do
        local criterion = verdict.criterion or "(unnamed criterion)"
        seen[criterion] = true
        if tostring(verdict.verdict or ""):lower() == "yes" then
            passed_count = passed_count + 1
        else
            table.insert(findings, {
                label    = "judge.failed",
                severity = severity,
                detail   = criterion .. " — " .. (verdict.reason or "no reason given"),
            })
        end
    end

    -- A criterion the model skipped is not a pass; report it rather than
    -- letting the score silently improve.
    for _, criterion in ipairs(criteria) do
        if not seen[criterion] then
            table.insert(findings, {
                label    = "judge.unanswered",
                severity = severity,
                detail   = criterion .. " — the judge returned no verdict for this criterion",
            })
        end
    end

    return findings, passed_count
end

local function run_judge(cfg, severity)
    local output = cfg.output
    if (output == nil or output == "") and (cfg.target or "") ~= "" then
        local fetched, err = fetch_body(cfg.target, cfg.timeout_ms or 30000)
        if err then return nil, err end
        output = fetched
    end
    if output == nil or output == "" then
        return nil, "nothing to grade: set output= or target="
    end

    local criteria = cfg.criteria or {}
    local verdicts, err = call_model(cfg, build_prompt(output, criteria))
    if err then return nil, err end

    local findings, passed_count = score_verdicts(verdicts, criteria, severity)
    local total = #criteria
    local score = 0
    if total > 0 then score = passed_count / total end

    local threshold = cfg.threshold or 1.0
    local passed = score >= threshold and severity ~= "warn"
    if severity == "warn" then passed = true end

    return {
        passed   = passed,
        findings = findings,
        summary  = string.format(
            "judge scored %.2f (%d/%d criteria) against a threshold of %.2f",
            score, passed_count, total, threshold
        ),
        level = (#findings == 0) and "info" or (severity == "warn" and "warn" or "error"),
        -- Added to this step's runner.run trace span. Keep these low-cardinality:
        -- the score and the model, never the prompt or a per-call id.
        attributes = {
            ["judge.score"]     = string.format("%.2f", score),
            ["judge.passed"]    = tostring(passed_count),
            ["judge.total"]     = tostring(total),
            ["judge.threshold"] = string.format("%.2f", threshold),
            ["judge.model"]     = tostring(cfg.model or "gpt-4o-mini"),
        },
    }, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/llm-judge/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then ngx.status = 400; ngx.say(json.encode({error = "invalid json"})); return ngx.exit(400) end

    local op  = payload.op or "judge"
    local cfg = payload.config or {}

    if (cfg.model_url or "") == "" then
        ngx.status = 400; ngx.say(json.encode({error = "model_url is required"})); return ngx.exit(400)
    end
    if type(cfg.criteria) ~= "table" or #cfg.criteria == 0 then
        ngx.status = 400; ngx.say(json.encode({error = "criteria must be a non-empty list"})); return ngx.exit(400)
    end

    -- monitor reports the same verdicts but never blocks the suite.
    local severity = (op == "monitor") and "warn" or (cfg.severity or "critical")

    local result, err = run_judge(cfg, severity)
    if err then ngx.status = 500; ngx.say(json.encode({error = err})); return ngx.exit(500) end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
