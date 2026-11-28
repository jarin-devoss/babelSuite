# Kafka Consumer Health

A real Kafka broker, a producer, a consumer group and a REST proxy. The lag of the group is read through the proxy by `assert_consumer_healthy` from `@babelsuite/kafka-checks`, which loads the `consumer-lag` plugin.

## Execution order

```
broker ─┬─ create topic orders ── order-producer ── order-worker ──┐
        └─ rest-proxy ── rest-proxy-ready ──────────────────────────┴── order-workers-lag-check ── done
```

## What breaks it

The producer writes `MESSAGES` orders and the consumer group `order-workers` reads `CONSUME` of them. The lag left is the difference, and the check fails the run when it is above `MAX_LAG`.

Under `lagging.yaml` 1000 orders are produced and 10 consumed: the lag is 990, the limit is 500, and the run must end **Failed**.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | 200 produced, 200 consumed: lag 0, limit 10. Healthy. |
| `lagging.yaml` | 1000 produced, 10 consumed: lag 990, limit 500. Failed. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The whole topology. |
| `profiles/` | What each profile produces and consumes. |

## Running

```bash
python scripts/push-to-zot.py --suite kafka-consumer-health --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "kafka-consumer-health", "profile": "local.yaml"}`.

The images are `apache/kafka:3.9.0` and `confluentinc/cp-kafka-rest:7.7.1`. The REST proxy is needed because `apache/kafka` ships none and the plugin reads the v3 consumer-group lag API.
