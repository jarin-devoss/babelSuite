// LLM judge plugin config schema.
// Grades a text output against a list of criteria using a chat-completions model.
//
// Supply the text either inline with `output`, or by naming a `target` URL whose
// response body is fetched and graded. One of the two is required.

output?:   string
target?:   string

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
