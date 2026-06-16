// JWT auditor plugin config schema.
// Validates configuration passed to check_token() and audit_endpoint().

auth_url:           string
username:           string
password:           string
token_field?:       string | *"token"
endpoint?:          string
max_lifetime_hours?: number | *24
required_claims?:   [...string] | *["sub", "iss", "exp", "iat"]
severity?:          "low" | "medium" | "high" | "critical" | *"high"
