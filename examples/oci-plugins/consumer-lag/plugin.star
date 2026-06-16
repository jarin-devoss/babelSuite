load("@babelsuite/runtime", "plugin")

_ref = plugin("consumer-lag")

def check_lag(name, group, max_lag = 1000, kafka_rest_url = "", severity = "critical", after = []):
    return _ref.check_lag(
        name          = name,
        after         = after,
        group         = group,
        max_lag       = max_lag,
        kafka_rest_url = kafka_rest_url,
        severity      = severity,
    )

def watch_lag(name, group, max_lag = 1000, kafka_rest_url = "", after = []):
    return _ref.watch_lag(
        name           = name,
        after          = after,
        group          = group,
        max_lag        = max_lag,
        kafka_rest_url = kafka_rest_url,
    )
