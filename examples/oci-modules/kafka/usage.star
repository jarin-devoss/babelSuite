load("@babelsuite/kafka", "kafka", "create_topic", "delete_topic", "set_group_offset")
load("@babelsuite/runtime", "service", "log")

broker = kafka(name="broker")

payments_topic = create_topic(
    broker,
    topic="payments.events",
    partitions=3,
    replication_factor=1,
)

topics_ready = log.info("payments.events topic ready — replaying consumer offsets", after=[payments_topic])

replay_offsets = set_group_offset(
    broker,
    group="fraud-worker",
    topic="payments.events",
    partition=0,
    offset=12,
    after=[topics_ready],
)

consumer = service.run(
    name="payments-consumer",
    image="ghcr.io/acme/payments-consumer:latest",
    env={
        "KAFKA_BOOTSTRAP_SERVERS": broker["bootstrap_servers"],
    },
    after=["broker", "broker-offset-fraud-worker-payments-events"],
)

cleanup_topic = delete_topic(broker, topic="payments.events", after=["payments-consumer"])
