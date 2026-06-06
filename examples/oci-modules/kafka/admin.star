load("@babelsuite/runtime", "task")
load("_shared.star", "quoted", "sanitize_name")

def _admin(broker, name, script, image = "bitnami/kafka:3.7", after = []):
    servers = '"${KAFKA_BOOTSTRAP_SERVERS:-' + broker.name + ':9092}"'
    return task.run(
        name     = name,
        image    = image,
        after    = [broker] + after,
        commands = ["sh", "-c", script.replace("__SERVERS__", servers)],
    )

def create_topic(broker, topic, partitions = 1, replication_factor = 1, configs = {}, image = "bitnami/kafka:3.7", after = []):
    config_flags = ""
    for key, value in configs.items():
        config_flags += " --config " + quoted(str(key) + "=" + str(value))
    return _admin(
        broker,
        name   = broker.name + "-create-" + sanitize_name(topic),
        image  = image,
        script = (
            "kafka-topics.sh"
            + " --bootstrap-server __SERVERS__"
            + " --create --if-not-exists"
            + " --topic " + quoted(topic)
            + " --partitions " + str(partitions)
            + " --replication-factor " + str(replication_factor)
            + config_flags
        ),
        after  = after,
    )

def delete_topic(broker, topic, image = "bitnami/kafka:3.7", after = []):
    return _admin(
        broker,
        name   = broker.name + "-delete-" + sanitize_name(topic),
        image  = image,
        script = (
            "kafka-topics.sh"
            + " --bootstrap-server __SERVERS__"
            + " --delete --if-exists"
            + " --topic " + quoted(topic)
        ),
        after  = after,
    )

def set_group_offset(broker, group, topic, offset, partition = 0, image = "bitnami/kafka:3.7", after = []):
    return _admin(
        broker,
        name   = broker.name + "-offset-" + sanitize_name(group) + "-" + sanitize_name(topic),
        image  = image,
        script = (
            "kafka-consumer-groups.sh"
            + " --bootstrap-server __SERVERS__"
            + " --group " + quoted(group)
            + " --topic " + quoted(topic + ":" + str(partition))
            + " --reset-offsets --to-offset " + str(offset)
            + " --execute"
        ),
        after  = after,
    )

def disconnect(broker, image = "bitnami/kafka:3.7", after = []):
    return _admin(
        broker,
        name   = broker.name + "-disconnect",
        image  = image,
        script = "kafka-server-stop.sh || true",
        after  = after,
    )
