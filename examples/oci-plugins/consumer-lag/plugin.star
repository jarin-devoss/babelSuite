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

def wait_caught_up(name, group, max_lag = 0, timeout_s = 60, interval_s = 2,
                   kafka_rest_url = "", severity = "critical", after = []):
    return _ref.wait_caught_up(
        name           = name,
        after          = after,
        group          = group,
        max_lag        = max_lag,
        timeout_s      = timeout_s,
        interval_s     = interval_s,
        kafka_rest_url = kafka_rest_url,
        severity       = severity,
    )

def check_trend(name, group, interval_s = 10, max_growth = 0,
                kafka_rest_url = "", severity = "critical", after = []):
    return _ref.check_trend(
        name           = name,
        after          = after,
        group          = group,
        interval_s     = interval_s,
        max_growth     = max_growth,
        kafka_rest_url = kafka_rest_url,
        severity       = severity,
    )
