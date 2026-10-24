// Kafka consumer lag monitor plugin config schema.
// Validates configuration passed to plugin.run(plugin="babelsuite-consumer-lag", config={...}).

kafka_rest_url: string
group:          string
max_lag:        int & >=0 | *1000
severity:       "warn" | "critical" | *"critical"

// wait_caught_up: how long to keep polling, and how often.
timeout_s:      number & >0 | *60
interval_s?:    number & >0

// check_trend: how much the total lag may grow between its two samples.
max_growth:     int | *0
