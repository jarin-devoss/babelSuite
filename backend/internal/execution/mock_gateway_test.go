package execution

import (
	"reflect"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites"
)

func gatewaySuite() *suites.Definition {
	return &suites.Definition{
		ID: "edge-lab",
		Topology: []suites.TopologyNode{
			{ID: "broker", Name: "broker", Kind: suites.NodeKindService},
			{ID: "stripe", Name: "stripe-mock", Kind: suites.NodeKindMock},
			{ID: "broker-mock", Name: "broker", Kind: suites.NodeKindMock},
			{ID: "dup", Name: "Stripe-Mock", Kind: suites.NodeKindMock},
		},
		APISurfaces: []suites.APISurface{
			{ID: "events", Protocol: "async", MockHost: "kafka://broker:9092", Operations: []suites.APIOperation{{ID: "order-created", Name: "orders"}}},
			{ID: "lost", Protocol: "async", MockHost: "nats://elsewhere:4222", Operations: []suites.APIOperation{{ID: "ping", Name: "ping"}}},
			{ID: "queue", Protocol: "async", MockHost: "amqp://broker:5672", Operations: []suites.APIOperation{{ID: "job", Name: "job"}}},
			{ID: "ticker", Protocol: "tcp", Operations: []suites.APIOperation{{ID: "quote", Name: "quote"}}},
			{ID: "dns", Protocol: "udp", Operations: []suites.APIOperation{{ID: "lookup", Name: "lookup"}}},
		},
	}
}

func TestMockNamesSkipServicesAndDuplicates(t *testing.T) {
	t.Parallel()
	got := suiteMockNames(gatewaySuite())
	if want := []string{"stripe-mock"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("suiteMockNames = %v, want %v (a service owns its name, and names are folded)", got, want)
	}
}

func TestStreamMocksCarryTheirPorts(t *testing.T) {
	t.Parallel()
	got := streamMocksOf(gatewaySuite())
	if len(got) != 2 {
		t.Fatalf("streamMocksOf = %+v, want the TCP and the UDP mock", got)
	}
	if got[0].Transport != "tcp" || got[0].Port == 0 || got[1].Transport != "udp" || got[1].Port == 0 || got[0].Port == got[1].Port {
		t.Fatalf("streamMocksOf = %+v", got)
	}
}

func TestMockWarningsSayWhatTheGatewayCannotDo(t *testing.T) {
	t.Parallel()
	all := strings.Join(mockWarnings(gatewaySuite()), "\n")
	for _, want := range []string{
		"queue/job",             // AMQP is not served
		"elsewhere:4222",        // an async mock whose broker is not a service
		"not a service of this", // ... and the reason
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in warnings:\n%s", want, all)
		}
	}
	if strings.Contains(all, "events/order-created") {
		t.Fatalf("a Kafka mock whose broker is a service was warned about:\n%s", all)
	}
}
