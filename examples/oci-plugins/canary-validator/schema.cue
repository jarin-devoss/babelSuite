// Canary traffic split validator plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-canary-validator", config={...}).

target:         string
canary_header:  string | *"X-Canary"
expected_ratio: number & >=0 & <=1
tolerance:      number | *0.05
sample_size:    int & >=1 | *100
