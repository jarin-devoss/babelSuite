// LLM judge plugin config schema.
// Grades a text output against a list of criteria using a chat-completions model.
//
// Supply the text either inline with `output`, or by naming a `target` URL whose
// response body is fetched and graded. One of the two is required.

output?:   string
target?:   string

// compare grades two candidates instead of one.
output_a?: string
output_b?: string
prefer?:   "a" | "b" | *"a"

// consistency: how many times to re-grade, and the score spread allowed.
rounds?:     int & >=2 | *3
max_spread?: >=0.0 & <=1.0 | *0.0

// Each criterion is answered yes/no independently. The score is the fraction
// answered yes, so criteria should be small and separately checkable.
criteria: [string, ...string]

model_url: string
model:     string | *"gpt-4o-mini"
api_key?:  string

// Minimum fraction of criteria that must pass, 0.0–1.0.
threshold: >=0.0 & <=1.0 | *1.0

timeout_ms: int & >0 | *30000
severity:   "warn" | "critical" | *"critical"

// Constrain the model's decoding to a JSON object via `response_format`.
// Leave this on: prompt instructions alone do not stop a model wrapping its
// answer in prose or truncating it mid-object. Set false only for an endpoint
// that rejects the field.
structured: bool | *true
