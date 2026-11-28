package apisix

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func RenderStandaloneConfig(suite SuiteConfig) string {
	routes := buildRoutes(suite)

	// Always include the traffic cannon and attack scanner so the sidecar is
	// ready for load and security steps without any runtime Admin API calls.
	routes = append(routes, trafficTriggerRoute())
	routes = append(routes, attackScannerTriggerRoute())
	if suite.Harden {
		routes = append(routes, floodTargetRoute())
	}
	if HasAsyncBrokerMocks(suite) {
		routes = append(routes, asyncPublishRoute())
	}

	// Register trigger routes for every user-defined Lua plugin.
	for _, p := range userPlugins(suite) {
		routes = append(routes, customPluginTriggerRoute(p))
	}

	document := routeDocument{
		Deployment: deploymentBlock{
			Role: "data_plane",
			RoleDataPlane: deploymentDataPlane{
				ConfigProvider: "yaml",
			},
		},
		Plugins:      buildPluginCatalog(suite),
		Routes:       routes,
		StreamRoutes: streamRoutes(suite),
	}
	if suite.Harden {
		document.GlobalRules = []globalRuleBlock{securityHeadersGlobalRule()}
	}

	body, err := yaml.Marshal(document)
	if err != nil {
		return "deployment:\n  role: data_plane\n  role_data_plane:\n    config_provider: yaml\nroutes: []\n#END\n"
	}

	// ${{NAME}} in a route is resolved by APISIX from the sidecar's environment:
	// BABELSUITE_ENGINE_ADDR and BABELSUITE_ENGINE_GRPC_ADDR are the control
	// plane's mock engine, BABELSUITE_MOCK_TOKEN the secret it requires.
	return strings.TrimRight(string(body), "\n") + "\n#END\n"
}

// buildRoutes returns the routes of the suite's mock operations: one per
// HTTP-family or WebSocket operation, and a shared pair for every gRPC and
// gRPC-Web call. TCP, UDP and async mocks have stream routes or the publisher
// route; an operation the gateway cannot serve gets none (see UnservedSurfaces).
func buildRoutes(suite SuiteConfig) []routeBlock {
	routes := make([]routeBlock, 0)
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			switch transport := transportKind(surface, operation); {
			case isHTTPCompatibleTransport(transport):
				routes = append(routes, httpCompatibleRoute(suite, surface, operation, transport))
			case transport == "websocket":
				routes = append(routes, websocketRoute(suite, surface, operation))
			}
		}
	}
	// A suite this one imports has its HTTP-family and WebSocket mocks served by
	// the same gateway, each on hosts of its own, so a step of the suite being run
	// reaches them by host or by the node's name.
	for _, imported := range suite.Imported {
		scoped := imported
		scoped.hostScoped = true
		for _, surface := range scoped.APISurfaces {
			for _, operation := range surface.Operations {
				switch transport := transportKind(surface, operation); {
				case isHTTPCompatibleTransport(transport):
					routes = append(routes, httpCompatibleRoute(scoped, surface, operation, transport))
				case transport == "websocket":
					routes = append(routes, websocketRoute(scoped, surface, operation))
				}
			}
		}
	}
	if ServesGRPC(suite) {
		routes = append(routes, grpcGatewayRoute(suite), grpcWebRoute(suite))
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].ID < routes[j].ID })
	return routes
}

// securityHeadersGlobalRule applies a baseline set of security response
// headers to every response the sidecar produces, including 404s for
// routes that don't exist — global rules run regardless of whether a
// route matched, unlike per-route plugins.
func securityHeadersGlobalRule() globalRuleBlock {
	return globalRuleBlock{
		ID: "babelsuite-security-headers",
		Plugins: map[string]any{
			"response-rewrite": map[string]any{
				"headers": map[string]any{
					"set": map[string]any{
						"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
						"X-Content-Type-Options":    "nosniff",
						"X-Frame-Options":           "DENY",
						"Content-Security-Policy":   "default-src 'self'",
						"Referrer-Policy":           "strict-origin-when-cross-origin",
						"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
					},
				},
			},
		},
	}
}

func buildPluginCatalog(suite SuiteConfig) []pluginSpec {
	seen := map[string]pluginSpec{
		TrafficCannonPluginName: {Name: TrafficCannonPluginName},
		AttackScannerPluginName: {Name: AttackScannerPluginName},
	}
	if suite.Harden {
		seen["response-rewrite"] = pluginSpec{Name: "response-rewrite"}
		seen["limit-count"] = pluginSpec{Name: "limit-count"}
		seen["echo"] = pluginSpec{Name: "echo"}
	}
	for _, p := range userPlugins(suite) {
		seen[p.Name] = pluginSpec{Name: p.Name}
	}
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			transport := transportKind(surface, operation)
			switch {
			case isHTTPCompatibleTransport(transport):
				seen[MockPluginName] = pluginSpec{Name: MockPluginName}
			case transport == "websocket":
				seen["proxy-rewrite"] = pluginSpec{Name: "proxy-rewrite"}
			case transport == "grpc":
				seen[MockPluginName] = pluginSpec{Name: MockPluginName}
				seen["proxy-rewrite"] = pluginSpec{Name: "proxy-rewrite"}
				seen["grpc-web"] = pluginSpec{Name: "grpc-web"}
			case transport == "tcp":
				seen[TCPMockPluginName] = pluginSpec{Name: TCPMockPluginName, Stream: true}
			case transport == "udp":
				seen[UDPMockPluginName] = pluginSpec{Name: UDPMockPluginName, Stream: true}
			case IsAsyncBroker(transport):
				seen[AsyncMockPluginName] = pluginSpec{Name: AsyncMockPluginName}
			}
		}
	}

	// An imported suite's HTTP-family and WebSocket mocks are served too, so the
	// plugins they need are loaded even when the suite being run has no such mock.
	for _, imported := range suite.Imported {
		for _, surface := range imported.APISurfaces {
			for _, operation := range surface.Operations {
				transport := transportKind(surface, operation)
				switch {
				case isHTTPCompatibleTransport(transport):
					seen[MockPluginName] = pluginSpec{Name: MockPluginName}
				case transport == "websocket":
					seen["proxy-rewrite"] = pluginSpec{Name: "proxy-rewrite"}
				}
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	output := make([]pluginSpec, 0, len(names))
	for _, name := range names {
		output = append(output, seen[name])
	}
	return output
}

// trafficTriggerRoute returns the hidden route that carries the cannon's whole
// protocol (start, status, result, stop) under /_babelsuite/traffic/.  The upstream node is a
// loopback placeholder — the cannon plugin short-circuits the request in the
// access phase and never forwards it.
func trafficTriggerRoute() routeBlock {
	return routeBlock{
		ID:      "babelsuite-traffic-trigger",
		Name:    "babelsuite-traffic-trigger",
		Desc:    "BabelSuite load test trigger — handled entirely by the traffic-cannon Lua plugin.",
		URI:     TrafficCannonRoutePrefix + "*",
		Methods: []string{"GET", "POST"},
		Plugins: map[string]any{
			TrafficCannonPluginName: map[string]any{},
		},
		Upstream: upstreamBlock{
			Type:  "roundrobin",
			Nodes: map[string]int{"127.0.0.1:1": 1},
		},
	}
}

func attackScannerTriggerRoute() routeBlock {
	return routeBlock{
		ID:      "babelsuite-attack-trigger",
		Name:    "babelsuite-attack-trigger",
		Desc:    "BabelSuite security assessment trigger — handled entirely by the attack-scanner Lua plugin.",
		URI:     AttackScannerTriggerRoute,
		Methods: []string{"POST"},
		Plugins: map[string]any{
			AttackScannerPluginName: map[string]any{},
		},
		Upstream: upstreamBlock{
			Type:  "roundrobin",
			Nodes: map[string]int{"127.0.0.1:1": 1},
		},
	}
}

// floodTargetRoute is a synthetic, always-present resource that
// security.flood() checks can target to validate rate-limit (HTTP 429)
// behavior. Suites' mock surfaces rarely configure rate-limiting
// themselves, so the sidecar ships one well-known rate-limited path —
// scoped to this single URI, it never affects requests to any other path.
func floodTargetRoute() routeBlock {
	return routeBlock{
		ID:   "babelsuite-flood-target",
		Name: "babelsuite-flood-target",
		Desc: "Synthetic rate-limited resource for security.flood() checks to validate 429 throttling against.",
		URI:  "/api/v1/resource",
		Plugins: map[string]any{
			"limit-count": map[string]any{
				"count":         20,
				"time_window":   1,
				"rejected_code": 429,
				"key_type":      "var",
				"key":           "remote_addr",
			},
			"echo": map[string]any{
				"body": "ok",
			},
		},
		Upstream: upstreamBlock{
			Type:  "roundrobin",
			Nodes: map[string]int{"127.0.0.1:1": 1},
		},
	}
}

func customPluginTriggerRoute(p CustomPluginConfig) routeBlock {
	return routeBlock{
		ID:      "babelsuite-plugin-" + p.Name,
		Name:    p.Name,
		Desc:    "BabelSuite user plugin trigger for " + p.Name + ".",
		URI:     p.Trigger,
		Methods: []string{"POST"},
		Plugins: map[string]any{
			p.Name: map[string]any{},
		},
		Upstream: upstreamBlock{
			Type:  "roundrobin",
			Nodes: map[string]int{"127.0.0.1:1": 1},
		},
	}
}

// userPlugins are the suite's user-defined plugins that may be rendered. A name
// under the babelsuite- prefix is the gateway's own: the platform refuses to
// register one, and one stored before that rule is left out here so it cannot
// shadow a built-in.
func userPlugins(suite SuiteConfig) []CustomPluginConfig {
	out := make([]CustomPluginConfig, 0, len(suite.CustomPlugins))
	for _, p := range suite.CustomPlugins {
		if IsReservedPluginName(p.Name) {
			continue
		}
		out = append(out, p)
	}
	return out
}
