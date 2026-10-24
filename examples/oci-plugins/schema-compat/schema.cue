// Schema registry compatibility check plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-schema-compat", config={...}).

registry_url: string
subject:      string

// check_compat tests against this mode; check_mode expects the registry to enforce it.
mode:         "BACKWARD" | "BACKWARD_TRANSITIVE" | "FORWARD" | "FORWARD_TRANSITIVE" |
              "FULL" | "FULL_TRANSITIVE" | "NONE" | *"BACKWARD"

// Required by every operation except check_mode.
new_schema?:  string
severity:     "warn" | "critical" | *"critical"
