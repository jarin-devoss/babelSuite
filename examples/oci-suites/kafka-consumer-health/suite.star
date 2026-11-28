load("@babelsuite/runtime",      "service", "task", "test", "log")
load("@babelsuite/kafka",        "kafka", "create_topic")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

# ── what the profile decides ─────────────────────────────────────────────────
# MESSAGES orders are produced; the order-workers group consumes CONSUME of them
# and stops. The lag left is MESSAGES - CONSUME, and the check fails the run when
# that is above MAX_LAG. local.yaml consumes everything (lag 0); lagging.yaml
# consumes 10 of 1000, so 990 are left and the run must end Failed.
MESSAGES = env.get("MESSAGES", "200")
CONSUME  = env.get("CONSUME",  "200")
MAX_LAG  = int(env.get("MAX_LAG", "10"))

KAFKA_IMAGE = "apache/kafka:3.9.0"
REST_IMAGE  = "confluentinc/cp-kafka-rest:7.7.1"
PYTHON      = "python:3.12-alpine"

KAFKA_BIN = "/opt/kafka/bin/"

# ── the broker, and the topic the orders go to ───────────────────────────────
broker       = kafka(name = "broker", image = KAFKA_IMAGE)
orders_topic = create_topic(broker, "orders", partitions = 1, image = KAFKA_IMAGE, after = [broker])

# ── the REST proxy the lag is read through (apache/kafka has none) ───────────
rest_proxy = service.run(
    name  = "rest-proxy",
    image = REST_IMAGE,
    env   = {
        "KAFKA_REST_BOOTSTRAP_SERVERS": "PLAINTEXT://broker:9092",
        "KAFKA_REST_LISTENERS":         "http://0.0.0.0:8082",
        "KAFKA_REST_HOST_NAME":         "rest-proxy",
    },
    after = [broker],
)

proxy_ready = test.run(
    name     = "rest-proxy-ready",
    image    = PYTHON,
    after    = [rest_proxy],
    commands = ["for i in $(seq 1 90); do python -c \"import urllib.request; urllib.request.urlopen('http://rest-proxy:8082/v3/clusters')\" 2>/dev/null && exit 0; sleep 2; done; echo 'rest-proxy never listed a cluster'; exit 1"],
)

# ── the orders arrive ────────────────────────────────────────────────────────
producer = task.run(
    name     = "order-producer",
    image    = KAFKA_IMAGE,
    after    = [orders_topic],
    commands = ["seq 1 " + MESSAGES + " | " + KAFKA_BIN + "kafka-console-producer.sh --bootstrap-server broker:9092 --topic orders"],
)

# ── the consumer: group order-workers reads CONSUME orders and commits its offset ──
worker = task.run(
    name     = "order-worker",
    image    = KAFKA_IMAGE,
    after    = [producer],
    commands = [KAFKA_BIN + "kafka-console-consumer.sh --bootstrap-server broker:9092 --topic orders --group order-workers --from-beginning --max-messages " + CONSUME + " --timeout-ms 60000 > /dev/null"],
)

# ── the check: the group's lag, read from the broker through the proxy ───────
lag_check = assert_consumer_healthy(
    rest_proxy = rest_proxy,
    group      = "order-workers",
    max_lag    = MAX_LAG,
    after      = [worker, proxy_ready],
)

done = log.info("order-workers lag is within " + str(MAX_LAG), after = [lag_check])
