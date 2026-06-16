load("@plugins/consumer-lag", "check_lag")

def assert_consumer_healthy(broker, group, topic, max_lag=500, after=[], rest_proxy=None):
    target = rest_proxy or broker
    return check_lag(
        name=group + "-lag-check",
        group=group,
        max_lag=max_lag,
        kafka_rest_url="http://" + target.name + ":8082",
        severity="critical",
        after=after,
    )
