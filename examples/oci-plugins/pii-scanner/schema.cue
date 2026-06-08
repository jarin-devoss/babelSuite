// PII scanner plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-pii-scanner", config={...}).

target:    string
patterns?: [...string]
severity:  "warn" | "critical" | *"critical"
