// Skill evaluation plugin config schema.
// Tests an AI agent's skills against an OpenAI-compatible chat-completions
// endpoint: does it pick the right skill (trigger, monitor), pick it every time
// (consistency), call it with the right arguments (arguments), call several in
// the right order (sequence), answer in time (latency), answer well when
// following it (outcome), and answer better than it does without it (uplift).
//
// plugin.star flattens the suite's skill and case dicts into the parallel
// lists below; entry i of every skill_* list describes the same skill, and
// entry i of every case_* list the same case.

model_url: string
model:     string | *"gpt-4o-mini"
api_key?:  string

// Optional system prompt placed before every case. For outcome it is followed
// by the expected skill's instructions.
system?: string

// Skill names are sent as tool names, which endpoints restrict to this shape.
skill_names:        [=~"^[A-Za-z0-9_-]{1,64}$", ...=~"^[A-Za-z0-9_-]{1,64}$"]
skill_descriptions: [...string]
skill_instructions: [...string]
// JSON schema of each skill's arguments, as a string. Empty means no arguments.
skill_parameters:   [...string]

case_prompts: [string, ...string]
// The skill a case should trigger; null means the model should call none.
case_expects: [...(string | null)]
// Expected arguments per case as [key, value] pairs. Objects match on the
// listed keys only; numbers compare by value.
case_args:     [...[...[string, _]]]
// Criteria the answer must meet, graded yes/no by the judge model.
case_criteria: [...[...string]]
// sequence only: every skill the model should call, in order. Empty means the
// case's expect alone, or no call at all.
case_sequences?: [...[...string]]
// sequence only. "exact" wants those calls and nothing else; "in_order" allows
// other calls around and between them.
sequence_match?: "exact" | "in_order" | *"exact"

// arguments only: also check the call against the skill's parameter schema —
// required keys, types and enums.
check_schema?: bool | *true

// uplift only: how many more criteria the answer must meet with the skill's
// instructions loaded than without them.
min_gain?: int & >=0 | *1

// consistency only: how many times each case is asked, and how hot.
samples?:     int & >=1 | *5
temperature?: number & >=0 & <=2 | *0.7

// latency only: the longest a single answer may take.
max_latency_ms?: int & >0 | *5000

// outcome and uplift. Each defaults to its model_* counterpart.
judge_url?:     string
judge_model?:   string
judge_api_key?: string

// Minimum fraction of cases that must pass, 0.0–1.0.
threshold: >=0.0 & <=1.0 | *1.0

timeout_ms: int & >0 | *30000
severity:   "warn" | "critical" | *"critical"

// Constrain the judge's decoding to a JSON object via `response_format`.
// Set false only for an endpoint that rejects the field.
structured: bool | *true
