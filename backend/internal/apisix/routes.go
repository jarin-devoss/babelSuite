package apisix

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// engineUpstream is where a route that is not answered by a plugin goes: the
// control plane's mock engine.
func engineUpstream() upstreamBlock {
	return upstreamBlock{
		Type:   "roundrobin",
		Scheme: "http",
		Nodes:  map[string]int{engineAddrEnv: 1},
	}
}

// unroutableUpstream is the upstream of a route a plugin answers. APISIX requires
// one, but the request never reaches it: if a plugin ever falls through without
// answering, the connection fails visibly at 127.0.0.1:1 instead of silently
// reaching something real, as the traffic and plugin trigger routes do.
func unroutableUpstream() upstreamBlock {
	return upstreamBlock{Type: "roundrobin", Nodes: map[string]int{"127.0.0.1:1": 1}}
}

// mockPluginConf is the configuration of babelsuite-mock for one operation. The
// ${{NAME}} values are resolved by APISIX from the sidecar's environment.
func mockPluginConf(suite SuiteConfig, surface SurfaceConfig, operation OperationConfig) map[string]any {
	return map[string]any{
		"suite":     suite.ID,
		"surface":   surface.ID,
		"operation": operation.ID,
		"engine":    engineAddrEnv,
		"token":     mockTokenEnv,
	}
}

// httpCompatibleRoute is the route of one HTTP-family mock operation (REST, SOAP,
// GraphQL, webhooks, SSE). The babelsuite-mock plugin asks the control plane's
// mock engine which example matches and writes its answer, so the gateway is the
// visible responder and every other plugin of the route still applies.
func httpCompatibleRoute(suite SuiteConfig, surface SurfaceConfig, operation OperationConfig, _ string) routeBlock {
	return routeBlock{
		ID:      suite.ID + "." + operation.ID,
		Name:    operation.ID,
		Desc:    strings.TrimSpace(operation.Summary),
		URI:     matchURI(operation),
		Methods: []string{httpMethod(operation)},
		Hosts:   routeHosts(suite, surface),
		Plugins: map[string]any{
			MockPluginName: mockPluginConf(suite, surface, operation),
		},
		Upstream: unroutableUpstream(),
	}
}

// websocketRoute is the route of a WebSocket mock. The upgrade cannot be accepted
// by Lua, so it is proxied to the engine's WebSocket endpoint, which plays the
// matched example's frames.
func websocketRoute(suite SuiteConfig, surface SurfaceConfig, operation OperationConfig) routeBlock {
	return routeBlock{
		ID:              suite.ID + "." + operation.ID,
		Name:            operation.ID,
		Desc:            strings.TrimSpace(operation.Summary),
		URI:             matchURI(operation),
		Methods:         []string{"GET"},
		Hosts:           routeHosts(suite, surface),
		EnableWebsocket: true,
		Plugins: map[string]any{
			"proxy-rewrite": map[string]any{
				"uri": "/mocks/ws/" + strings.Trim(suite.ID, "/") + "/" + strings.Trim(surface.ID, "/") + "/" + strings.Trim(operation.ID, "/"),
				"headers": map[string]any{
					"set": map[string]string{
						"Authorization":          "Bearer " + mockTokenEnv,
						"X-Babelsuite-Ingress":   "$server_addr",
						"X-Babelsuite-Operation": operation.ID,
					},
				},
			},
		},
		Upstream: engineUpstream(),
	}
}

// grpcHeaders are what the control plane's gRPC front end needs to know about a
// call: which suite it is for, which run's network it came in on, and the token.
func grpcHeaders(suite SuiteConfig) map[string]any {
	return map[string]any{
		"headers": map[string]any{
			"set": map[string]string{
				"X-Babelsuite-Suite":   suite.ID,
				"X-Babelsuite-Ingress": "$server_addr",
				"Authorization":        "Bearer " + mockTokenEnv,
			},
		},
	}
}

func grpcUpstream() upstreamBlock {
	return upstreamBlock{Type: "roundrobin", Scheme: "grpc", Nodes: map[string]int{engineGRPCAddrEnv: 1}}
}

// grpcGatewayRoute is the one route every gRPC mock of the suite shares. The full
// method a client calls (/pkg.Service/Method) is only known once the suite's
// .proto files are compiled, so the gateway matches every gRPC call and the
// control plane's gRPC front end, which has the protos, picks the operation. It
// is matched on the content type, so it never captures an HTTP mock, and it has
// the lowest priority, so any other route wins over it.
//
// A gRPC success needs an HTTP/2 trailer, which no Lua in APISIX can write, so
// the front end answers; babelsuite-mock only checks the call and turns an
// unreachable front end into a real gRPC error.
func grpcGatewayRoute(suite SuiteConfig) routeBlock {
	return routeBlock{
		ID:       suite.ID + ".grpc",
		Name:     "babelsuite-grpc-mocks",
		Desc:     "gRPC mocks of the suite: forwarded to the control plane's gRPC front end.",
		URI:      "/*",
		Methods:  []string{"POST"},
		Priority: -100,
		Vars:     [][]any{{"http_content_type", "~~", "^application/grpc$"}},
		Plugins: map[string]any{
			MockPluginName: map[string]any{
				"suite":         suite.ID,
				"engine":        engineAddrEnv,
				"token":         mockTokenEnv,
				"mode":          "grpc-check",
				"grpc_upstream": engineGRPCAddrEnv,
			},
			"proxy-rewrite": grpcHeaders(suite),
		},
		Upstream: grpcUpstream(),
	}
}

// grpcWebRoute carries gRPC-Web (a browser's gRPC) to the same front end: the
// grpc-web plugin translates it to gRPC.
func grpcWebRoute(suite SuiteConfig) routeBlock {
	return routeBlock{
		ID:       suite.ID + ".grpc-web",
		Name:     "babelsuite-grpc-web-mocks",
		Desc:     "gRPC-Web mocks of the suite, translated to gRPC for the control plane's front end.",
		URI:      "/*",
		Methods:  []string{"POST", "OPTIONS"},
		Priority: -90,
		Vars:     [][]any{{"http_content_type", "~~", "^application/grpc-web"}},
		Plugins: map[string]any{
			"grpc-web":      map[string]any{},
			"proxy-rewrite": grpcHeaders(suite),
		},
		Upstream: grpcUpstream(),
	}
}

// StreamEndpoint is a TCP or UDP mock and the port the gateway answers it on.
type StreamEndpoint struct {
	Surface   string
	Operation string
	Transport string // "tcp" or "udp"
	Port      int
}

const (
	tcpMockBasePort = 9100
	udpMockBasePort = 9200
)

// StreamEndpoints lists the suite's TCP and UDP mocks with their ports, in a
// stable order: a request/response transport has nothing on the wire to say
// which operation it is for, so each operation gets a port of its own.
func StreamEndpoints(suite SuiteConfig) []StreamEndpoint {
	var out []StreamEndpoint
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			transport := transportKind(surface, operation)
			if transport == "tcp" || transport == "udp" {
				out = append(out, StreamEndpoint{Surface: surface.ID, Operation: operation.ID, Transport: transport})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Transport != out[j].Transport {
			return out[i].Transport < out[j].Transport
		}
		if out[i].Surface != out[j].Surface {
			return out[i].Surface < out[j].Surface
		}
		return out[i].Operation < out[j].Operation
	})
	next := map[string]int{"tcp": tcpMockBasePort, "udp": udpMockBasePort}
	for index := range out {
		out[index].Port = next[out[index].Transport]
		next[out[index].Transport]++
	}
	return out
}

// streamRoutes renders one stream route per TCP or UDP operation.
func streamRoutes(suite SuiteConfig) []streamRouteBlock {
	var out []streamRouteBlock
	for _, endpoint := range StreamEndpoints(suite) {
		conf := map[string]any{
			"suite":     suite.ID,
			"surface":   endpoint.Surface,
			"operation": endpoint.Operation,
			"engine":    engineAddrEnv,
			"token":     mockTokenEnv,
		}
		name := TCPMockPluginName
		if endpoint.Transport == "udp" {
			name = UDPMockPluginName
		} else {
			conf["framing"] = "line"
		}
		out = append(out, streamRouteBlock{
			ID:         suite.ID + "." + endpoint.Operation + "." + endpoint.Transport,
			ServerPort: endpoint.Port,
			Plugins:    map[string]any{name: conf},
			Upstream:   unroutableUpstream(),
		})
	}
	return out
}

// asyncPublishRoute is the trigger route the control plane posts to, to have the
// sidecar publish an async mock's message to the suite's broker.
func asyncPublishRoute() routeBlock {
	return routeBlock{
		ID:      "babelsuite-async-publish",
		Name:    "babelsuite-async-publish",
		Desc:    "BabelSuite async mock publisher: handled entirely by the babelsuite-async-mock Lua plugin.",
		URI:     AsyncPublishRoute,
		Methods: []string{"POST"},
		Plugins: map[string]any{
			AsyncMockPluginName: map[string]any{"engine": engineAddrEnv, "token": mockTokenEnv},
		},
		Upstream: unroutableUpstream(),
	}
}

// UnservedSurface is a mock operation the gateway cannot serve.
type UnservedSurface struct {
	Surface   string
	Operation string
	Transport string
	Reason    string
}

// IsAsyncBroker reports whether a transport is published to a broker.
func IsAsyncBroker(transport string) bool {
	switch transport {
	case "kafka", "rocketmq", "mqtt", "nats":
		return true
	}
	return false
}

// ServesGRPC reports whether the suite declares a gRPC mock.
func ServesGRPC(suite SuiteConfig) bool {
	return suiteHasTransport(suite, func(transport string) bool { return transport == "grpc" })
}

// HasAsyncBrokerMocks reports whether the suite declares a mock published to a
// broker, which needs the sidecar's publisher route.
func HasAsyncBrokerMocks(suite SuiteConfig) bool {
	return suiteHasTransport(suite, IsAsyncBroker)
}

func suiteHasTransport(suite SuiteConfig, match func(string) bool) bool {
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			if match(transportKind(surface, operation)) {
				return true
			}
		}
	}
	return false
}

// UnservedSurfaces lists the mock operations the gateway cannot serve.
//
// It serves the HTTP family (REST, SOAP, GraphQL, webhooks, SSE), WebSocket, gRPC
// and gRPC-Web, TCP and UDP, and it publishes async mocks to a broker (Kafka,
// RocketMQ, MQTT, NATS). What is left cannot be done by APISIX at all: AMQP has
// no client in the image, and an async surface that names no broker has nowhere
// to publish.
func UnservedSurfaces(suite SuiteConfig) []UnservedSurface {
	var out []UnservedSurface
	for _, imported := range suite.Imported {
		for _, surface := range imported.APISurfaces {
			for _, operation := range surface.Operations {
				transport := transportKind(surface, operation)
				if isHTTPCompatibleTransport(transport) || transport == "websocket" {
					continue
				}
				out = append(out, UnservedSurface{
					Surface:   imported.ID + "/" + surface.ID,
					Operation: operation.ID,
					Transport: transportDisplayName(transport),
					Reason:    "the imported suite " + imported.ID + " is served for its HTTP-family and WebSocket mocks only; " + transportDisplayName(transport) + " mocks are served for the suite being run",
				})
			}
		}
	}
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			transport := transportKind(surface, operation)
			if servedTransport(transport) {
				continue
			}
			reason := "the gateway serves HTTP-family, WebSocket, gRPC, TCP and UDP mocks and publishes Kafka, RocketMQ, MQTT and NATS ones; " + transportDisplayName(transport) + " is none of these"
			switch transport {
			case "amqp":
				reason = "AMQP is not served: APISIX has no AMQP client to publish with"
			case "async":
				reason = "the surface names no broker (a kafka://, rocketmq://, mqtt:// or nats:// host), so there is nowhere to publish"
			}
			out = append(out, UnservedSurface{
				Surface:   surface.ID,
				Operation: operation.ID,
				Transport: transportDisplayName(transport),
				Reason:    reason,
			})
		}
	}
	return out
}

func servedTransport(transport string) bool {
	return isHTTPCompatibleTransport(transport) || transport == "websocket" || transport == "grpc" ||
		transport == "tcp" || transport == "udp" || IsAsyncBroker(transport)
}

// portString is a port as it appears in a listener list.
func portString(port int) string { return strconv.Itoa(port) }

// AsyncPublication is an async mock message and the broker it is published to.
type AsyncPublication struct {
	Surface   string
	Operation string
	Kind      string // kafka, rocketmq, mqtt or nats
	Host      string // the broker's host as the surface names it
	Port      int
	Topic     string
}

var defaultBrokerPorts = map[string]int{"kafka": 9092, "rocketmq": 9876, "mqtt": 1883, "nats": 4222}

// AsyncPublications lists the suite's async mocks that name a broker. The
// surface's mock host is the broker (kafka://broker:9092) and the operation's
// name is the topic or subject; a surface that gives no port is taken to listen
// on the broker's usual one.
func AsyncPublications(suite SuiteConfig) []AsyncPublication {
	var out []AsyncPublication
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			kind := transportKind(surface, operation)
			if !IsAsyncBroker(kind) {
				continue
			}
			host, port := brokerAddress(surface, operation)
			if port == 0 {
				port = defaultBrokerPorts[kind]
			}
			topic := strings.Trim(strings.TrimSpace(operation.Name), "/")
			if topic == "" {
				topic = operation.ID
			}
			out = append(out, AsyncPublication{
				Surface: surface.ID, Operation: operation.ID, Kind: kind,
				Host: strings.ToLower(host), Port: port, Topic: topic,
			})
		}
	}
	return out
}

func brokerAddress(surface SurfaceConfig, operation OperationConfig) (string, int) {
	for _, raw := range []string{surface.MockHost, operation.MockURL} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		port, _ := strconv.Atoi(parsed.Port())
		return parsed.Hostname(), port
	}
	return "", 0
}
