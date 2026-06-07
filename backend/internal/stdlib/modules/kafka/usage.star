load("@babelsuite/kafka",   "kafka", "create_topic", "set_group_offset", "delete_topic")
load("@babelsuite/runtime", "service", "log")

broker = kafka(name="broker")

payments_topic = create_topic(broker, "payments.events", partitions=3)
returns_topic  = create_topic(broker, "returns.events",  partitions=1, after=[payments_topic])

topics_ready = log.info("topics ready", after=[returns_topic])

replay = set_group_offset(broker, group="fraud-worker", topic="payments.events", offset=12, after=[topics_ready])

consumer = service.run(
    name  = "payments-consumer",
    image = "ghcr.io/acme/payments-consumer:latest",
    env   = {"KAFKA_BOOTSTRAP_SERVERS": broker.name + ":9092"},
    after = [replay],
)

delete_topic(broker, "returns.events", after=[consumer])
