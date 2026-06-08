load("@babelsuite/runtime", "plugin")

_ref = plugin("dlq-inspector")

def inspect(name, topic, max_messages = 0, kafka_rest_url = "", severity = "critical", after = []):
    return _ref.inspect(
        name           = name,
        after          = after,
        topic          = topic,
        max_messages   = max_messages,
        kafka_rest_url = kafka_rest_url,
        severity       = severity,
    )

def watch_dlq(name, topic, max_messages = 0, kafka_rest_url = "", after = []):
    return _ref.watch_dlq(
        name           = name,
        after          = after,
        topic          = topic,
        max_messages   = max_messages,
        kafka_rest_url = kafka_rest_url,
    )
