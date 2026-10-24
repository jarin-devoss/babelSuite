// Dead letter queue inspector plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-dlq-inspector", config={...}).

kafka_rest_url: string
topic?:         string          // inspect, watch_dlq, check_growth
topics?:        [...string]     // inspect_topics
max_messages:   int & >=0 | *0
severity:       "warn" | "critical" | *"critical"

// check_growth: the wait between its two samples, and how much the DLQ may grow.
interval_s:     number & >0 | *10
max_growth:     int | *0
