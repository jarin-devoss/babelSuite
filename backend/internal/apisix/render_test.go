package apisix

import (
	"strings"
	"testing"
)

func TestTransportKindInfersConcreteBrokerTransportFromAsyncScheme(t *testing.T) {
	testCases := []struct {
		name      string
		surface   SurfaceConfig
		operation OperationConfig
		expected  string
	}{
		{
			name: "async kafka host",
			surface: SurfaceConfig{
				Protocol: "Async",
				MockHost: "kafka://edge-lab.mock.internal",
			},
			operation: OperationConfig{
				MockURL: "kafka://edge-lab.mock.internal/orders.created",
				MockMetadata: OperationMetadataConfig{
					Adapter: "async",
				},
			},
			expected: "kafka",
		},
		{
			name: "async mqtt host",
			surface: SurfaceConfig{
				Protocol: "Async",
				MockHost: "mqtts://edge-lab.mock.internal",
			},
			operation: OperationConfig{
				MockURL: "mqtts://edge-lab.mock.internal/devices/heartbeat",
				MockMetadata: OperationMetadataConfig{
					Adapter: "async",
				},
			},
			expected: "mqtt",
		},
		{
			name: "async without broker scheme",
			surface: SurfaceConfig{
				Protocol: "Async",
				MockHost: "https://edge-lab.mock.internal",
			},
			operation: OperationConfig{
				MockURL: "/events/orders.created",
				MockMetadata: OperationMetadataConfig{
					Adapter: "async",
				},
			},
			expected: "async",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := transportKind(tc.surface, tc.operation)
			if actual != tc.expected {
				t.Fatalf("expected transport %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestRenderStandaloneConfigIncludesRESTRoutes(t *testing.T) {
	body := RenderStandaloneConfig(SuiteConfig{
		ID: "payment-suite",
		APISurfaces: []SurfaceConfig{
			{
				ID:       "payment-gateway",
				Protocol: "REST",
				MockHost: "https://payment-suite.mock.internal",
				Operations: []OperationConfig{
					{
						ID:           "create-payment",
						Method:       "POST",
						Name:         "/payments",
						Summary:      "Create a payment authorization and fan out to Stripe.",
						MockMetadata: OperationMetadataConfig{Adapter: "rest"},
					},
				},
			},
		},
	})

	for _, want := range []string{
		"uri: /payments",
		"name: " + MockPluginName,
		"suite: payment-suite",
		"surface: payment-gateway",
		"operation: create-payment",
		"engine: ${{BABELSUITE_ENGINE_ADDR}}",
		"token: ${{BABELSUITE_MOCK_TOKEN}}",
		"127.0.0.1:1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in the APISIX config, got:\n%s", want, body)
		}
	}
	if !strings.HasSuffix(body, "#END\n") {
		t.Fatalf("expected APISIX standalone config terminator, got:\n%s", body)
	}
}

func TestRenderStandaloneConfigOmitsHardeningByDefault(t *testing.T) {
	body := RenderStandaloneConfig(SuiteConfig{ID: "payment-suite"})

	if strings.Contains(body, "babelsuite-flood-target") {
		t.Fatalf("expected no synthetic flood target without Harden, got:\n%s", body)
	}
	if strings.Contains(body, "global_rules") {
		t.Fatalf("expected no global security-header rule without Harden, got:\n%s", body)
	}
	if strings.Contains(body, "name: response-rewrite") || strings.Contains(body, "name: limit-count") || strings.Contains(body, "name: echo") {
		t.Fatalf("expected hardening plugins absent from catalog without Harden, got:\n%s", body)
	}
}

func TestRenderStandaloneConfigAppliesHardeningWhenRequested(t *testing.T) {
	body := RenderStandaloneConfig(SuiteConfig{ID: "security-suite", Harden: true})

	if !strings.Contains(body, "babelsuite-flood-target") {
		t.Fatalf("expected synthetic flood target with Harden, got:\n%s", body)
	}
	if !strings.Contains(body, "global_rules") {
		t.Fatalf("expected global security-header rule with Harden, got:\n%s", body)
	}
	if !strings.Contains(body, "name: response-rewrite") || !strings.Contains(body, "name: limit-count") || !strings.Contains(body, "name: echo") {
		t.Fatalf("expected hardening plugins present in catalog with Harden, got:\n%s", body)
	}
}

func mockSuite(surfaces ...SurfaceConfig) SuiteConfig {
	return SuiteConfig{ID: "edge-lab", APISurfaces: surfaces}
}

var (
	restSurface = SurfaceConfig{ID: "api", Protocol: "REST", MockHost: "https://edge-lab.mock.internal", Operations: []OperationConfig{
		{ID: "get-order", Method: "GET", Name: "/orders", MockMetadata: OperationMetadataConfig{Adapter: "rest"}},
	}}
	grpcSurface = SurfaceConfig{ID: "pricing", Protocol: "gRPC", MockHost: "grpc://edge-lab.mock.internal", Operations: []OperationConfig{
		{ID: "quote-refund", Name: "/quote-refund", ContractPath: "api/proto/pricing.proto#QuoteRefund", MockMetadata: OperationMetadataConfig{Adapter: "grpc"}},
	}}
	wsSurface = SurfaceConfig{ID: "live", Protocol: "WebSocket", MockHost: "wss://edge-lab.mock.internal", Operations: []OperationConfig{
		{ID: "ticker", Name: "/ticker", MockMetadata: OperationMetadataConfig{Adapter: "rest"}},
	}}
	kafkaSurface = SurfaceConfig{ID: "events", Protocol: "Async", MockHost: "kafka://broker:9092", Operations: []OperationConfig{
		{ID: "order-created", MockURL: "kafka://broker:9092/orders.created", MockMetadata: OperationMetadataConfig{Adapter: "async"}},
	}}
	tcpSurface = SurfaceConfig{ID: "legacy", Protocol: "TCP", MockHost: "tcp://edge-lab.mock.internal", Operations: []OperationConfig{
		{ID: "ping", MockMetadata: OperationMetadataConfig{Adapter: "async"}},
		{ID: "status", MockMetadata: OperationMetadataConfig{Adapter: "async"}},
	}}
	udpSurface = SurfaceConfig{ID: "beacon", Protocol: "UDP", MockHost: "udp://edge-lab.mock.internal", Operations: []OperationConfig{
		{ID: "hello", MockMetadata: OperationMetadataConfig{Adapter: "async"}},
	}}
	amqpSurface = SurfaceConfig{ID: "queue", Protocol: "AMQP", MockHost: "amqp://broker:5672", Operations: []OperationConfig{
		{ID: "job", MockMetadata: OperationMetadataConfig{Adapter: "async"}},
	}}
)

// ext-plugin-pre-req was on every route and did nothing: no plugin runner was
// configured, and allow_degradation made APISIX skip it silently. The upstream
// variables were never set. Neither belongs in what is rendered.
func TestTheRenderedConfigCarriesNothingThatDoesNothing(t *testing.T) {
	body := RenderStandaloneConfig(mockSuite(restSurface, grpcSurface, wsSurface, kafkaSurface, tcpSurface, udpSurface, amqpSurface))

	for _, dead := range []string{
		"ext-plugin-pre-req", "allow_degradation", "babelsuite-resolver", "babelsuite-internal",
		"APISIX_GRPC_UPSTREAM_ADDR", "APISIX_KAFKA_UPSTREAM_ADDR", "APISIX_MQTT_UPSTREAM_ADDR", "APISIX_TCP_UPSTREAM_ADDR", "APISIX_UDP_UPSTREAM_ADDR",
		"grpc-transcode", "kafka-proxy", "mqtt-proxy", "protos:", "degraphql", "admin",
		"still need", "plugin-runner", "Resolver contracts",
	} {
		if strings.Contains(body, dead) {
			t.Errorf("the rendered config still carries %q:\n%s", dead, body)
		}
	}
}

func TestEveryRouteThatAuthenticatesPresentsTheMockTokenFromTheEnvironment(t *testing.T) {
	body := RenderStandaloneConfig(mockSuite(restSurface, grpcSurface, wsSurface, tcpSurface))

	// REST (plugin conf), gRPC (plugin conf + rewritten header), gRPC-Web (header),
	// WebSocket (header) and the TCP stream plugin.
	if got := strings.Count(body, "${{BABELSUITE_MOCK_TOKEN}}"); got < 6 {
		t.Fatalf("every authenticated route must take the token from the sidecar's environment, found %d uses:\n%s", got, body)
	}
	if strings.Contains(body, "Bearer babelsuite") {
		t.Fatalf("a literal token is rendered:\n%s", body)
	}
}

func TestOperationsTheGatewayCannotServeAreReportedAndGetNoRoute(t *testing.T) {
	suite := mockSuite(restSurface, amqpSurface)
	body := RenderStandaloneConfig(suite)
	if strings.Contains(body, "operation: job") {
		t.Fatalf("an operation the gateway cannot serve was routed:\n%s", body)
	}

	unserved := UnservedSurfaces(suite)
	if len(unserved) != 1 || unserved[0].Operation != "job" || unserved[0].Transport != "AMQP" || !strings.Contains(unserved[0].Reason, "AMQP") {
		t.Fatalf("unserved = %+v, want the AMQP operation with its reason", unserved)
	}

	served := mockSuite(restSurface, grpcSurface, wsSurface, kafkaSurface, tcpSurface, udpSurface)
	if got := UnservedSurfaces(served); len(got) != 0 {
		t.Fatalf("REST, gRPC, WebSocket, Kafka, TCP and UDP are served and must not be reported: %+v", got)
	}

	noBroker := SurfaceConfig{ID: "events", Protocol: "Async", MockHost: "https://x", Operations: []OperationConfig{{ID: "e", MockMetadata: OperationMetadataConfig{Adapter: "async"}}}}
	if got := UnservedSurfaces(mockSuite(noBroker)); len(got) != 1 || !strings.Contains(got[0].Reason, "names no broker") {
		t.Fatalf("an async surface with no broker must say so: %+v", got)
	}
}

func TestEveryGRPCOperationOfTheSuiteSharesOneRouteAndGRPCWebHasItsOwn(t *testing.T) {
	body := RenderStandaloneConfig(mockSuite(restSurface, grpcSurface))

	if got := strings.Count(body, "scheme: grpc"); got != 2 {
		t.Fatalf("want one gRPC and one gRPC-Web route to the gRPC upstream, got %d:\n%s", got, body)
	}
	for _, want := range []string{
		"id: edge-lab.grpc", "id: edge-lab.grpc-web",
		"${{BABELSUITE_ENGINE_GRPC_ADDR}}", "priority: -100", "priority: -90",
		"^application/grpc$", "^application/grpc-web",
		"mode: grpc-check", "grpc_upstream: ${{BABELSUITE_ENGINE_GRPC_ADDR}}",
		"name: grpc-web",
		"X-Babelsuite-Suite: edge-lab", "X-Babelsuite-Ingress: $server_addr",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the gRPC routes lack %q:\n%s", want, body)
		}
	}
	if !ServesGRPC(mockSuite(restSurface, grpcSurface)) || ServesGRPC(mockSuite(restSurface)) {
		t.Fatal("ServesGRPC is wrong")
	}
	if strings.Contains(RenderStandaloneConfig(mockSuite(restSurface)), "scheme: grpc") {
		t.Fatal("a suite with no gRPC mock got a gRPC route")
	}
}

func TestAWebSocketMockIsProxiedToTheEnginesWebSocketEndpoint(t *testing.T) {
	body := RenderStandaloneConfig(mockSuite(wsSurface))
	for _, want := range []string{"enable_websocket: true", "uri: /mocks/ws/edge-lab/live/ticker", "Bearer ${{BABELSUITE_MOCK_TOKEN}}", "${{BABELSUITE_ENGINE_ADDR}}"} {
		if !strings.Contains(body, want) {
			t.Errorf("the WebSocket route lacks %q:\n%s", want, body)
		}
	}
}

// A request/response transport has nothing on the wire to say which operation it
// is for, so each operation gets its own port.
func TestEachTCPAndUDPOperationGetsItsOwnPortAndStreamRoute(t *testing.T) {
	suite := mockSuite(restSurface, tcpSurface, udpSurface)

	endpoints := StreamEndpoints(suite)
	got := map[string]int{}
	for _, endpoint := range endpoints {
		got[endpoint.Transport+"/"+endpoint.Operation] = endpoint.Port
	}
	want := map[string]int{"tcp/ping": 9100, "tcp/status": 9101, "udp/hello": 9200}
	if len(got) != len(want) {
		t.Fatalf("endpoints = %v, want %v", got, want)
	}
	for key, port := range want {
		if got[key] != port {
			t.Errorf("%s is on port %d, want %d (all: %v)", key, got[key], port, got)
		}
	}

	body := RenderStandaloneConfig(suite)
	for _, wantText := range []string{
		"stream_routes:", "server_port: 9100", "server_port: 9101", "server_port: 9200",
		"name: " + TCPMockPluginName, "name: " + UDPMockPluginName,
	} {
		if !strings.Contains(body, wantText) {
			t.Errorf("the config lacks %q:\n%s", wantText, body)
		}
	}
	if !strings.Contains(body, "stream: true") {
		t.Errorf("the stream plugins must be listed as stream plugins:\n%s", body)
	}

	config := SidecarConfig(suite)
	for _, wantText := range []string{"proxy_mode: http&stream", "tcp: [9100, 9101]", "udp: [9200]", "enable_http2: true"} {
		if !strings.Contains(config, wantText) {
			t.Errorf("config.yaml lacks %q:\n%s", wantText, config)
		}
	}
	if plain := SidecarConfig(mockSuite(restSurface)); strings.Contains(plain, "stream") {
		t.Errorf("a suite with no TCP or UDP mock must not turn the stream subsystem on:\n%s", plain)
	}
}

func TestKafkaMocksNeedThePublisherRouteAndNothingElse(t *testing.T) {
	with := RenderStandaloneConfig(mockSuite(kafkaSurface))
	for _, want := range []string{"id: babelsuite-async-publish", "uri: " + AsyncPublishRoute, "name: " + AsyncMockPluginName} {
		if !strings.Contains(with, want) {
			t.Errorf("the publisher route is missing %q:\n%s", want, with)
		}
	}
	if strings.Contains(with, "operation: order-created") {
		t.Fatalf("an async mock is published, not routed:\n%s", with)
	}
	if strings.Contains(RenderStandaloneConfig(mockSuite(restSurface)), AsyncPublishRoute) {
		t.Fatal("a suite with no broker mock got the publisher route")
	}
	if !HasAsyncBrokerMocks(mockSuite(kafkaSurface)) || HasAsyncBrokerMocks(mockSuite(restSurface)) {
		t.Fatal("HasAsyncBrokerMocks is wrong")
	}
}

// The traffic cannon, the attack scanner, user plugins and harden are the other
// uses of the sidecar; none of them may depend on what changed.
func TestTheSidecarsOtherUsesAreStillRendered(t *testing.T) {
	body := RenderStandaloneConfig(SuiteConfig{
		ID:            "edge-lab",
		Harden:        true,
		CustomPlugins: []CustomPluginConfig{{Name: "spice-sim", Trigger: "/_babelsuite/plugins/spice-sim/start", Lua: "return {}"}},
		APISurfaces:   []SurfaceConfig{restSurface, grpcSurface},
	})
	for _, want := range []string{
		"name: " + TrafficCannonPluginName, "name: " + AttackScannerPluginName, "name: spice-sim", "name: " + MockPluginName,
		"name: response-rewrite", "name: limit-count", "name: proxy-rewrite",
		"babelsuite-traffic-trigger", "babelsuite-attack-trigger", "/_babelsuite/plugins/spice-sim/start", "babelsuite-flood-target",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the rendered config lacks %q:\n%s", want, body)
		}
	}
}

func TestTheSidecarServesHTTP2NextToHTTP1AndHasNoAdminAPI(t *testing.T) {
	config := SidecarConfig(mockSuite(restSurface))
	if !strings.Contains(config, "apisix:\n  enable_http2: true\n") {
		t.Fatalf("a gRPC client needs h2c on the gateway's port:\n%s", config)
	}
	if strings.Contains(config, "node_listen") {
		t.Fatal("the per-port HTTP/2 form stops APISIX 3.9 from starting")
	}
	// A standalone data plane has no Admin API; an open one with the shipped key is
	// how APISIX is taken over.
	for _, admin := range []string{"enable_admin", "admin_key", "port_admin", "allow_admin", "admin_listen"} {
		if strings.Contains(config, admin) {
			t.Fatalf("the sidecar must not configure an Admin API (%s):\n%s", admin, config)
		}
	}
	if !strings.Contains(config, "role: data_plane") || !strings.Contains(config, "config_provider: yaml") {
		t.Fatalf("the sidecar must stay a standalone data plane:\n%s", config)
	}
}

func TestPluginNamesStartingWithBabelsuiteAreReserved(t *testing.T) {
	for _, name := range []string{"babelsuite-mock", "babelsuite-anything", "  Babelsuite-X ", "BABELSUITE-TRAFFIC-CANNON"} {
		if !IsReservedPluginName(name) {
			t.Errorf("%q must be reserved", name)
		}
	}
	for _, name := range []string{"spice-sim", "my-babelsuite-plugin", "babelsuite", ""} {
		if IsReservedPluginName(name) {
			t.Errorf("%q must not be reserved", name)
		}
	}
}

func TestEveryPluginFileTheSidecarMountsIsShipped(t *testing.T) {
	files := LuaPluginFiles()
	for _, name := range []string{MockPluginName, AsyncMockPluginName, TrafficCannonPluginName, AttackScannerPluginName} {
		if strings.TrimSpace(files[name+".lua"]) == "" {
			t.Errorf("%s.lua is not shipped", name)
		}
	}
	stream := StreamLuaPluginFiles()
	for _, name := range []string{TCPMockPluginName, UDPMockPluginName} {
		if strings.TrimSpace(stream[name+".lua"]) == "" {
			t.Errorf("stream plugin %s.lua is not shipped", name)
		}
	}
}

func TestRenderIgnoresAUserPluginUnderAReservedName(t *testing.T) {
	t.Parallel()
	suite := mockSuite(restSurface)
	suite.CustomPlugins = []CustomPluginConfig{
		{Name: "babelsuite-mock", Trigger: "/_babelsuite/plugins/babelsuite-mock/start", Lua: "return {}"},
		{Name: "judge", Trigger: "/_babelsuite/plugins/judge/start", Lua: "return {}"},
	}
	body := RenderStandaloneConfig(suite)
	if strings.Contains(body, "/_babelsuite/plugins/babelsuite-mock/start") {
		t.Fatalf("a user plugin shadowed a built-in:\n%s", body)
	}
	if !strings.Contains(body, "/_babelsuite/plugins/judge/start") {
		t.Fatalf("a user plugin was lost:\n%s", body)
	}
}

func TestAsyncPublicationsNameTheBrokerAndTopic(t *testing.T) {
	t.Parallel()
	suite := SuiteConfig{ID: "s", APISurfaces: []SurfaceConfig{
		{ID: "k", Protocol: "async", MockHost: "kafka://Broker:9093", Operations: []OperationConfig{{ID: "created", Name: "/orders/"}}},
		{ID: "m", Protocol: "async", MockHost: "mqtt://mqtt-broker", Operations: []OperationConfig{{ID: "temp"}}},
		{ID: "r", Protocol: "rest", MockHost: "http://api", Operations: []OperationConfig{{ID: "get", Name: "/x"}}},
	}}
	got := AsyncPublications(suite)
	want := []AsyncPublication{
		{Surface: "k", Operation: "created", Kind: "kafka", Host: "broker", Port: 9093, Topic: "orders"},
		{Surface: "m", Operation: "temp", Kind: "mqtt", Host: "mqtt-broker", Port: 1883, Topic: "temp"},
	}
	if len(got) != len(want) {
		t.Fatalf("AsyncPublications = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("publication %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAsyncPublishRouteNeedsTheTokenAndEngineFromTheSidecar(t *testing.T) {
	t.Parallel()
	body := RenderStandaloneConfig(mockSuite(asyncSurfaceForTest()))
	for _, want := range []string{"engine: ${{BABELSUITE_ENGINE_ADDR}}", "token: ${{BABELSUITE_MOCK_TOKEN}}"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func asyncSurfaceForTest() SurfaceConfig {
	return SurfaceConfig{ID: "k", Protocol: "async", MockHost: "kafka://broker:9092", Operations: []OperationConfig{{ID: "created", Name: "orders"}}}
}

// An example suite's surfaces declare no protocol: the operations' adapters say
// what they are. A blank used to be read as REST, so a gRPC or async operation of
// such a surface got a REST route.
func TestASurfaceWithoutAProtocolIsServedByItsOperationsAdapter(t *testing.T) {
	t.Parallel()
	suite := SuiteConfig{ID: "s", APISurfaces: []SurfaceConfig{
		{ID: "pricing", Operations: []OperationConfig{{ID: "quote", MockMetadata: OperationMetadataConfig{Adapter: "grpc"}}}},
		{ID: "events", Operations: []OperationConfig{{ID: "refund", MockMetadata: OperationMetadataConfig{Adapter: "async"}}}},
		{ID: "api", Operations: []OperationConfig{{ID: "get", Name: "/x", Method: "GET", MockMetadata: OperationMetadataConfig{Adapter: "rest"}}}},
	}}
	if !ServesGRPC(suite) {
		t.Fatal("the gRPC operation of a surface with no protocol was not recognised")
	}
	body := RenderStandaloneConfig(suite)
	if !strings.Contains(body, "id: s.grpc") {
		t.Fatalf("no gRPC route was rendered:\n%s", body)
	}
	if strings.Contains(body, "operation: quote") || strings.Contains(body, "operation: refund") {
		t.Fatalf("a gRPC or async operation got a REST route:\n%s", body)
	}
	unserved := UnservedSurfaces(suite)
	if len(unserved) != 1 || unserved[0].Operation != "refund" {
		t.Fatalf("UnservedSurfaces = %+v, want the async operation that names no broker", unserved)
	}
}
