package suites

import (
	"os"
	"strings"
	"testing"
)

func kafkaChecksResolver(t *testing.T) ModuleResolver {
	t.Helper()
	checksStar, err := os.ReadFile("../../../examples/oci-modules/kafka-checks/checks.star")
	if err != nil {
		t.Fatalf("read checks.star: %v", err)
	}
	consumerLagStar, err := os.ReadFile("../../../examples/oci-plugins/consumer-lag/plugin.star")
	if err != nil {
		t.Fatalf("read consumer-lag plugin.star: %v", err)
	}
	return func(name string) (map[string]string, error) {
		switch name {
		case "kafka-checks":
			return map[string]string{"checks.star": string(checksStar)}, nil
		case "consumer-lag":
			return map[string]string{"plugin.star": string(consumerLagStar)}, nil
		}
		return nil, nil
	}
}

func TestKafkaChecksModuleWithConsumerLagPlugin(t *testing.T) {
	suiteStar := `
load("@babelsuite/runtime", "service")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

proxy   = service.run(image="alpine:3.19", name="rest-proxy", env={"KAFKA_REST_LISTENERS": "http://0.0.0.0:8083"})
worker  = service.run(image="alpine:3.19", name="worker", after=[proxy])
check   = assert_consumer_healthy(rest_proxy=proxy, group="workers", after=[worker])
`
	nodes, err := evalStarlarkTopology(suiteStar, kafkaChecksResolver(t))
	if err != nil {
		t.Fatalf("evalStarlarkTopology error: %v", err)
	}

	var found bool
	for _, n := range nodes {
		if n.Kind == NodeKindPlugin && n.Variant == "consumer-lag" {
			found = true
			// The proxy's address comes from the node it was given, port included.
			if got := n.PluginConfig["kafka_rest_url"]; got != "http://rest-proxy:8083" {
				t.Errorf("kafka_rest_url = %v, want http://rest-proxy:8083", got)
			}
		}
	}
	if !found {
		t.Error("expected a consumer-lag plugin node, none found")
	}
}

// apache/kafka has no REST proxy, so a lag check that is not given one cannot
// work. It used to default to http://<broker>:8082, which nothing answers.
func TestKafkaChecksRequiresARestProxy(t *testing.T) {
	suiteStar := `
load("@babelsuite/runtime", "service")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

broker = service.run(image="alpine:3.19", name="broker")
check  = assert_consumer_healthy(group="workers", after=[broker])
`
	_, err := evalStarlarkTopology(suiteStar, kafkaChecksResolver(t))
	if err == nil || !strings.Contains(err.Error(), "needs rest_proxy=") {
		t.Fatalf("err = %v, want a refusal that names rest_proxy=", err)
	}
}
