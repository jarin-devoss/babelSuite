package suites

import (
	"os"
	"testing"
)

func TestKafkaChecksModuleWithConsumerLagPlugin(t *testing.T) {
	checksStar, err := os.ReadFile("../../../examples/oci-modules/kafka-checks/checks.star")
	if err != nil {
		t.Fatalf("read checks.star: %v", err)
	}
	consumerLagStar, err := os.ReadFile("../../../examples/oci-plugins/consumer-lag/plugin.star")
	if err != nil {
		t.Fatalf("read consumer-lag plugin.star: %v", err)
	}

	suiteStar := `
load("@babelsuite/runtime", "service")
load("@babelsuite/kafka-checks", "assert_consumer_healthy")

broker  = service.run(name="broker")
worker  = service.run(name="worker", after=[broker])
check   = assert_consumer_healthy(broker=broker, group="workers", topic="orders", after=[worker])
`

	resolver := func(name string) (map[string]string, error) {
		switch name {
		case "kafka-checks":
			return map[string]string{"checks.star": string(checksStar)}, nil
		case "consumer-lag":
			return map[string]string{"plugin.star": string(consumerLagStar)}, nil
		}
		return nil, nil
	}

	nodes, err := evalStarlarkTopology(suiteStar, resolver)
	if err != nil {
		t.Fatalf("evalStarlarkTopology error: %v", err)
	}

	var found bool
	for _, n := range nodes {
		t.Logf("node: id=%s name=%s kind=%s variant=%s", n.ID, n.Name, n.Kind, n.Variant)
		if n.Kind == NodeKindPlugin && n.Variant == "consumer-lag" {
			found = true
		}
	}
	if !found {
		t.Error("expected a consumer-lag plugin node, none found")
	}
}
