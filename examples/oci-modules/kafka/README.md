# @babelsuite/kafka

Pure Starlark Kafka module providing a managed cluster service and topic administration helpers, built on BabelSuite's runtime primitives.

## Overview

This module wraps Kafka cluster lifecycle and admin operations into importable Starlark symbols. Suites load it instead of declaring raw `service.run()` calls, getting consistent cluster config, health checking, and a topic admin API in return.

## Details

| Field | Value |
|---|---|
| Repository | `localhost:5000/babelsuite/kafka` |
| Latest version | `1.2.3` |
| Available tags | `1.2.3`, `1.2.2`, `latest` |
| Entrypoint | `module.star` |

## Exported Symbols

| Symbol | Description |
|---|---|
| `kafka` | Starts and returns a managed Kafka cluster service |
| `create_topic` | Creates a topic with configurable partitions and replication |
| `delete_topic` | Deletes a topic by name |
| `set_group_offset` | Resets a consumer group offset to a specific position |
| `disconnect` | Gracefully shuts down the cluster connection |

## Usage

```python
load("localhost:5000/babelsuite/kafka:1.2.3", "kafka", "create_topic")

broker = kafka()
orders_topic = create_topic("orders", partitions=3, after=[broker])
```

See `module.star` for the full implementation and `usage.star` for a complete consumer example.

## Pull and Fork

```bash
# Pull the module into a suite
babelctl run localhost:5000/babelsuite/kafka:1.2.3

# Fork to a local copy for modification
babelctl fork localhost:5000/babelsuite/kafka:1.2.3 ./stdlib-kafka
```
