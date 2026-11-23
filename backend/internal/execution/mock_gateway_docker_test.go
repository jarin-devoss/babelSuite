package execution

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/mocking/grpcmock"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

const gatewayToken = "gateway-lab-token"

const gatewayLabProto = `syntax = "proto3";
package demo.v1;
message QuoteRequest { string order = 1; }
message Quote { string id = 1; int32 cents = 2; }
service Pricing {
  rpc QuoteRefund (QuoteRequest) returns (Quote);
  rpc Refuse (QuoteRequest) returns (Quote);
}
`

func gatewayLabDefinition(t *testing.T) suites.Definition {
	t.Helper()
	base, err := suites.NewWorkspaceService().Get("storefront-browser-lab")
	if err != nil {
		t.Fatalf("load a base suite: %v", err)
	}
	definition := *base
	definition.ID = "gateway-lab"
	definition.SuiteStar = strings.Join([]string{
		`load("@babelsuite/runtime", "mock", "test")`,
		`m = mock.api(name="shop-mock")`,
		`worker = test.run(name="worker", image="busybox", after=[m], commands=[`,
		`  "echo BYNAME=$(wget -qO- http://shop-mock:9080/items)",`,
		`  "echo BYGATEWAY=$(wget -qO- --header 'Host: shop.mock.internal' http://babelsuite-gateway:9080/items)",`,
		`  "echo TCPMOCKS=$BABELSUITE_TCP_MOCKS",`,
		`  "echo UDPMOCKS=$BABELSUITE_UDP_MOCKS",`,
		`])`,
	}, "\n")
	definition.Profiles = []suites.ProfileOption{{FileName: "local.yaml", Default: true}}
	definition.SourceFiles = []suites.SourceFile{{Path: "api/proto/pricing.proto", Content: gatewayLabProto}}
	definition.SeedSources = nil

	exchange := func(status, body string) []suites.ExchangeExample {
		return []suites.ExchangeExample{{Name: "example", ResponseStatus: status, ResponseMediaType: "application/json", ResponseBody: body}}
	}
	definition.APISurfaces = []suites.APISurface{
		{ID: "shop", Protocol: "rest", MockHost: "http://shop.mock.internal", Operations: []suites.APIOperation{
			{ID: "list-items", Method: "GET", Name: "/items", MockMetadata: suites.MockOperationMetadata{Adapter: "rest"}, Exchanges: exchange("200", `{"items":["a"]}`)},
		}},
		{ID: "live", Protocol: "websocket", MockHost: "ws://live.mock.internal", Operations: []suites.APIOperation{
			{ID: "ticker", Method: "GET", Name: "/ticker", MockMetadata: suites.MockOperationMetadata{Adapter: "websocket"}, Exchanges: exchange("200", `["tick-1"]`)},
		}},
		{ID: "pricing", Protocol: "grpc", Operations: []suites.APIOperation{
			{ID: "quote-refund", ContractPath: "api/proto/pricing.proto#QuoteRefund", MockMetadata: suites.MockOperationMetadata{Adapter: "grpc"}, Exchanges: exchange("200", `{"id":"q1","cents":4200}`)},
			{ID: "refuse", ContractPath: "api/proto/pricing.proto#Refuse", MockMetadata: suites.MockOperationMetadata{Adapter: "grpc"}, Exchanges: exchange("5", `{"message":"no such order"}`)},
		}},
		{ID: "ticks", Protocol: "tcp", Operations: []suites.APIOperation{
			{ID: "quote", Name: "quote", Exchanges: []suites.ExchangeExample{{Name: "example", ResponseStatus: "200", ResponseMediaType: "text/plain", ResponseBody: "PRICE 42"}}},
		}},
		{ID: "dns", Protocol: "udp", Operations: []suites.APIOperation{
			{ID: "lookup", Name: "lookup", Exchanges: []suites.ExchangeExample{{Name: "example", ResponseStatus: "200", ResponseMediaType: "text/plain", ResponseBody: "10.0.0.7"}}},
		}},
	}
	return definition
}

func listenAll(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

// Every protocol the gateway serves is driven through a real APISIX sidecar made
// by the control plane's own configuration: REST, WebSocket, gRPC (a success and
// an error), TCP and UDP answered from the mock engine; the mock is reached by
// its node's name from a run; the engine refuses a call that does not carry the
// token; and the sidecar publishes an async mock to a NATS and an MQTT broker.
func TestTheSidecarServesEveryProtocolThroughRealAPISIX(t *testing.T) {
	requireDocker(t)
	definition := gatewayLabDefinition(t)
	reader := mockSuiteReader{suite: definition}

	runner.ConfigureSidecarSecrets(gatewayToken)
	t.Cleanup(func() { runner.ConfigureSidecarSecrets("") })

	engine := mocking.NewService(reader)
	mux := http.NewServeMux()
	mocking.NewHandler(engine, gatewayToken).Register(mux)
	httpListener := listenAll(t)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(httpListener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+strconv.Itoa(httpListener.Addr().(*net.TCPAddr).Port))

	grpcListener := listenAll(t)
	grpcServer := grpcmock.New(reader, engine, gatewayToken)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	t.Cleanup(grpcServer.Stop)
	t.Setenv("BABELSUITE_ENGINE_GRPC_ADDR", "host.docker.internal:"+strconv.Itoa(grpcListener.Addr().(*net.TCPAddr).Port))

	sidecar := runner.SidecarContainerName(definition.ID, "local.yaml")
	_ = exec.Command("docker", "rm", "-f", sidecar).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })

	service := NewServiceWithPlatform(staticSuiteSource{items: map[string]suites.Definition{definition.ID: definition}},
		stubPlatformSource{settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{{
				AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true,
				APISIXSidecar: platform.APISIXSidecarConfig{
					Image:           "apache/apisix:3.9.0-debian",
					ConfigMountPath: "/usr/local/apisix/conf/apisix.yaml",
					ListenPort:      9080,
					AdminPort:       9180,
				},
			}},
		}})
	service.ConfigureMockResetter(engine)
	defer service.Close()

	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: definition.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create the execution: %v", err)
	}
	if snapshot := waitForExecutionTerminal(t, service, execution.ID); snapshot.Status != "Healthy" {
		t.Fatalf("the run ended %s, want Healthy", snapshot.Status)
	}

	// What a step on the run's network saw.
	lines, err := service.SnapshotLogs(execution.ID, "")
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	var logText strings.Builder
	for _, entry := range lines {
		logText.WriteString(entry.Text + "\n")
	}
	logs := logText.String()
	for _, want := range []string{
		`BYNAME={"items":["a"]}`,    // the mock is reachable by its node's name
		`BYGATEWAY={"items":["a"]}`, // and through the gateway's own alias
		"ticks/quote=babelsuite-gateway:9100",
		"dns/lookup=babelsuite-gateway:9200",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("the step's output lacks %q:\n%s", want, logs)
		}
	}

	gateway := runner.SuiteSidecarURL(definition.ID, "local.yaml")
	if gateway == "" {
		t.Fatal("the sidecar publishes no port")
	}
	hostPort := strings.TrimPrefix(gateway, "http://")

	t.Run("a direct call to the engine without the token is refused", func(t *testing.T) {
		response, err := http.Post("http://127.0.0.1:"+strconv.Itoa(httpListener.Addr().(*net.TCPAddr).Port)+"/mocks/rest/gateway-lab/shop/items", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.StatusCode)
		}
	})

	t.Run("REST answers through the gateway plugin", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, gateway+"/items", nil)
		request.Host = "shop.mock.internal"
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || !strings.Contains(string(body), `"items"`) {
			t.Fatalf("status %d body %s", response.StatusCode, body)
		}
	})

	t.Run("WebSocket frames come from the engine", func(t *testing.T) {
		header := http.Header{}
		header.Set("Host", "live.mock.internal")
		conn, handshake, err := websocket.DefaultDialer.Dial("ws://"+hostPort+"/ticker", header)
		if handshake != nil && handshake.Body != nil {
			_ = handshake.Body.Close()
		}
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, frame, err := conn.ReadMessage(); err != nil || string(frame) != "tick-1" {
			t.Fatalf("frame = %q, %v", frame, err)
		}
	})

	t.Run("gRPC answers a success and an error", func(t *testing.T) {
		conn, err := grpc.NewClient(hostPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		reply := &wrapperspb.StringValue{}
		if err := conn.Invoke(ctx, "/demo.v1.Pricing/QuoteRefund", wrapperspb.String("ord_1"), reply); err != nil {
			t.Fatalf("QuoteRefund: %v", err)
		}
		if reply.GetValue() != "q1" {
			t.Fatalf("quote id = %q, want q1", reply.GetValue())
		}

		err = conn.Invoke(ctx, "/demo.v1.Pricing/Refuse", wrapperspb.String("ord_1"), &wrapperspb.StringValue{})
		if status.Code(err) != codes.NotFound || !strings.Contains(err.Error(), "no such order") {
			t.Fatalf("Refuse = %v, want NotFound: no such order", err)
		}

		err = conn.Invoke(ctx, "/demo.v1.Pricing/Missing", wrapperspb.String("x"), &wrapperspb.StringValue{})
		if status.Code(err) != codes.Unimplemented {
			t.Fatalf("an unmocked method = %v, want Unimplemented", err)
		}
	})

	t.Run("TCP is answered by the stream plugin", func(t *testing.T) {
		port := runner.SuiteSidecarHostPort(definition.ID, "local.yaml", "9100/tcp")
		if port == "" {
			t.Fatal("the TCP mock port is not published")
		}
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write([]byte("QUOTE\n")); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "PRICE 42" {
			t.Fatalf("answer = %q, %v", line, err)
		}
	})

	t.Run("UDP is answered by the stream plugin", func(t *testing.T) {
		port := runner.SuiteSidecarHostPort(definition.ID, "local.yaml", "9200/udp")
		if port == "" {
			t.Fatal("the UDP mock port is not published")
		}
		conn, err := net.DialTimeout("udp", "127.0.0.1:"+port, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write([]byte("lookup")); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 512)
		n, err := conn.Read(buffer)
		if err != nil || !strings.Contains(string(buffer[:n]), "10.0.0.7") {
			t.Fatalf("answer = %q, %v", buffer[:n], err)
		}
	})
}

// fakeBroker accepts one connection, plays the broker's greeting and records
// what the client sent.
type fakeBroker struct {
	listener net.Listener
	greeting string
	mu       sync.Mutex
	received []byte
}

func newFakeBroker(t *testing.T, greeting string) *fakeBroker {
	t.Helper()
	broker := &fakeBroker{listener: listenAll(t), greeting: greeting}
	go func() {
		for {
			conn, err := broker.listener.Accept()
			if err != nil {
				return
			}
			go broker.serve(conn)
		}
	}()
	return broker
}

func (b *fakeBroker) port() int { return b.listener.Addr().(*net.TCPAddr).Port }

func (b *fakeBroker) data() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.received)
}

func (b *fakeBroker) serve(conn net.Conn) {
	defer conn.Close()
	if b.greeting != "" {
		_, _ = conn.Write([]byte(b.greeting))
	}
	buffer := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buffer)
		if n > 0 {
			b.mu.Lock()
			b.received = append(b.received, buffer[:n]...)
			b.mu.Unlock()
			chunk := string(buffer[:n])
			switch {
			case strings.Contains(chunk, "PING"):
				_, _ = conn.Write([]byte("PONG\r\n")) // NATS
			case len(chunk) > 0 && chunk[0] == 0x10:
				_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00}) // MQTT CONNACK, accepted
			}
		}
		if err != nil {
			return
		}
	}
}

// The sidecar publishes an async mock to the broker it is told about, and only
// when the request carries the mock token.
func TestTheSidecarPublishesAnAsyncMockToANATSAndAnMQTTBroker(t *testing.T) {
	requireDocker(t)
	definition := gatewayLabDefinition(t)
	definition.APISurfaces = []suites.APISurface{
		{ID: "events", Protocol: "async", MockHost: "nats://broker:4222", Operations: []suites.APIOperation{
			{ID: "order-created", Name: "orders.created", MockMetadata: suites.MockOperationMetadata{Adapter: "async"},
				Exchanges: []suites.ExchangeExample{{Name: "example", ResponseStatus: "200", ResponseMediaType: "application/json", ResponseBody: `{"order":"o1"}`}}},
		}},
		{ID: "telemetry", Protocol: "async", MockHost: "mqtt://broker:1883", Operations: []suites.APIOperation{
			{ID: "temperature", Name: "sensors/temp", MockMetadata: suites.MockOperationMetadata{Adapter: "async"},
				Exchanges: []suites.ExchangeExample{{Name: "example", ResponseStatus: "200", ResponseMediaType: "application/json", ResponseBody: `{"celsius":21}`}}},
		}},
	}
	definition.SuiteStar = strings.Join([]string{
		`load("@babelsuite/runtime", "mock", "test")`,
		`m = mock.api(name="events-mock")`,
		`t = test.run(name="noop", image="busybox", after=[m], commands=["true"])`,
	}, "\n")
	reader := mockSuiteReader{suite: definition}

	runner.ConfigureSidecarSecrets(gatewayToken)
	t.Cleanup(func() { runner.ConfigureSidecarSecrets("") })

	engine := mocking.NewService(reader)
	mux := http.NewServeMux()
	mocking.NewHandler(engine, gatewayToken).Register(mux)
	httpListener := listenAll(t)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(httpListener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+strconv.Itoa(httpListener.Addr().(*net.TCPAddr).Port))

	sidecar := runner.SidecarContainerName(definition.ID, "local.yaml")
	_ = exec.Command("docker", "rm", "-f", sidecar).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })

	service := NewServiceWithPlatform(staticSuiteSource{items: map[string]suites.Definition{definition.ID: definition}},
		stubPlatformSource{settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{{
				AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true,
				APISIXSidecar: platform.APISIXSidecarConfig{Image: "apache/apisix:3.9.0-debian", ConfigMountPath: "/usr/local/apisix/conf/apisix.yaml", ListenPort: 9080, AdminPort: 9180},
			}},
		}})
	service.ConfigureMockResetter(engine)
	defer service.Close()
	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: definition.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := waitForExecutionTerminal(t, service, execution.ID); snapshot.Status != "Healthy" {
		if record, err := service.GetExecution(execution.ID, ""); err == nil {
			for _, event := range record.Events {
				t.Logf("%s [%s] %s", event.Level, event.Source, event.Text)
			}
		}
		t.Fatalf("the run ended %s, want Healthy", snapshot.Status)
	}

	nats := newFakeBroker(t, "INFO {\"server_id\":\"fake\"}\r\n")
	mqtt := newFakeBroker(t, "")
	trigger := runner.SuiteSidecarURL(definition.ID, "local.yaml") + "/_babelsuite/async/publish"

	post := func(authorization, body string) (int, string) {
		request, _ := http.NewRequest(http.MethodPost, trigger, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		answer, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(answer)
	}
	message := func(surface, operation, kind string, port int, topic string) string {
		return `{"suite":"gateway-lab","surface":"` + surface + `","operation":"` + operation + `","kind":"` + kind +
			`","host":"host.docker.internal","port":` + strconv.Itoa(port) + `,"topic":"` + topic + `"}`
	}

	if code, _ := post("", message("events", "order-created", "nats", nats.port(), "orders.created")); code != http.StatusUnauthorized {
		t.Fatalf("a trigger without the token: status %d, want 401", code)
	}
	if code, _ := post("Bearer wrong", message("events", "order-created", "nats", nats.port(), "orders.created")); code != http.StatusUnauthorized {
		t.Fatalf("a trigger with a wrong token: status %d, want 401", code)
	}

	if code, answer := post("Bearer "+gatewayToken, message("events", "order-created", "nats", nats.port(), "orders.created")); code != http.StatusOK {
		t.Fatalf("NATS publish: status %d %s", code, answer)
	}
	if got := nats.data(); !strings.Contains(got, "PUB orders.created") || !strings.Contains(got, `{"order":"o1"}`) {
		t.Fatalf("the NATS broker received %q", got)
	}

	if code, answer := post("Bearer "+gatewayToken, message("telemetry", "temperature", "mqtt", mqtt.port(), "sensors/temp")); code != http.StatusOK {
		t.Fatalf("MQTT publish: status %d %s", code, answer)
	}
	if got := mqtt.data(); !strings.Contains(got, "sensors/temp") || !strings.Contains(got, `{"celsius":21}`) {
		t.Fatalf("the MQTT broker received %q", got)
	}
}

// A run whose suite declares a NATS service and an async mock on it has the
// gateway publish the mock's message once the broker is up: the broker is reached
// by its address on the run's network, and the broker itself counts the message.
func TestARunPublishesItsAsyncMockToTheBrokerItStarted(t *testing.T) {
	requireDocker(t)
	definition := gatewayLabDefinition(t)
	definition.APISurfaces = []suites.APISurface{
		{ID: "events", Protocol: "async", MockHost: "nats://broker:4222", Operations: []suites.APIOperation{
			{ID: "order-created", Name: "orders.created", MockMetadata: suites.MockOperationMetadata{Adapter: "async"},
				Exchanges: []suites.ExchangeExample{{Name: "example", ResponseStatus: "200", ResponseMediaType: "application/json", ResponseBody: `{"order":"o1"}`}}},
		}},
	}
	definition.SuiteStar = strings.Join([]string{
		`load("@babelsuite/runtime", "mock", "service", "test")`,
		`broker = service.run(name="broker", image="nats:2.10-alpine", commands=["nats-server -m 8222"])`,
		`m = mock.api(name="events-mock", after=[broker])`,
		`check = test.run(name="check", image="busybox", after=[m], commands=[`,
		`  "for i in 1 2 3 4 5 6 7 8 9 10; do wget -qO- http://broker:8222/varz | grep -q '\"in_msgs\": 1' && echo BROKERGOT=1 && exit 0; sleep 1; done; echo BROKERGOT=0; exit 0",`,
		`])`,
	}, "\n")
	reader := mockSuiteReader{suite: definition}

	runner.ConfigureSidecarSecrets(gatewayToken)
	t.Cleanup(func() { runner.ConfigureSidecarSecrets("") })
	engine := mocking.NewService(reader)
	mux := http.NewServeMux()
	mocking.NewHandler(engine, gatewayToken).Register(mux)
	httpListener := listenAll(t)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(httpListener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+strconv.Itoa(httpListener.Addr().(*net.TCPAddr).Port))

	sidecar := runner.SidecarContainerName(definition.ID, "local.yaml")
	_ = exec.Command("docker", "rm", "-f", sidecar).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })

	service := NewServiceWithPlatform(staticSuiteSource{items: map[string]suites.Definition{definition.ID: definition}},
		stubPlatformSource{settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{{
				AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true,
				APISIXSidecar: platform.APISIXSidecarConfig{Image: "apache/apisix:3.9.0-debian", ConfigMountPath: "/usr/local/apisix/conf/apisix.yaml", ListenPort: 9080, AdminPort: 9180},
			}},
		}})
	service.ConfigureMockResetter(engine)
	defer service.Close()
	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: definition.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := waitForExecutionTerminal(t, service, execution.ID)

	var events strings.Builder
	if record, err := service.GetExecution(execution.ID, ""); err == nil {
		for _, event := range record.Events {
			events.WriteString(event.Level + " [" + event.Source + "] " + event.Text + "\n")
		}
	}
	if snapshot.Status != "Healthy" {
		t.Fatalf("the run ended %s, want Healthy:\n%s", snapshot.Status, events.String())
	}
	if !strings.Contains(events.String(), "Published async mock events/order-created to nats orders.created") {
		t.Fatalf("the run does not say it published the message:\n%s", events.String())
	}
	lines, _ := service.SnapshotLogs(execution.ID, "")
	var logs strings.Builder
	for _, entry := range lines {
		logs.WriteString(entry.Text + "\n")
	}
	if !strings.Contains(logs.String(), "BROKERGOT=1") {
		t.Fatalf("the broker did not count the message:\n%s\n%s", logs.String(), events.String())
	}
}
