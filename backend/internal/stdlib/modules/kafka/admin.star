load("@babelsuite/runtime", "task")
load("_shared.star", "quoted", "sanitize_name")

def _admin(broker, name, script, image = "apache/kafka:latest", after = []):
    servers = '"${KAFKA_BOOTSTRAP_SERVERS:-' + broker.name + ':9092}"'
    wait    = "for i in $(seq 1 30); do /opt/kafka/bin/kafka-topics.sh --bootstrap-server " + servers + " --list >/dev/null 2>&1 && break; sleep 2; done"
    return task.run(
        name     = name,
        image    = image,
        after    = [broker] + after,
        commands = [wait + " && " + script.replace("__SERVERS__", servers)],
    )

def create_topic(broker, topic, partitions = 1, replication_factor = 1, configs = {}, image = "apache/kafka:latest", after = []):
    config_flags = ""
    for key, value in configs.items():
        config_flags += " --config " + quoted(str(key) + "=" + str(value))
    return _admin(
        broker,
        name   = broker.name + "-create-" + sanitize_name(topic),
        image  = image,
        script = (
            "/opt/kafka/bin/kafka-topics.sh"
            + " --bootstrap-server __SERVERS__"
            + " --create --if-not-exists"
            + " --topic " + quoted(topic)
            + " --partitions " + str(partitions)
            + " --replication-factor " + str(replication_factor)
            + config_flags
        ),
        after  = after,
    )

def delete_topic(broker, topic, image = "apache/kafka:latest", after = []):
    return _admin(
        broker,
        name   = broker.name + "-delete-" + sanitize_name(topic),
        image  = image,
        script = (
            "/opt/kafka/bin/kafka-topics.sh"
            + " --bootstrap-server __SERVERS__"
            + " --delete --if-exists"
            + " --topic " + quoted(topic)
        ),
        after  = after,
    )

def set_group_offset(broker, group, topic, offset, partition = 0, image = "apache/kafka:latest", after = []):
    return _admin(
        broker,
        name   = broker.name + "-offset-" + sanitize_name(group) + "-" + sanitize_name(topic),
        image  = image,
        script = (
            "/opt/kafka/bin/kafka-consumer-groups.sh"
            + " --bootstrap-server __SERVERS__"
            + " --group " + quoted(group)
            + " --topic " + quoted(topic + ":" + str(partition))
            + " --reset-offsets --to-offset " + str(offset)
            + " --execute"
        ),
        after  = after,
    )

def disconnect(broker, image = "apache/kafka:latest", after = []):
    return _admin(
        broker,
        name   = broker.name + "-disconnect",
        image  = image,
        script = "/opt/kafka/bin/kafka-server-stop.sh || true",
        after  = after,
    )
