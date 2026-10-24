local json = require("cjson")
local http = require("resty.http")

-- cjson encodes an empty Lua table as {}, which the runner cannot decode into
-- a findings array — so a step where everything passed is exactly the case
-- that breaks. Same guard the consumer-lag plugin uses.
local function as_array(t) return #t == 0 and json.empty_array or t end

-- One copy of the default, because it is both sent to the endpoint and
-- reported as a span attribute. Two literals would eventually disagree and the
-- trace would name a model the plugin never called. Matches schema.cue.
local DEFAULT_MODEL = "gpt-4o-mini"

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "llm-judge"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- The model answers each criterion independently rather than emitting one
-- overall score. Small yes/no judgements are far more repeatable than a 1-10
-- rating, and each "no" carries its own reason straight into a finding.
-- The shape is shown as a filled-in example, not a template with placeholders.
-- Small models copy a literal "yes|no" or "<one sentence>" straight through
-- into their answer, which scores every criterion as a failure.
local SYSTEM_PROMPT = [[
You grade text against criteria. Answer each criterion independently.
Reply with JSON only: no prose, no markdown fences.
Return one object with a "verdicts" array holding one entry per criterion.
Each entry has "criterion" (copied exactly from the list), "verdict", and
"reason". The "verdict" field must be the word yes or the word no, nothing else.

Example of a well-formed reply:
{"verdicts":[{"criterion":"mentions a price","verdict":"yes","reason":"it quotes $40"},{"criterion":"names a courier","verdict":"no","reason":"no carrier is named"}]}

Answer yes only when the text clearly satisfies the criterion. When it is
ambiguous or unsupported, answer no and say why.
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

    local request = {
        model = cfg.model or DEFAULT_MODEL,
        messages = {
            {role = "system", content = SYSTEM_PROMPT},
            {role = "user",   content = prompt},
        },
        temperature = 0,
    }

    -- Ask the server to constrain decoding to JSON. Instructions alone are not
    -- enough: models wrap the object in prose or stop mid-string, and the reply
    -- is then unparseable. Set structured=false for an endpoint that rejects
    -- the field.
    if cfg.structured ~= false then
        request.response_format = {type = "json_object"}
    end

    local body = json.encode(request)

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
        -- Quote what actually came back. Without it the only way to tell a
        -- prose preamble from a truncated object from a renamed field is to
        -- reproduce the call by hand against the same model.
        local snippet = tostring(content):gsub("%s+", " ")
        if #snippet > 300 then snippet = snippet:sub(1, 300) .. "..." end
        return nil, "judge model did not return the expected verdicts shape; got: " .. snippet
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
        findings = as_array(findings),
        summary  = string.format(
            "judge scored %.2f (%d/%d criteria) against a threshold of %.2f",
            score, passed_count, total, threshold
        ),
        level = (#findings == 0) and "info" or (severity == "warn" and "warn" or "error"),
        -- Added to this step's runner.run trace span. Keep these low-cardinality:
        -- the score and the model, never the prompt or a per-call id.
        attributes = {
            ["judge.status"]    = "scored",
            ["judge.score"]     = string.format("%.2f", score),
            ["judge.passed"]    = tostring(passed_count),
            ["judge.total"]     = tostring(total),
            ["judge.threshold"] = string.format("%.2f", threshold),
            ["judge.model"]     = tostring(cfg.model or DEFAULT_MODEL),
        },
    }, nil
end

-- run_consistency: grades the same text N times and fails when the judgements
-- disagree. A judge whose verdict flips between runs cannot gate a pipeline;
-- this measures that instability directly instead of trusting a single pass.
local function run_consistency(cfg, severity)
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
    local rounds   = tonumber(cfg.rounds) or 3
    local prompt   = build_prompt(output, criteria)

    local scores = {}
    for _ = 1, rounds do
        local verdicts, err = call_model(cfg, prompt)
        if err then return nil, err end
        local _, passed_count = score_verdicts(verdicts, criteria, severity)
        scores[#scores + 1] = #criteria > 0 and (passed_count / #criteria) or 0
    end

    local lo, hi, sum = scores[1], scores[1], 0
    for _, s in ipairs(scores) do
        if s < lo then lo = s end
        if s > hi then hi = s end
        sum = sum + s
    end
    local spread = hi - lo
    local mean   = sum / rounds

    local max_spread = tonumber(cfg.max_spread) or 0.0
    local findings = {}
    if spread > max_spread then
        table.insert(findings, {
            label    = "judge.inconsistent",
            severity = severity,
            detail   = string.format("score ranged %.2f–%.2f across %d rounds (max spread %.2f)",
                lo, hi, rounds, max_spread),
        })
    end

    local ok = #findings == 0
    return {
        passed   = ok or severity == "warn",
        findings = as_array(findings),
        summary  = string.format("consistency: mean %.2f, spread %.2f over %d rounds", mean, spread, rounds),
        level    = ok and "info" or (severity == "warn" and "warn" or "error"),
        attributes = {
            ["judge.status"] = "scored",
            ["judge.mean"]   = string.format("%.2f", mean),
            ["judge.spread"] = string.format("%.2f", spread),
            ["judge.rounds"] = tostring(rounds),
            ["judge.model"]  = tostring(cfg.model or DEFAULT_MODEL),
        },
    }, nil
end

-- run_compare: grades two candidate texts against the same criteria and reports
-- which scored higher. A finding is raised when the preferred side (default
-- "a", the baseline) does not win, so a regression in a new prompt or model is
-- visible as a step failure.
local function run_compare(cfg, severity)
    local a, b = cfg.output_a or "", cfg.output_b or ""
    if a == "" or b == "" then
        return nil, "compare needs both output_a= and output_b="
    end
    local criteria = cfg.criteria or {}

    local function grade(text)
        local verdicts, err = call_model(cfg, build_prompt(text, criteria))
        if err then return nil, err end
        local _, passed_count = score_verdicts(verdicts, criteria, severity)
        return #criteria > 0 and (passed_count / #criteria) or 0, nil
    end

    local score_a, err = grade(a)
    if err then return nil, err end
    local score_b
    score_b, err = grade(b)
    if err then return nil, err end

    local prefer = cfg.prefer or "a"
    local winner = score_a == score_b and "tie" or (score_a > score_b and "a" or "b")

    local findings = {}
    if prefer ~= winner and winner ~= "tie" then
        table.insert(findings, {
            label    = "judge.compare.regression",
            severity = severity,
            detail   = string.format("preferred '%s' but '%s' scored higher (a=%.2f b=%.2f)",
                prefer, winner, score_a, score_b),
        })
    end

    local ok = #findings == 0
    return {
        passed   = ok or severity == "warn",
        findings = as_array(findings),
        summary  = string.format("compare: a=%.2f b=%.2f winner=%s", score_a, score_b, winner),
        level    = ok and "info" or (severity == "warn" and "warn" or "error"),
        attributes = {
            ["judge.status"]  = "scored",
            ["judge.score_a"] = string.format("%.2f", score_a),
            ["judge.score_b"] = string.format("%.2f", score_b),
            ["judge.winner"]  = winner,
            ["judge.model"]   = tostring(cfg.model or DEFAULT_MODEL),
        },
    }, nil
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
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

    local result, err
    if op == "consistency" then
        result, err = run_consistency(cfg, severity)
    elseif op == "compare" then
        result, err = run_compare(cfg, severity)
    else
        result, err = run_judge(cfg, severity)
    end
    if err then
        -- A model that is unreachable, or that answers unusably, is a step
        -- outcome rather than a transport fault. Reporting it as a 200 with
        -- passed=false lets the finding, the summary and the span attributes
        -- all reach the run; a 500 discards every one of them, and it also
        -- broke monitor's promise never to block the suite.
        result = {
            passed   = (op == "monitor"),
            findings = {{
                label    = "judge.unavailable",
                severity = (op == "monitor") and "warn" or "critical",
                detail   = err,
            }},
            summary = "judge did not produce a score: " .. err,
            level   = (op == "monitor") and "warn" or "error",
            attributes = {
                ["judge.status"] = "unavailable",
                ["judge.model"]  = tostring(cfg.model or DEFAULT_MODEL),
            },
        }
    end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
