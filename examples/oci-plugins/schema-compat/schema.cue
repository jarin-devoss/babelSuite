// Schema registry compatibility check plugin config schema.
// Validates the config of a step that calls one of this plugin's operations,
// as loaded with load("@plugins/schema-compat", ...). The runner checks it before dispatch.

registry_url: string
subject:      string

// check_compat tests against this mode; check_mode expects the registry to enforce it.
mode:         "BACKWARD" | "BACKWARD_TRANSITIVE" | "FORWARD" | "FORWARD_TRANSITIVE" |
              "FULL" | "FULL_TRANSITIVE" | "NONE" | *"BACKWARD"

// Required by every operation except check_mode.
new_schema?:  string
severity:     "warn" | "critical" | *"critical"

// The schema language of new_schema and of the subject.
schema_type?: "AVRO" | "JSON" | "PROTOBUF"
