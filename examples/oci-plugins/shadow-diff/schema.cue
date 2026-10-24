// Shadow traffic response diffing plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-shadow-diff", config={...}).

// Full URLs, or base URLs for compare_paths.
primary:   string
shadow:    string
threshold: number | *0.0
ignore_fields?: [...string]
severity: "warn" | "critical" | *"warn"

// compare_paths: paths appended to both base URLs, e.g. "/orders/42".
paths?:    [...string]

// compare_latency: requests per side, and how much slower the shadow's median may be.
samples:   int & >=1 | *10
max_ratio: number & >0 | *1.5
