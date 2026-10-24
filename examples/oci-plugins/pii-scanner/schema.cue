// PII scanner plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-pii-scanner", config={...}).

target?:   string          // scan, probe, scan_headers
targets?:  [...string]     // scan_many
patterns?: [...string]
severity:  "warn" | "critical" | *"critical"
