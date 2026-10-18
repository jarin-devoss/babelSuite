local cjson = require("cjson")
local http = require("resty.http")

-- A private cjson instance, so the array setting below cannot leak into other
-- plugins sharing this worker. Decoded arrays keep an array marker: a skill's
-- parameter schema with `"required": []` must go back out as [] and not {},
-- or the model endpoint rejects the tool definition.
local json = cjson.new()
json.decode_array_with_array_mt(true)

-- cjson encodes an empty Lua table as {}, which the runner cannot decode into
-- a findings array — so a step where everything passed is exactly the case
-- that breaks. Same guard the consumer-lag plugin uses.
local function as_array(t) return #t == 0 and json.empty_array or t end

-- One copy of the default, because it is both sent to the endpoint and
-- reported as a span attribute. Matches schema.cue.
local DEFAULT_MODEL = "gpt-4o-mini"

local _M = {}
_M.version  = 0.1
_M.priority = 10
_M.name     = "skill-eval"
_M.schema   = {type = "object", properties = {}, additionalProperties = false}

-- Starlark None arrives as JSON null, which cjson decodes to a sentinel rather
-- than nil. A case that expects no skill is written either way.
local function blank(v) return v == nil or v == json.null or v == "" end

local function list(v) return type(v) == "table" and v or {} end

-- Findings name the case by position and the start of its prompt. The whole
-- prompt would bury the verdict; the index alone makes the reader count.
local function case_label(i, case)
    local prompt = tostring(case.prompt or ""):gsub("%s+", " ")
    if #prompt > 60 then prompt = prompt:sub(1, 57) .. "..." end
    return string.format("case %d (%q)", i, prompt)
end

-- post_chat sends one chat-completions request. Token usage, when the server
-- reports it, is added to cfg._usage so the step can say what the eval cost.
local function post_chat(cfg, url, api_key, request)
    local headers = {["Content-Type"] = "application/json"}
    if not blank(api_key) then headers["Authorization"] = "Bearer " .. api_key end

    local httpc = http.new()
    httpc:set_timeout(cfg.timeout_ms or 30000)
    local res, err = httpc:request_uri(url, {
        method  = "POST",
        body    = json.encode(request),
        headers = headers,
    })
    if err then return nil, "model unreachable: " .. err end
    if res.status < 200 or res.status >= 300 then
        return nil, "model returned HTTP " .. res.status .. ": " .. (res.body or "")
    end

    local ok, decoded = pcall(json.decode, res.body or "")
    if not ok or type(decoded) ~= "table" then
        return nil, "model returned a non-JSON response"
    end
    if type(decoded.usage) == "table" and cfg._usage then
        cfg._usage.prompt     = cfg._usage.prompt + (tonumber(decoded.usage.prompt_tokens) or 0)
        cfg._usage.completion = cfg._usage.completion + (tonumber(decoded.usage.completion_tokens) or 0)
        cfg._usage.reported   = true
    end
    local choice = decoded.choices and decoded.choices[1]
    local message = choice and choice.message
    if type(message) ~= "table" then
        return nil, "model response carried no message"
    end
    return message, nil
end

-- ── The agent under test ────────────────────────────────────────────────────

-- Each skill is offered to the model as a function tool. Its description is
-- the only thing the model has to route on, which is exactly what `trigger`
-- measures: whether the descriptions tell the skills apart.
local function skill_tools(skills)
    local tools = {}
    for _, skill in ipairs(skills) do
        tools[#tools + 1] = {
            type = "function",
            ["function"] = {
                name        = skill.name,
                description = blank(skill.description) and "" or skill.description,
                parameters  = skill.parameters or {type = "object", properties = {}},
            },
        }
    end
    return tools
end

local function agent_messages(system, prompt)
    local messages = {}
    if not blank(system) then messages[1] = {role = "system", content = system} end
    messages[#messages + 1] = {role = "user", content = prompt}
    return messages
end

-- skill_calls lists every skill the model called, in order, as {name, arguments}.
-- An empty list means it answered in prose.
local function skill_calls(message)
    local calls = {}
    for _, call in ipairs(list(message.tool_calls)) do
        local fn = type(call) == "table" and call["function"]
        if type(fn) == "table" and not blank(fn.name) then
            calls[#calls + 1] = {name = fn.name, arguments = fn.arguments}
        end
    end
    -- Older servers still answer with the pre-tools function_call field.
    if #calls == 0 and type(message.function_call) == "table" and not blank(message.function_call.name) then
        calls[1] = {name = message.function_call.name, arguments = message.function_call.arguments}
    end
    return calls
end

local function now_ms()
    ngx.update_time()
    return ngx.now() * 1000
end

-- ask_agent returns every call the model made and how long it took to answer.
-- Temperature is 0 unless the operation is measuring variation on purpose.
local function ask_agent(cfg, prompt, temperature)
    local started = now_ms()
    local message, err = post_chat(cfg, cfg.model_url, cfg.api_key, {
        model       = cfg.model or DEFAULT_MODEL,
        messages    = agent_messages(cfg.system, prompt),
        tools       = skill_tools(cfg.skills),
        temperature = temperature or 0,
    })
    if err then return nil, nil, err end
    return skill_calls(message), now_ms() - started, nil
end

-- first_call returns the skill the agent reached for first and its raw
-- arguments, or nil when it answered in prose. trigger and arguments only
-- judge that first choice; sequence looks at all of them.
local function first_call(calls)
    local call = calls[1]
    if not call then return nil, nil end
    return call.name, call.arguments
end

-- route_finding compares the chosen skill with the expected one. It returns
-- nil when the case passed.
local function route_finding(label, expect, chosen, severity)
    if blank(expect) then
        if chosen == nil then return nil end
        return {label = "skill.false_trigger", severity = severity,
                detail = label .. " — expected no skill, the model called " .. chosen}
    end
    if chosen == nil then
        return {label = "skill.missed", severity = severity,
                detail = label .. " — expected " .. expect .. ", the model called no skill"}
    end
    if chosen ~= expect then
        return {label = "skill.misrouted", severity = severity,
                detail = label .. " — expected " .. expect .. ", the model called " .. chosen}
    end
    return nil
end

-- ── trigger / monitor ───────────────────────────────────────────────────────

local function eval_trigger(cfg, case, label, severity)
    local calls, _, err = ask_agent(cfg, case.prompt)
    if err then return nil, err end
    local finding = route_finding(label, case.expect, first_call(calls), severity)
    return finding and {finding} or {}, nil
end

-- ── consistency ─────────────────────────────────────────────────────────────

-- eval_consistency asks the same prompt several times at a non-zero
-- temperature. One correct answer at temperature 0 says nothing about an agent
-- that is sampled in production; a skill picked 3 times in 5 is a coin flip
-- with a good description.
--
-- The step's score is pass^k: a case passes only when every sample is right.
-- pass@k — right at least once — is reported beside it, because the gap
-- between the two is how unreliable the routing is.
local function eval_consistency(cfg, case, label, severity, _, stats)
    local samples = cfg.samples or 5
    local temperature = cfg.temperature or 0.7
    local tally, order, correct = {}, {}, 0
    for _ = 1, samples do
        local calls, _, err = ask_agent(cfg, case.prompt, temperature)
        if err then return nil, err end
        local chosen = first_call(calls)
        if route_finding(label, case.expect, chosen, severity) == nil then correct = correct + 1 end
        local key = chosen or "no skill"
        if not tally[key] then tally[key] = 0; order[#order + 1] = key end
        tally[key] = tally[key] + 1
    end
    if correct > 0 then stats.any_correct = stats.any_correct + 1 end
    if correct == samples then return {}, nil end

    local spread = {}
    for _, key in ipairs(order) do spread[#spread + 1] = string.format("%s %d/%d", key, tally[key], samples) end
    return {{
        label = "skill.unstable", severity = severity,
        detail = string.format("%s — expected %s every time, got %s",
            label, blank(case.expect) and "no skill" or case.expect, table.concat(spread, ", ")),
    }}, nil
end

-- ── sequence ────────────────────────────────────────────────────────────────

-- in_order reports whether expected appears in got in the same order, with
-- other calls allowed around and between its steps.
local function in_order(expected, got)
    local next_step = 1
    for _, name in ipairs(got) do
        if name == expected[next_step] then next_step = next_step + 1 end
    end
    return next_step > #expected
end

-- eval_sequence checks every skill the model called, in order. Some prompts
-- need more than one skill — look the order up, then refund it — and a model
-- that only makes the first call passes trigger while leaving the job undone.
--
-- sequence_match = "exact" (the default) wants those calls and nothing else.
-- "in_order" only wants the listed calls to happen in that order: an agent
-- that also checks shipping on the way still did the job, and an exact match
-- would fail it for taking a valid route.
local function eval_sequence(cfg, case, label, severity)
    local calls, _, err = ask_agent(cfg, case.prompt)
    if err then return nil, err end
    local got = {}
    for i, call in ipairs(calls) do got[i] = call.name end

    local same
    if cfg.sequence_match == "in_order" then
        same = in_order(case.sequence, got)
    else
        same = #got == #case.sequence
        for i = 1, #case.sequence do
            if got[i] ~= case.sequence[i] then same = false end
        end
    end
    if same then return {}, nil end

    local function show_list(names) return #names == 0 and "no skills" or table.concat(names, " → ") end
    return {{
        label = "skill.wrong_sequence", severity = severity,
        detail = label .. " — expected " .. show_list(case.sequence) .. ", the model called " .. show_list(got),
    }}, nil
end

-- ── latency ─────────────────────────────────────────────────────────────────

-- eval_latency times each answer against max_latency_ms. Routing is still
-- checked, so a fast wrong answer does not count as a pass.
local function eval_latency(cfg, case, label, severity, _, stats)
    local calls, elapsed, err = ask_agent(cfg, case.prompt)
    if err then return nil, err end
    stats.timings[#stats.timings + 1] = elapsed

    local findings = {}
    local limit = cfg.max_latency_ms or 5000
    if elapsed > limit then
        findings[1] = {
            label = "skill.slow", severity = severity,
            detail = string.format("%s — answered in %d ms, over the %d ms budget",
                label, math.floor(elapsed), limit),
        }
    end
    local routed = route_finding(label, case.expect, first_call(calls), severity)
    if routed then findings[#findings + 1] = routed end
    return findings, nil
end

-- ── arguments ───────────────────────────────────────────────────────────────

-- matches reports whether the model's value satisfies the expected one.
-- Objects match on the expected keys only, so a case pins the arguments it
-- cares about without restating every optional one. Arrays match exactly.
-- Numbers compare by value, because models often send 42 as "42".
local function matches(expected, actual)
    if expected == json.null then return actual == nil or actual == json.null end
    if type(expected) == "number" then return tonumber(actual) == expected end
    if type(expected) ~= "table" then return expected == actual end
    if type(actual) ~= "table" then return false end
    if getmetatable(expected) == json.array_mt then
        if #expected ~= #actual then return false end
        for i = 1, #expected do
            if not matches(expected[i], actual[i]) then return false end
        end
        return true
    end
    for key, value in pairs(expected) do
        if not matches(value, actual[key]) then return false end
    end
    return true
end

local function show(v)
    if v == nil or v == json.null then return "absent" end
    return json.encode(v)
end

-- JSON schema types, judged the way the API behind a tool would: "1001" is not
-- an integer, even though matches() above accepts it as the value 1001.
local function schema_type(value)
    if type(value) == "string" or type(value) == "boolean" then return type(value) end
    if type(value) == "number" then return value == math.floor(value) and "integer" or "number" end
    if type(value) == "table" then
        return getmetatable(value) == json.array_mt and "array" or "object"
    end
    return "null"
end

local function type_ok(want, value)
    local got = schema_type(value)
    if want == "number" then return got == "number" or got == "integer" end
    return got == want
end

-- schema_problems checks the arguments against the skill's parameter schema:
-- required keys, declared types, and enums. It is the subset of JSON schema
-- tool definitions actually use, not a full validator. A model that is right
-- about the value but sends it in the wrong shape still breaks the real tool.
local function schema_problems(schema, args)
    local problems = {}
    if type(schema) ~= "table" then return problems end
    for _, key in ipairs(list(schema.required)) do
        if args[key] == nil or args[key] == json.null then
            problems[#problems + 1] = key .. " is required but missing"
        end
    end
    local keys = {}
    for key in pairs(list(schema.properties)) do keys[#keys + 1] = key end
    table.sort(keys)
    for _, key in ipairs(keys) do
        local prop, value = schema.properties[key], args[key]
        if type(prop) == "table" and value ~= nil and value ~= json.null then
            if type(prop.type) == "string" and not type_ok(prop.type, value) then
                problems[#problems + 1] = string.format("%s should be %s, got %s %s",
                    key, prop.type, schema_type(value), json.encode(value))
            elseif type(prop.enum) == "table" then
                local allowed = false
                for _, option in ipairs(prop.enum) do
                    if option == value then allowed = true end
                end
                if not allowed then
                    problems[#problems + 1] = string.format("%s is %s, not one of %s",
                        key, json.encode(value), json.encode(prop.enum))
                end
            end
        end
    end
    return problems
end

local function eval_arguments(cfg, case, label, severity, skills_by_name)
    local calls, _, err = ask_agent(cfg, case.prompt)
    if err then return nil, err end
    local chosen, raw = first_call(calls)

    local routed = route_finding(label, case.expect, chosen, severity)
    if routed then return {routed}, nil end

    local args = raw
    if type(raw) == "string" then
        local ok, decoded = pcall(json.decode, raw ~= "" and raw or "{}")
        if not ok or type(decoded) ~= "table" then
            return {{label = "skill.bad_args", severity = severity,
                     detail = label .. " — arguments are not a JSON object: " .. raw:sub(1, 200)}}, nil
        end
        args = decoded
    elseif type(raw) ~= "table" then
        args = {}
    end

    local findings = {}
    if cfg.check_schema ~= false then
        for _, problem in ipairs(schema_problems(skills_by_name[chosen].parameters, args)) do
            findings[#findings + 1] = {
                label = "skill.schema_violation", severity = severity,
                detail = label .. " — " .. chosen .. "." .. problem,
            }
        end
    end
    -- Sorted so a failing case reports its keys in the same order every run.
    local keys = {}
    for key in pairs(case.args) do keys[#keys + 1] = key end
    table.sort(keys)
    for _, key in ipairs(keys) do
        local want = case.args[key]
        if not matches(want, args[key]) then
            findings[#findings + 1] = {
                label = "skill.wrong_args", severity = severity,
                detail = string.format("%s — %s.%s: expected %s, got %s",
                    label, chosen, key, show(want), show(args[key])),
            }
        end
    end
    return findings, nil
end

-- ── outcome ─────────────────────────────────────────────────────────────────

-- The grading prompt is the one llm-judge uses, for the same reason: small
-- yes/no judgements are far more repeatable than a rating, and the example is
-- filled in because small models copy placeholders straight through.
local JUDGE_PROMPT = [[
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

-- Models often wrap JSON in markdown fences despite instructions not to.
local function strip_fences(text)
    return text:match("```%a*%s*(.-)%s*```") or text
end

local function judge(cfg, answer, criteria)
    local lines = {"Criteria:"}
    for i, criterion in ipairs(criteria) do lines[#lines + 1] = i .. ". " .. criterion end
    lines[#lines + 1] = ""
    lines[#lines + 1] = "Text to grade:"
    lines[#lines + 1] = answer

    local request = {
        model    = blank(cfg.judge_model) and (cfg.model or DEFAULT_MODEL) or cfg.judge_model,
        messages = {
            {role = "system", content = JUDGE_PROMPT},
            {role = "user",   content = table.concat(lines, "\n")},
        },
        temperature = 0,
    }
    if cfg.structured ~= false then request.response_format = {type = "json_object"} end

    local url = blank(cfg.judge_url) and cfg.model_url or cfg.judge_url
    local key = blank(cfg.judge_api_key) and cfg.api_key or cfg.judge_api_key
    local message, err = post_chat(cfg, url, key, request)
    if err then return nil, "judge " .. err end

    local content = message.content
    if type(content) ~= "string" or content == "" then
        return nil, "judge response carried no message content"
    end
    local ok, parsed = pcall(json.decode, strip_fences(content))
    if not ok or type(parsed) ~= "table" or type(parsed.verdicts) ~= "table" then
        local snippet = content:gsub("%s+", " ")
        if #snippet > 300 then snippet = snippet:sub(1, 300) .. "..." end
        return nil, "judge did not return the expected verdicts shape; got: " .. snippet
    end
    return parsed.verdicts, nil
end

-- answer_and_grade has the agent answer a case's prompt under the given system
-- prompt, with no tools, and has the judge grade the answer. It returns the
-- findings, and how many criteria the answer met.
local function answer_and_grade(cfg, system, case, label, severity)
    local message, err = post_chat(cfg, cfg.model_url, cfg.api_key, {
        model       = cfg.model or DEFAULT_MODEL,
        messages    = agent_messages(system, case.prompt),
        temperature = 0,
    })
    if err then return nil, nil, err end

    local answer = message.content
    if type(answer) ~= "string" or answer == "" then
        return {{label = "skill.no_answer", severity = severity,
                 detail = label .. " — the model returned no text"}}, 0, nil
    end

    local verdicts, jerr = judge(cfg, answer, case.criteria)
    if jerr then return nil, nil, jerr end

    local findings, seen, met = {}, {}, 0
    for _, verdict in ipairs(verdicts) do
        local criterion = verdict.criterion or "(unnamed criterion)"
        seen[criterion] = true
        if tostring(verdict.verdict or ""):lower() == "yes" then
            met = met + 1
        else
            findings[#findings + 1] = {
                label = "skill.criterion_failed", severity = severity,
                detail = label .. " — " .. criterion .. " — " .. tostring(verdict.reason or "no reason given"),
            }
        end
    end
    -- A criterion the judge skipped is not a pass.
    for _, criterion in ipairs(case.criteria) do
        if not seen[criterion] then
            findings[#findings + 1] = {
                label = "skill.unanswered", severity = severity,
                detail = label .. " — " .. criterion .. " — the judge returned no verdict",
            }
        end
    end
    return findings, met, nil
end

local function with_skill(cfg, skill)
    if blank(cfg.system) then return skill.instructions end
    return cfg.system .. "\n\n" .. skill.instructions
end

-- eval_outcome loads the expected skill's instructions as the system prompt,
-- the way an agent reads a skill's body once it has chosen it, and grades the
-- answer. Routing is not retested here; trigger covers that.
local function eval_outcome(cfg, case, label, severity, skills_by_name)
    local skill = skills_by_name[case.expect]
    local findings, _, err = answer_and_grade(cfg, with_skill(cfg, skill), case, label, severity)
    return findings, err
end

-- eval_uplift answers each case twice — once with the skill's instructions
-- loaded and once without — and grades both. A skill earns its place only if
-- the answer gets better: when the base model already meets the criteria
-- without it, the skill costs context and adds nothing, and the step should
-- say so. A case passes when the skill gains at least min_gain criteria.
local function eval_uplift(cfg, case, label, severity, skills_by_name, stats)
    local skill = skills_by_name[case.expect]
    local _, with_met, err = answer_and_grade(cfg, with_skill(cfg, skill), case, label, severity)
    if err then return nil, err end
    local _, without_met, berr = answer_and_grade(cfg, cfg.system, case, label, severity)
    if berr then return nil, berr end

    local gain = with_met - without_met
    stats.gain = stats.gain + gain
    if gain >= (cfg.min_gain or 1) then return {}, nil end

    local total = #case.criteria
    local why = ""
    if gain < 0 then
        why = "; the skill makes the answer worse"
    elseif without_met == total then
        why = "; the base model already does this"
    end
    return {{
        label = gain < 0 and "skill.regression" or "skill.no_uplift", severity = severity,
        detail = string.format("%s — with %s %d/%d criteria, without it %d/%d%s",
            label, case.expect, with_met, total, without_met, total, why),
    }}, nil
end

-- ── config ──────────────────────────────────────────────────────────────────

-- Plugin config only carries strings, numbers, bools and lists, so plugin.star
-- flattens the suite's skill and case dicts into parallel lists. This zips
-- them back into one table per skill and per case.
local function normalize(cfg)
    local skills = {}
    for i, name in ipairs(list(cfg.skill_names)) do
        local skill = {
            name         = name,
            description  = list(cfg.skill_descriptions)[i],
            instructions = list(cfg.skill_instructions)[i],
        }
        local schema = list(cfg.skill_parameters)[i]
        if not blank(schema) then
            local ok, decoded = pcall(json.decode, schema)
            if not ok or type(decoded) ~= "table" then
                return nil, "skill " .. tostring(name) .. ": parameters is not a JSON schema object"
            end
            skill.parameters = decoded
        end
        skills[i] = skill
    end

    local cases = {}
    for i, prompt in ipairs(list(cfg.case_prompts)) do
        local args
        local arg_pairs = list(list(cfg.case_args)[i])
        if #arg_pairs > 0 then
            args = {}
            for _, pair in ipairs(arg_pairs) do
                if type(pair) == "table" and type(pair[1]) == "string" then args[pair[1]] = pair[2] end
            end
        end
        local expect = list(cfg.case_expects)[i]
        expect = not blank(expect) and expect or nil
        -- A case without an explicit sequence expects just its one skill, or
        -- no call at all, so the same cases can be reused across operations.
        local sequence = list(cfg.case_sequences)[i]
        if type(sequence) ~= "table" or #sequence == 0 then sequence = {expect} end
        cases[i] = {
            prompt   = prompt,
            expect   = expect,
            args     = args,
            criteria = list(list(cfg.case_criteria)[i]),
            sequence = sequence,
        }
    end

    cfg.skills, cfg.cases = skills, cases
    return cfg, nil
end

-- validate rejects config that cannot be evaluated before any model is called.
-- An expected skill that is not in the list is a typo in the suite, and
-- scoring it as a misroute would blame the model for it.
local function validate(op, cfg)
    if blank(cfg.model_url) then return nil, "model_url is required" end
    if #cfg.skills == 0 then return nil, "skills must be a non-empty list" end
    if #cfg.cases == 0 then return nil, "cases must be a non-empty list" end

    local by_name = {}
    for i, skill in ipairs(cfg.skills) do
        if blank(skill.name) then return nil, "skill " .. i .. " has no name" end
        by_name[skill.name] = skill
    end

    for i, case in ipairs(cfg.cases) do
        if blank(case.prompt) then return nil, "case " .. i .. " has no prompt" end
        if case.expect and not by_name[case.expect] then
            return nil, "case " .. i .. " expects unknown skill " .. tostring(case.expect)
        end
        for _, name in ipairs(case.sequence) do
            if not by_name[name] then
                return nil, "case " .. i .. " sequence names unknown skill " .. tostring(name)
            end
        end
        if op == "arguments" then
            if not case.expect or not case.args then
                return nil, "arguments: case " .. i .. " needs expect and args"
            end
        elseif op == "outcome" or op == "uplift" then
            if not case.expect or #case.criteria == 0 then
                return nil, op .. ": case " .. i .. " needs expect and a non-empty criteria list"
            end
            if blank(by_name[case.expect].instructions) then
                return nil, op .. ": skill " .. case.expect .. " has no instructions to follow"
            end
        end
    end
    if op == "consistency" and (cfg.samples or 5) < 1 then
        return nil, "consistency: samples must be at least 1"
    end
    return by_name, nil
end

-- ── dispatch ────────────────────────────────────────────────────────────────

local EVALUATORS = {
    trigger     = eval_trigger,
    monitor     = eval_trigger,
    arguments   = eval_arguments,
    outcome     = eval_outcome,
    consistency = eval_consistency,
    sequence    = eval_sequence,
    latency     = eval_latency,
    uplift      = eval_uplift,
}

local OPERATIONS = "trigger, arguments, outcome, uplift, consistency, sequence, latency or monitor"

local function run(op, cfg, severity, skills_by_name)
    local evaluate = EVALUATORS[op]
    local findings, passed_count = {}, 0
    local stats = {timings = {}, any_correct = 0, gain = 0}
    cfg._usage = {prompt = 0, completion = 0, reported = false}
    for i, case in ipairs(cfg.cases) do
        local case_findings, err = evaluate(cfg, case, case_label(i, case), severity, skills_by_name, stats)
        -- A model that cannot be reached fails every remaining case the same
        -- way, so the first error ends the run instead of repeating N times.
        if err then return nil, err end
        if #case_findings == 0 then passed_count = passed_count + 1 end
        for _, f in ipairs(case_findings) do findings[#findings + 1] = f end
    end

    local total = #cfg.cases
    local score = passed_count / total
    local threshold = cfg.threshold or 1.0
    local passed = severity == "warn" or score >= threshold

    local attributes = {
        ["skill.op"]        = op,
        ["skill.status"]    = "scored",
        ["skill.score"]     = string.format("%.2f", score),
        ["skill.passed"]    = tostring(passed_count),
        ["skill.total"]     = tostring(total),
        ["skill.threshold"] = string.format("%.2f", threshold),
        ["skill.skills"]    = tostring(#cfg.skills),
        ["skill.model"]     = tostring(cfg.model or DEFAULT_MODEL),
    }
    -- Only when the server reported usage: a 0 would read as a free eval.
    if cfg._usage.reported then
        attributes["skill.tokens_prompt"]     = tostring(cfg._usage.prompt)
        attributes["skill.tokens_completion"] = tostring(cfg._usage.completion)
    end
    if op == "consistency" then
        attributes["skill.samples"]     = tostring(cfg.samples or 5)
        attributes["skill.temperature"] = string.format("%.2f", cfg.temperature or 0.7)
        attributes["skill.pass_at_k"]   = string.format("%.2f", stats.any_correct / total)
    elseif op == "uplift" then
        attributes["skill.uplift_avg"] = string.format("%.2f", stats.gain / total)
    elseif op == "latency" then
        local slowest = 0
        for _, ms in ipairs(stats.timings) do slowest = math.max(slowest, ms) end
        attributes["skill.latency_max_ms"] = tostring(math.floor(slowest))
        attributes["skill.latency_budget_ms"] = tostring(cfg.max_latency_ms or 5000)
    end

    return {
        passed   = passed,
        findings = as_array(findings),
        summary  = string.format(
            "%s scored %.2f (%d/%d cases) against a threshold of %.2f",
            op, score, passed_count, total, threshold
        ),
        level = (#findings == 0) and "info" or (severity == "warn" and "warn" or "error"),
        -- Added to this step's runner.run trace span. Scores, counts and names
        -- only — never a prompt or an answer.
        attributes = attributes,
    }, nil
end

local function reject(message)
    ngx.status = 400
    ngx.say(json.encode({error = message}))
    return ngx.exit(400)
end

function _M.access(conf, ctx)
    if ngx.var.uri ~= "/_babelsuite/plugins/" .. _M.name .. "/start" then return ngx.exit(404) end
    if ngx.req.get_method() ~= "POST" then return ngx.exit(405) end

    ngx.req.read_body()
    local ok, payload = pcall(json.decode, ngx.req.get_body_data() or "{}")
    if not ok then return reject("invalid json") end

    local op  = payload.op or "trigger"
    local cfg = payload.config or {}
    if not EVALUATORS[op] then
        return reject("unknown operation " .. tostring(op) .. "; use " .. OPERATIONS)
    end

    local _, nerr = normalize(cfg)
    if nerr then return reject(nerr) end
    local skills_by_name, verr = validate(op, cfg)
    if verr then return reject(verr) end

    -- monitor reports routing at warn level and never blocks the suite.
    local severity = (op == "monitor") and "warn" or (cfg.severity or "critical")

    local result, err = run(op, cfg, severity, skills_by_name)
    if err then
        -- Unreachable or unusable models are a step outcome, not a transport
        -- fault: a 200 with passed=false keeps the finding, summary and span
        -- attributes, where a 500 would discard them and break monitor.
        result = {
            passed   = (op == "monitor"),
            findings = {{
                label    = "skill.unavailable",
                severity = (op == "monitor") and "warn" or "critical",
                detail   = err,
            }},
            summary = op .. " did not produce a score: " .. err,
            level   = (op == "monitor") and "warn" or "error",
            attributes = {
                ["skill.op"]     = op,
                ["skill.status"] = "unavailable",
                ["skill.model"]  = tostring(cfg.model or DEFAULT_MODEL),
            },
        }
    end

    ngx.status = 200
    ngx.header["Content-Type"] = "application/json"
    ngx.say(json.encode(result))
    return ngx.exit(200)
end

return _M
