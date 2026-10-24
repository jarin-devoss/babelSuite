// JWT auditor plugin config schema.
// Validates configuration passed to check_token() and audit_endpoint().

auth_url:           string
username:           string
password:           string
token_field?:       string | *"token"
endpoint?:          string
endpoints?:         [...string]       // audit_endpoints
expected_issuer?:   string            // check_claims
expected_audience?: string            // check_claims
forbidden_scopes?:  [...string]       // check_claims
max_lifetime_hours?: number | *24
required_claims?:   [...string] | *["sub", "iss", "exp", "iat"]
severity?:          "low" | "medium" | "high" | "critical" | *"high"
