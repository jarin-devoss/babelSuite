//go:build integration

package runner

import (
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The Kafka plugins are tested against real brokers and real REST proxies:
//   - apache/kafka behind confluentinc/cp-kafka-rest, which serves the v2
//     partition offsets and the v3 consumer-group lag;
//   - Redpanda, whose HTTP proxy serves neither, and whose schema registry the
//     schema-compat plugin is tested against.
//
// The first test that needs them starts them (a minute), and TestMain removes
// them. A test is skipped, not failed, when an image cannot be pulled.
const (
	kafkaImage    = "apache/kafka:latest"
	restImage     = "confluentinc/cp-kafka-rest:7.7.1"
	redpandaImage = "redpandadata/redpanda:v24.2.7"

	itNetwork  = "it-kafka-net"
	itKafka    = "it-kafka"
	itRest     = "it-kafka-rest"
	itRedpanda = "it-redpanda"
)

// kafkaStack is what the Kafka plugin tests talk to. Every URL is the address
// the sidecar's container reaches the service at.
type kafkaStack struct {
	RestURL     string // Confluent REST Proxy
	RedpandaURL string // Redpanda HTTP Proxy (serves no offsets and no lag)
	RegistryURL string // Redpanda schema registry
	// RegistryLocal is the same registry as this test process reaches it, for
	// setting up the subjects the plugin is asked about.
	RegistryLocal string
}

var (
	kafkaOnce  sync.Once
	kafkaStk   *kafkaStack
	kafkaSkip  string
	kafkaError error
)

func docker(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustDocker(args ...string) string {
	out, err := docker(args...)
	if err != nil {
		panic(fmt.Sprintf("docker %s: %v\n%s", strings.Join(args, " "), err, out))
	}
	return out
}

// removeKafkaContainers clears the Kafka stack, and only it: the sidecars the
// other integration tests share may be running.
func removeKafkaContainers() {
	for _, name := range []string{itRest, itKafka, itRedpanda} {
		_ = removeContainer(name)
	}
	_, _ = docker("network", "rm", itNetwork)
}

// removeIntegrationContainers is called by TestMain once every test is done.
func removeIntegrationContainers() {
	removeKafkaContainers()
	for _, name := range []string{SidecarContainerName(pluginSuite, pluginProfile), SidecarContainerName(integrationSuite, integrationProfile)} {
		_ = removeContainer(name)
	}
}

func hostPortOf(container, port string) (int, error) {
	out, err := docker("port", container, port)
	if err != nil {
		return 0, fmt.Errorf("docker port %s %s: %v: %s", container, port, err, out)
	}
	// "0.0.0.0:49153" possibly followed by an IPv6 line.
	line := strings.Split(out, "\n")[0]
	return strconv.Atoi(line[strings.LastIndex(line, ":")+1:])
}

func waitFor(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s never answered 200 (last error %v)", url, err)
		}
		time.Sleep(time.Second)
	}
}

func startKafka(t *testing.T) *kafkaStack {
	t.Helper()
	kafkaOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				kafkaError = fmt.Errorf("%v", r)
			}
		}()
		removeKafkaContainers()
		_, _ = docker("network", "create", itNetwork)

		for _, image := range []string{kafkaImage, restImage, redpandaImage} {
			if _, err := docker("image", "inspect", image); err != nil {
				if out, err := docker("pull", "-q", image); err != nil {
					kafkaSkip = fmt.Sprintf("cannot pull %s: %v %s", image, err, out)
					return
				}
			}
		}

		mustDocker("run", "-d", "--name", itKafka, "--network", itNetwork,
			"-e", "KAFKA_NODE_ID=1", "-e", "KAFKA_PROCESS_ROLES=broker,controller",
			"-e", "KAFKA_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093",
			"-e", "KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://"+itKafka+":9092",
			"-e", "KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER",
			"-e", "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"-e", "KAFKA_CONTROLLER_QUORUM_VOTERS=1@"+itKafka+":9093",
			"-e", "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1",
			"-e", "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1",
			"-e", "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1",
			kafkaImage)
		mustDocker("run", "-d", "--name", itRest, "--network", itNetwork, "-p", "0:8082",
			"-e", "KAFKA_REST_BOOTSTRAP_SERVERS="+itKafka+":9092",
			"-e", "KAFKA_REST_HOST_NAME="+itRest,
			"-e", "KAFKA_REST_LISTENERS=http://0.0.0.0:8082",
			restImage)
		mustDocker("run", "-d", "--name", itRedpanda, "--network", itNetwork, "-p", "0:8082", "-p", "0:8081",
			redpandaImage, "redpanda", "start", "--overprovisioned", "--smp", "1", "--memory", "700M",
			"--reserve-memory", "0M", "--node-id", "0", "--check=false",
			"--kafka-addr", "0.0.0.0:9092", "--advertise-kafka-addr", itRedpanda+":9092",
			"--pandaproxy-addr", "0.0.0.0:8082", "--advertise-pandaproxy-addr", itRedpanda+":8082",
			"--schema-registry-addr", "0.0.0.0:8081")

		restPort, err := hostPortOf(itRest, "8082/tcp")
		if err != nil {
			kafkaError = err
			return
		}
		proxyPort, err := hostPortOf(itRedpanda, "8082/tcp")
		if err != nil {
			kafkaError = err
			return
		}
		registryPort, err := hostPortOf(itRedpanda, "8081/tcp")
		if err != nil {
			kafkaError = err
			return
		}
		for _, url := range []string{
			fmt.Sprintf("http://localhost:%d/topics", restPort),
			fmt.Sprintf("http://localhost:%d/topics", proxyPort),
			fmt.Sprintf("http://localhost:%d/subjects", registryPort),
		} {
			if err := waitFor(url, 3*time.Minute); err != nil {
				kafkaError = err
				return
			}
		}

		seedKafka()
		kafkaStk = &kafkaStack{
			RestURL:     fmt.Sprintf("http://host.docker.internal:%d", restPort),
			RedpandaURL: fmt.Sprintf("http://host.docker.internal:%d", proxyPort),
			RegistryURL: fmt.Sprintf("http://host.docker.internal:%d", registryPort),

			RegistryLocal: fmt.Sprintf("http://localhost:%d", registryPort),
		}
	})
	if kafkaSkip != "" {
		t.Skip(kafkaSkip)
	}
	if kafkaError != nil {
		t.Fatal(kafkaError)
	}
	return kafkaStk
}

// seedKafka makes the situation the plugins are asked about:
//   - topic orders: 100 messages, and group billing has read one, so it lags 99;
//   - topic orders-dlq: 4 messages;
//   - topic orders-empty: no messages.
func seedKafka() {
	bin := "/opt/kafka/bin/"
	bootstrap := itKafka + ":9092"
	for _, topic := range []string{"orders", "orders-dlq", "orders-empty"} {
		mustDocker("exec", itKafka, bin+"kafka-topics.sh", "--bootstrap-server", bootstrap,
			"--create", "--if-not-exists", "--topic", topic, "--partitions", "3", "--replication-factor", "1")
	}
	produce := func(topic string, count int) {
		var input strings.Builder
		for i := 1; i <= count; i++ {
			fmt.Fprintf(&input, "message-%d\n", i)
		}
		cmd := exec.Command("docker", "exec", "-i", itKafka, bin+"kafka-console-producer.sh", "--bootstrap-server", bootstrap, "--topic", topic)
		cmd.Stdin = strings.NewReader(input.String())
		if out, err := cmd.CombinedOutput(); err != nil {
			panic(fmt.Sprintf("produce to %s: %v: %s", topic, err, out))
		}
	}
	produce("orders", 100)
	produce("orders-dlq", 4)
	mustDocker("exec", itKafka, bin+"kafka-console-consumer.sh", "--bootstrap-server", bootstrap,
		"--topic", "orders", "--group", "billing", "--from-beginning", "--max-messages", "1")
}
