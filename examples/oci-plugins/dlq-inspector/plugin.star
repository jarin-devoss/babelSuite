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

def check_growth(name, topic, interval_s = 10, max_growth = 0,
                 kafka_rest_url = "", severity = "critical", after = []):
    return _ref.check_growth(
        name           = name,
        after          = after,
        topic          = topic,
        interval_s     = interval_s,
        max_growth     = max_growth,
        kafka_rest_url = kafka_rest_url,
        severity       = severity,
    )

def inspect_topics(name, topics, max_messages = 0, kafka_rest_url = "",
                   severity = "critical", after = []):
    return _ref.inspect_topics(
        name           = name,
        after          = after,
        topics         = topics,
        max_messages   = max_messages,
        kafka_rest_url = kafka_rest_url,
        severity       = severity,
    )
