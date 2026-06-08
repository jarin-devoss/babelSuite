// Shadow traffic response diffing plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-shadow-diff", config={...}).

primary:   string
shadow:    string
threshold: number | *0.0
ignore_fields?: [...string]
severity: "warn" | "critical" | *"warn"
