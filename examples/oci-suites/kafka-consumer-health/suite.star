load("@babelsuite/runtime",      "service", "task")
load("@babelsuite/kafka",        "kafka", "create_topic")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

broker        = kafka(name="broker")
orders_topic  = create_topic(broker=broker, topic="orders", partitions=1, after=[broker])

worker = service.run(
    name="order-worker",
    image="busybox",
    commands=["echo 'consuming orders...' && sleep 30"],
    after=[orders_topic],
)

rest_stub = service.run(
    name="broker-rest",
    image="busybox",
    commands=["mkdir -p /www/consumers/order-workers && echo '{\"offsets\":[{\"topic\":\"orders\",\"partition\":0,\"offset\":40,\"end_offset\":50}]}' > /www/consumers/order-workers/offsets && httpd -f -p 8082 -h /www"],
    after=[broker],
)

lag_check = assert_consumer_healthy(
    broker=broker,
    rest_proxy=rest_stub,
    group="order-workers",
    topic="orders",
    max_lag=500,
    after=[worker, rest_stub],
)
