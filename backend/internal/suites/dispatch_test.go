package suites

import (
	"strings"
	"testing"
)

func TestDefaultDispatcherRules(t *testing.T) {
	cases := []struct {
		name     string
		surface  APISurface
		op       APIOperation
		metadata MockOperationMetadata
		contains []string
	}{
		{
			name:     "REST GET",
			surface:  APISurface{Protocol: "http"},
			op:       APIOperation{Method: "GET", Name: "/orders"},
			contains: []string{"GET", "/orders"},
		},
		{
			name:     "gRPC",
			surface:  APISurface{Protocol: "grpc"},
			op:       APIOperation{Name: "/orders.OrderService/GetOrder"},
			contains: []string{"gRPC"},
		},
		{
			name:     "grpcs alias normalised to gRPC",
			surface:  APISurface{Protocol: "grpcs"},
			op:       APIOperation{Name: "/svc/Method"},
			contains: []string{"gRPC"},
		},
		{
			name:     "async protocol uses event adapter",
			surface:  APISurface{Protocol: "async"},
			op:       APIOperation{Name: "/events/orders"},
			contains: []string{"event adapter"},
		},
		{
			name:     "kafka protocol uses event adapter",
			surface:  APISurface{Protocol: "kafka"},
			op:       APIOperation{Name: "orders.created"},
			contains: []string{"event adapter"},
		},
		{
			name:     "mqtt protocol uses event adapter",
			surface:  APISurface{Protocol: "mqtt"},
			op:       APIOperation{Name: "sensors/temperature"},
			contains: []string{"event adapter"},
		},
		{
			name:     "WebSocket",
			surface:  APISurface{Protocol: "websocket"},
			op:       APIOperation{Name: "/ws/chat"},
			contains: []string{"WebSocket"},
		},
		{
			name:     "SSE",
			surface:  APISurface{Protocol: "sse"},
			op:       APIOperation{Name: "/events/stream"},
			contains: []string{"server-sent events"},
		},
		{
			name:     "GraphQL",
			surface:  APISurface{Protocol: "graphql"},
			op:       APIOperation{Name: "/graphql"},
			contains: []string{"GraphQL"},
		},
		{
			name:     "TCP transport",
			surface:  APISurface{Protocol: "tcp"},
			op:       APIOperation{Name: "/svc/port"},
			contains: []string{"TCP"},
		},
		{
			name:     "UDP transport",
			surface:  APISurface{Protocol: "udp"},
			op:       APIOperation{Name: "/svc/port"},
			contains: []string{"UDP"},
		},
		{
			name:     "SOAP POST",
			surface:  APISurface{Protocol: "SOAP"},
			op:       APIOperation{Method: "POST", Name: "/soap/service"},
			contains: []string{"SOAP", "POST"},
		},
		{
			name:     "empty method defaults to POST",
			surface:  APISurface{Protocol: "http"},
			op:       APIOperation{Method: "", Name: "/rpc"},
			contains: []string{"POST"},
		},
		{
			name:     "custom resolver URL used as resolver path",
			surface:  APISurface{Protocol: "http"},
			op:       APIOperation{Method: "GET", Name: "/items"},
			metadata: MockOperationMetadata{ResolverURL: "http://resolver.internal/custom/path"},
			contains: []string{"/custom/path"},
		},
		{
			name:     "operation Name starting with slash used verbatim as public path",
			surface:  APISurface{Protocol: "http"},
			op:       APIOperation{Method: "GET", Name: "/orders/{id}"},
			contains: []string{"/orders/{id}"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultDispatcherRules("suite-1", tc.surface, tc.op, tc.metadata)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in output, got: %s", want, got)
				}
			}
		})
	}
}

func TestSanitizeIdentifier_AllSymbolsProducesValidPath(t *testing.T) {
	t.Parallel()
	id := sanitizeIdentifier("///")
	if id != "" {
		t.Errorf("expected empty string for all-symbol ID, got %q", id)
	}
	op := APIOperation{ID: "///"}
	path := publicPathForOperation(op)
	if !strings.HasPrefix(path, "/") {
		t.Errorf("publicPathForOperation must return /-prefixed path, got %q", path)
	}
	if path == "" {
		t.Error("publicPathForOperation must not return empty string")
	}
}
