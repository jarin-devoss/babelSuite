// PII scanner plugin config schema.
// Validates the config of a step that calls one of this plugin's operations,
// as loaded with load("@plugins/pii-scanner", ...). The runner checks it before dispatch.

target?:   string          // scan, probe, scan_headers
targets?:  [...string]     // scan_many
patterns?: [...string]
severity:  "warn" | "critical" | *"critical"
