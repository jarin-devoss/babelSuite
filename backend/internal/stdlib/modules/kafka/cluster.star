load("@babelsuite/runtime", "service")

def kafka(name = "kafka", image = "apache/kafka:latest", port = 9092, after = [], env = {}):
    return service.run(
        name  = name,
        image = image,
        after = after,
        env   = utils.merge({
            "KAFKA_NODE_ID":                        "1",
            "KAFKA_PROCESS_ROLES":                  "broker,controller",
            "KAFKA_LISTENERS":                      "PLAINTEXT://:" + str(port) + ",CONTROLLER://:9093",
            "KAFKA_ADVERTISED_LISTENERS":           "PLAINTEXT://" + name + ":" + str(port),
            "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP": "PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT",
            "KAFKA_CONTROLLER_QUORUM_VOTERS":       "1@" + name + ":9093",
            "KAFKA_CONTROLLER_LISTENER_NAMES":      "CONTROLLER",
            "KAFKA_INTER_BROKER_LISTENER_NAME":     "PLAINTEXT",
            "KAFKA_AUTO_CREATE_TOPICS_ENABLE":      "true",
            "KAFKA_LOG_DIRS":                       "/tmp/kraft-combined-logs",
        }, env),
    )
