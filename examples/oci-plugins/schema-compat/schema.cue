// Schema registry compatibility check plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-schema-compat", config={...}).

registry_url: string
subject:      string
mode:         "BACKWARD" | "FORWARD" | "FULL" | *"BACKWARD"
new_schema:   string
