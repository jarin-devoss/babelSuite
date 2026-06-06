load("@babelsuite/runtime", "service")
load("_shared.star", "merge_dicts")

def kafka(name = "kafka", image = "bitnami/kafka:3.7", port = 9092, after = [], env = {}):
    return service.run(
        name  = name,
        image = image,
        after = after,
        env   = merge_dicts({
            "KAFKA_CFG_NODE_ID":                        "1",
            "KAFKA_CFG_PROCESS_ROLES":                  "broker,controller",
            "KAFKA_CFG_LISTENERS":                      "PLAINTEXT://:9092,CONTROLLER://:9093",
            "KAFKA_CFG_ADVERTISED_LISTENERS":           "PLAINTEXT://" + name + ":" + str(port),
            "KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP": "PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT",
            "KAFKA_CFG_CONTROLLER_QUORUM_VOTERS":       "1@" + name + ":9093",
            "KAFKA_CFG_CONTROLLER_LISTENER_NAMES":      "CONTROLLER",
            "KAFKA_CFG_INTER_BROKER_LISTENER_NAME":     "PLAINTEXT",
            "KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE":      "true",
            "ALLOW_PLAINTEXT_LISTENER":                 "yes",
        }, env),
    )
