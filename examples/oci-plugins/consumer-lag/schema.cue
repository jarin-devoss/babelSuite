// Kafka consumer lag monitor plugin config schema.
// Validates the config of a step that calls one of this plugin's operations,
// as loaded with load("@plugins/consumer-lag", ...). The runner checks it before dispatch.

kafka_rest_url: string
group:          string
max_lag:        int & >=0 | *1000
severity:       "warn" | "critical" | *"critical"

// Confluent REST Proxy v3 cluster id. Read from /v3/clusters when omitted.
cluster_id?:    string

// wait_caught_up: how long to keep polling, and how often.
timeout_s:      number & >0 | *60
interval_s?:    number & >0

// check_trend: how much the total lag may grow between its two samples.
max_growth:     int | *0
