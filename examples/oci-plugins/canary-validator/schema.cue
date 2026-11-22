// Canary traffic split validator plugin config schema.
// Validates the config of a step that calls one of this plugin's operations,
// as loaded with load("@plugins/canary-validator", ...). The runner checks it before dispatch.

target:          string
canary_header:   string | *"X-Canary"
expected_ratio?: number & >=0 & <=1
tolerance:       number | *0.05
sample_size:     int & >=1 | *100
severity:        "warn" | "critical" | *"critical"

// sticky: the session key sent on every request.
session_header:  string | *"X-Session-Id"
session_value:   string | *"babelsuite-sticky-check"

// compare_errors: how much higher the canary's 5xx rate may be than stable's.
max_error_delta: number & >=0 & <=1 | *0.01
