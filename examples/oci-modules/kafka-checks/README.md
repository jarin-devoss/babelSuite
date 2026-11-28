# @babelsuite/kafka-checks

Verification helpers for Kafka, built on the `consumer-lag` plugin.

## Exported symbols

| Symbol | Description |
|---|---|
| `assert_consumer_healthy(group, rest_proxy, max_lag, rest_port, after)` | Fails the run when a consumer group's lag is above `max_lag`. The check reports the lag of every topic the group consumes. |

## Usage

`apache/kafka` has no REST proxy, so `rest_proxy=` is required: start one that serves consumer-group lag (the Confluent REST Proxy v3 API, for example `confluentinc/cp-kafka-rest:7.7.1`) and pass its node.

```python
load("@babelsuite/runtime",      "service")
load("@babelsuite/kafka",        "kafka")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

broker = kafka()
proxy  = service.run(
    name  = "rest-proxy",
    image = "confluentinc/cp-kafka-rest:7.7.1",
    env   = {"KAFKA_REST_BOOTSTRAP_SERVERS": "kafka:9092", "KAFKA_REST_LISTENERS": "http://0.0.0.0:8082"},
    after = [broker],
)
lag = assert_consumer_healthy(group="orders-consumer", rest_proxy=proxy, max_lag=500, after=[proxy])
```

The proxy's port is read from its `KAFKA_REST_LISTENERS` (or `PORT`) and defaults to 8082; `rest_port=` overrides it. Without `rest_proxy=` the suite fails at evaluation and says why.

The plugin it uses, `consumer-lag`, must be registered in platform settings.
