load("@plugins/consumer-lag", "check_lag")

# _rest_port reads the port a REST proxy listens on from what it was started
# with: KAFKA_REST_LISTENERS (the Confluent REST proxy's setting, e.g.
# http://0.0.0.0:8082) or PORT. It falls back to the proxy's usual 8082.
def _rest_port(rest_proxy):
    env = rest_proxy.env
    listeners = env.get("KAFKA_REST_LISTENERS", "")
    if listeners:
        return listeners.split(",")[0].rsplit(":", 1)[-1].rstrip("/")
    return env.get("PORT", "8082")

# assert_consumer_healthy fails the run when a consumer group's lag is above
# max_lag. The lag comes from a Kafka REST proxy, which apache/kafka does not
# ship, so rest_proxy= is required: start one (for example
# confluentinc/cp-kafka-rest) and pass its node. The check reports the lag of
# every topic the group consumes.
def assert_consumer_healthy(group, rest_proxy = None, max_lag = 500, rest_port = None, after = []):
    if rest_proxy == None:
        fail("assert_consumer_healthy needs rest_proxy=: apache/kafka has no REST proxy. Start one (for example confluentinc/cp-kafka-rest) and pass it as rest_proxy=")
    port = rest_port if rest_port != None else _rest_port(rest_proxy)
    return check_lag(
        name = group + "-lag-check",
        group = group,
        max_lag = max_lag,
        kafka_rest_url = "http://" + rest_proxy.name + ":" + str(port),
        severity = "critical",
        after = after,
    )
