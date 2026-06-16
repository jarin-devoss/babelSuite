// Dead letter queue inspector plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-dlq-inspector", config={...}).

kafka_rest_url: string
topic:          string
max_messages:   int & >=0 | *0
severity:       "warn" | "critical" | *"critical"
