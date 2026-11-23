package mocking

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/babelsuite/babelsuite/internal/suites"
)

func webSocketSuite() suites.Definition {
	return suites.Definition{
		ID: "edge-lab",
		APISurfaces: []suites.APISurface{{
			ID: "live", Protocol: "websocket", MockHost: "ws://live.mock.internal",
			Operations: []suites.APIOperation{{
				ID: "ticker", Method: "GET", Name: "/ticker",
				MockMetadata: suites.MockOperationMetadata{Adapter: "websocket"},
				Exchanges: []suites.ExchangeExample{{
					Name:              "frames",
					ResponseStatus:    "200",
					ResponseMediaType: "application/json",
					ResponseBody:      `["tick-1","tick-2",{"n":3}]`,
				}},
			}},
		}},
	}
}

func webSocketServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(NewService(staticSuiteReader{suite: webSocketSuite()}), testGatewayToken).Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// dialWebSocket returns the connection and, for a refused upgrade, the status the
// server answered with.
func dialWebSocket(t *testing.T, server *httptest.Server, path, authorization string) (*websocket.Conn, int, error) {
	t.Helper()
	header := http.Header{}
	if authorization != "" {
		header.Set("Authorization", authorization)
	}
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path, header)
	status := 0
	if response != nil {
		status = response.StatusCode
		if response.Body != nil {
			_ = response.Body.Close()
		}
	}
	return conn, status, err
}

func TestWebSocketMockPlaysTheExampleFrames(t *testing.T) {
	server := webSocketServer(t)
	conn, _, err := dialWebSocket(t, server, "/mocks/ws/edge-lab/live/ticker", "Bearer "+testGatewayToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	for _, want := range []string{"tick-1", "tick-2", `{"n":3}`} {
		_, got, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != want {
			t.Fatalf("frame = %q, want %q", got, want)
		}
	}

	// A message the client sends is answered with the example again.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"subscribe":"x"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, got, err := conn.ReadMessage(); err != nil || string(got) != "tick-1" {
		t.Fatalf("answer to a client message = %q, %v", got, err)
	}
}

func TestWebSocketMockRefusesACallerWithoutTheToken(t *testing.T) {
	server := webSocketServer(t)
	for _, authorization := range []string{"", "Bearer wrong"} {
		_, status, err := dialWebSocket(t, server, "/mocks/ws/edge-lab/live/ticker", authorization)
		if err == nil {
			t.Fatalf("a caller with %q was upgraded", authorization)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("status for %q = %d, want 401", authorization, status)
		}
	}
}

func TestWebSocketMockAnswersAnUnknownOperationBeforeUpgrading(t *testing.T) {
	server := webSocketServer(t)
	_, status, err := dialWebSocket(t, server, "/mocks/ws/edge-lab/live/nope", "Bearer "+testGatewayToken)
	if err == nil || status != http.StatusNotFound {
		t.Fatalf("unknown operation: err=%v status=%d, want a 404", err, status)
	}
}
