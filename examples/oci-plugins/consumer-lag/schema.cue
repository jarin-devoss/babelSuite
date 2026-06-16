// Kafka consumer lag monitor plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-consumer-lag", config={...}).

kafka_rest_url: string
group:          string
max_lag:        int & >=0 | *1000
severity:       "warn" | "critical" | *"critical"
