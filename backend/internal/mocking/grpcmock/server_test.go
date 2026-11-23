package grpcmock

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/suites"
)

const pricingProto = `syntax = "proto3";
package demo.v1;

message QuoteRequest { string order = 1; }
message Quote { string id = 1; int32 cents = 2; }

service Pricing {
  rpc QuoteRefund (QuoteRequest) returns (Quote);
  rpc Watch (QuoteRequest) returns (stream Quote);
  rpc Upload (stream QuoteRequest) returns (Quote);
}
`

// refers to a message it never defines, the way an example suite's proto did
const brokenProto = `syntax = "proto3";
package demo.v1;
service Fraud { rpc Screen (ScreenRequest) returns (ScreenResponse); }
`

func grpcOp(id, contract string) suites.APIOperation {
	return suites.APIOperation{ID: id, ContractPath: contract, MockMetadata: suites.MockOperationMetadata{Adapter: "grpc"}}
}

func testSuite() *suites.Definition {
	return &suites.Definition{
		ID: "demo",
		SourceFiles: []suites.SourceFile{
			{Path: "api/proto/pricing.proto", Content: pricingProto},
			{Path: "api/proto/fraud.proto", Content: brokenProto},
		},
		APISurfaces: []suites.APISurface{{
			ID: "pricing", Protocol: "gRPC",
			Operations: []suites.APIOperation{
				grpcOp("quote-refund", "api/proto/pricing.proto#QuoteRefund"),
				grpcOp("watch", ""),  // found by its id: Watch
				grpcOp("upload", ""), // found by its id: Upload
				grpcOp("screen", "api/proto/fraud.proto#Screen"),
				grpcOp("nowhere", "api/proto/missing.proto#Nothing"),
				{ID: "rest-one", MockMetadata: suites.MockOperationMetadata{Adapter: "rest"}},
			},
		}},
	}
}

type suiteReader struct{ suite *suites.Definition }

func (r *suiteReader) Get(id string) (*suites.Definition, error) {
	if id != r.suite.ID {
		return nil, suites.ErrNotFound
	}
	clone := *r.suite
	return &clone, nil
}

type engineCall struct {
	surface, operation, adapter string
	body                        string
	headers                     http.Header
}

type fakeEngine struct {
	mu     sync.Mutex
	answer func(call engineCall) (*mocking.Result, error)
	calls  []engineCall
}

func (e *fakeEngine) InvokeAdapter(_ context.Context, _ string, surfaceID, operationID, adapter string, request *http.Request) (*mocking.Result, error) {
	raw, _ := io.ReadAll(request.Body)
	call := engineCall{surface: surfaceID, operation: operationID, adapter: adapter, body: string(raw), headers: request.Header.Clone()}
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	return e.answer(call)
}

func (e *fakeEngine) last() engineCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls[len(e.calls)-1]
}

const token = "gateway-token"

type rig struct {
	t      *testing.T
	conn   *grpc.ClientConn
	engine *fakeEngine
	server *Server
	index  *Index
}

func newRig(t *testing.T) *rig {
	t.Helper()
	engine := &fakeEngine{answer: func(engineCall) (*mocking.Result, error) {
		return &mocking.Result{Status: 200, Body: []byte(`{"id":"q1","cents":4200}`), Headers: http.Header{"X-Pricing-Path": {"auto"}}}, nil
	}}
	server := New(&suiteReader{suite: testSuite()}, engine, token)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &rig{t: t, conn: conn, engine: engine, server: server, index: Build(testSuite())}
}

func (r *rig) ctx(pairs ...string) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	r.t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func (r *rig) good(extra ...string) context.Context {
	return r.ctx(append([]string{"authorization", "Bearer " + token, "x-babelsuite-suite", "demo"}, extra...)...)
}

func (r *rig) request(method, order string) (proto.Message, proto.Message) {
	entry, ok := r.index.lookup(method)
	if !ok {
		r.t.Fatalf("%s is not served by the test suite: %+v", method, r.index.Problems())
	}
	in := dynamicpb.NewMessage(entry.desc.Input())
	in.Set(in.Descriptor().Fields().ByName("order"), protoreflect.ValueOfString(order))
	return in, dynamicpb.NewMessage(entry.desc.Output())
}

func code(err error) codes.Code { return status.Code(err) }

func TestAUnaryCallGetsTheMockedMessage(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")

	if err := r.conn.Invoke(r.good("x-risk-tier", "high"), "/demo.v1.Pricing/QuoteRefund", in, out); err != nil {
		t.Fatalf("call: %v", err)
	}
	fields := out.ProtoReflect().Descriptor().Fields()
	if got := out.ProtoReflect().Get(fields.ByName("cents")).Int(); got != 4200 {
		t.Fatalf("cents = %d, want 4200", got)
	}
	if got := out.ProtoReflect().Get(fields.ByName("id")).String(); got != "q1" {
		t.Fatalf("id = %q", got)
	}

	call := r.engine.last()
	if call.surface != "pricing" || call.operation != "quote-refund" || call.adapter != "grpc" {
		t.Fatalf("the engine was asked for %+v", call)
	}
	if call.body != `{"order":"o-1"}` && call.body != `{"order": "o-1"}` {
		t.Fatalf("the request reached the engine as %q, want its JSON", call.body)
	}
	if call.headers.Get("X-Risk-Tier") != "high" {
		t.Fatalf("call metadata must reach the engine as headers: %v", call.headers)
	}
	if call.headers.Get("Authorization") != "" {
		t.Fatalf("the gateway token must not be passed on to the engine: %v", call.headers)
	}
	if call.headers.Get("Content-Type") != "application/json" {
		t.Fatalf("the engine expects JSON: %v", call.headers)
	}
}

func TestTheRunsIdentityReachesTheEngineSoItsStateIsScoped(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	if err := r.conn.Invoke(r.good("x-babelsuite-ingress", "172.20.0.2", "x-babelsuite-execution", "run-1"), "/demo.v1.Pricing/QuoteRefund", in, out); err != nil {
		t.Fatal(err)
	}
	call := r.engine.last()
	if call.headers.Get(mocking.HeaderIngress) != "172.20.0.2" || call.headers.Get(mocking.HeaderExecution) != "run-1" {
		t.Fatalf("the run scope headers did not reach the engine: %v", call.headers)
	}
}

func TestAnExampleStatusIsAGRPCCodeWithTheBodysMessage(t *testing.T) {
	r := newRig(t)
	r.engine.answer = func(engineCall) (*mocking.Result, error) {
		return &mocking.Result{Status: 5, Body: []byte(`{"code":"QUOTE_NOT_AVAILABLE","message":"No refund quote mock matched the request."}`)}, nil
	}
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	err := r.conn.Invoke(r.good(), "/demo.v1.Pricing/QuoteRefund", in, out)
	if code(err) != codes.NotFound || !strings.Contains(err.Error(), "No refund quote mock matched the request.") {
		t.Fatalf("got %v, want NotFound with the body's message", err)
	}
}

func TestTheEnginesOwnRefusalsAreMappedToTheNearestCode(t *testing.T) {
	for engineStatus, want := range map[int]codes.Code{
		400: codes.InvalidArgument, 401: codes.Unauthenticated, 403: codes.PermissionDenied, 404: codes.NotFound,
		409: codes.AlreadyExists, 429: codes.ResourceExhausted, 500: codes.Internal, 501: codes.Unimplemented,
		503: codes.Unavailable, 504: codes.DeadlineExceeded, 418: codes.Unknown, 99: codes.Unknown,
		3: codes.InvalidArgument, 16: codes.Unauthenticated,
	} {
		got, _ := statusOf(&mocking.Result{Status: engineStatus})
		if got != want {
			t.Errorf("engine status %d -> %v, want %v", engineStatus, got, want)
		}
	}
	if got, _ := statusOf(&mocking.Result{Status: 200}); got != codes.OK {
		t.Errorf("200 must be OK (the engine reads a code 0 as unset and reports 200), got %v", got)
	}
	if got, _ := statusOf(nil); got != codes.Internal {
		t.Errorf("no answer -> %v, want Internal", got)
	}
}

func TestAServerStreamingMethodAnswersWithTheOneMockedMessageThenEnds(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/Watch", "o-1")
	stream, err := r.conn.NewStream(r.good(), &grpc.StreamDesc{ServerStreams: true}, "/demo.v1.Pricing/Watch")
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.SendMsg(in); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if err := stream.RecvMsg(out); err != nil {
		t.Fatalf("first message: %v", err)
	}
	if err := stream.RecvMsg(dynamicpb.NewMessage(out.ProtoReflect().Descriptor())); err == nil || err.Error() != "EOF" {
		t.Fatalf("the stream must end after the one mocked message, got %v", err)
	}
}

func TestAClientStreamingMethodIsRefusedAsNotMocked(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/Upload", "o-1")
	stream, err := r.conn.NewStream(r.good(), &grpc.StreamDesc{ClientStreams: true}, "/demo.v1.Pricing/Upload")
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.SendMsg(in)
	_ = stream.CloseSend()
	err = stream.RecvMsg(out)
	if code(err) != codes.Unimplemented || !strings.Contains(err.Error(), "streaming requests are not mocked") {
		t.Fatalf("got %v, want Unimplemented saying streaming requests are not mocked", err)
	}
}

func TestAMethodThatIsNotMockedIsRefusedAndNamedInTheError(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	err := r.conn.Invoke(r.good(), "/demo.v1.Pricing/DoesNotExist", in, out)
	if code(err) != codes.FailedPrecondition && code(err) != codes.Unimplemented {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Fatalf("the error must name the method: %v", err)
	}
}

// The suite has one proto that does not compile. The method of the proto that
// does still works, and a call to the broken one says why it cannot.
func TestABrokenProtoDoesNotStopTheOthersAndTheErrorNamesTheFile(t *testing.T) {
	r := newRig(t)

	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	if err := r.conn.Invoke(r.good(), "/demo.v1.Pricing/QuoteRefund", in, out); err != nil {
		t.Fatalf("a good method failed because another proto is broken: %v", err)
	}

	err := r.conn.Invoke(r.good(), "/demo.v1.Fraud/Screen", in, out)
	if code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v, want FailedPrecondition", err)
	}
	for _, want := range []string{"fraud.proto", "does not compile", "ScreenRequest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error must say %q: %v", want, err)
		}
	}
}

func TestCheckReportsEveryOperationThatCannotBeServedWithItsReason(t *testing.T) {
	problems := Check(testSuite())
	got := map[string]string{}
	for _, problem := range problems {
		got[problem.Operation] = problem.Reason
	}
	if len(got) != 2 {
		t.Fatalf("problems = %v, want the broken-proto and the missing-contract operations only", got)
	}
	if !strings.Contains(got["screen"], "fraud.proto does not compile") {
		t.Errorf("screen: %q", got["screen"])
	}
	if !strings.Contains(got["nowhere"], "missing.proto is not among the suite's files") {
		t.Errorf("nowhere: %q", got["nowhere"])
	}
	for _, served := range []string{"quote-refund", "watch", "upload", "rest-one"} {
		if _, bad := got[served]; bad {
			t.Errorf("%s is served (or not gRPC) and must not be reported", served)
		}
	}
}

func TestACallWithoutTheTokenIsRefused(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	for name, ctx := range map[string]context.Context{
		"no token":     r.ctx("x-babelsuite-suite", "demo"),
		"wrong token":  r.ctx("authorization", "Bearer nope", "x-babelsuite-suite", "demo"),
		"old constant": r.ctx("authorization", "Bearer babelsuite-internal", "x-babelsuite-suite", "demo"),
	} {
		if err := r.conn.Invoke(ctx, "/demo.v1.Pricing/QuoteRefund", in, out); code(err) != codes.Unauthenticated {
			t.Errorf("%s: got %v, want Unauthenticated", name, err)
		}
	}
	if len(r.engine.calls) != 0 {
		t.Fatal("the engine was called for a call that carried no token")
	}
}

func TestACallWithNoTokenConfiguredIsRefused(t *testing.T) {
	engine := &fakeEngine{answer: func(engineCall) (*mocking.Result, error) { return &mocking.Result{Status: 200}, nil }}
	server := New(&suiteReader{suite: testSuite()}, engine, "")
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, _ := grpc.NewClient("passthrough:///x", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer ", "x-babelsuite-suite", "demo")
	in, out := newRig(t).request("/demo.v1.Pricing/QuoteRefund", "o")
	if err := conn.Invoke(ctx, "/demo.v1.Pricing/QuoteRefund", in, out); code(err) != codes.Unauthenticated {
		t.Fatalf("got %v, want Unauthenticated", err)
	}
}

func TestACallThatNamesNoSuiteOrAnUnknownOneIsRefusedClearly(t *testing.T) {
	r := newRig(t)
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	if err := r.conn.Invoke(r.ctx("authorization", "Bearer "+token), "/demo.v1.Pricing/QuoteRefund", in, out); code(err) != codes.InvalidArgument {
		t.Errorf("no suite: got %v, want InvalidArgument", err)
	}
	if err := r.conn.Invoke(r.ctx("authorization", "Bearer "+token, "x-babelsuite-suite", "ghost"), "/demo.v1.Pricing/QuoteRefund", in, out); code(err) != codes.NotFound {
		t.Errorf("unknown suite: got %v, want NotFound", err)
	}
}

func TestAnAnswerThatDoesNotFitTheResponseMessageIsAnInternalErrorNamingIt(t *testing.T) {
	r := newRig(t)
	r.engine.answer = func(engineCall) (*mocking.Result, error) {
		return &mocking.Result{Status: 200, Body: []byte(`{"cents":"not a number"}`)}, nil
	}
	in, out := r.request("/demo.v1.Pricing/QuoteRefund", "o-1")
	err := r.conn.Invoke(r.good(), "/demo.v1.Pricing/QuoteRefund", in, out)
	if code(err) != codes.Internal || !strings.Contains(err.Error(), "demo.v1.Quote") {
		t.Fatalf("got %v, want Internal naming demo.v1.Quote", err)
	}
}

func TestReflectionListsTheMockedServicesAndDescribesThem(t *testing.T) {
	r := newRig(t)
	stream, err := rpb.NewServerReflectionClient(r.conn).ServerReflectionInfo(r.good())
	if err != nil {
		t.Fatal(err)
	}
	ask := func(request *rpb.ServerReflectionRequest) *rpb.ServerReflectionResponse {
		if err := stream.Send(request); err != nil {
			t.Fatal(err)
		}
		response, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	list := ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_ListServices{ListServices: "*"}})
	var names []string
	for _, service := range list.GetListServicesResponse().GetService() {
		names = append(names, service.GetName())
	}
	if len(names) != 1 || names[0] != "demo.v1.Pricing" {
		t.Fatalf("services = %v, want only the mocked demo.v1.Pricing", names)
	}

	described := ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: "demo.v1.Pricing"}})
	files := described.GetFileDescriptorResponse().GetFileDescriptorProto()
	if len(files) == 0 {
		t.Fatalf("no descriptor for the service: %+v", described)
	}
	parsed := new(descriptorpb.FileDescriptorProto)
	if err := proto.Unmarshal(files[0], parsed); err != nil || parsed.GetName() != "api/proto/pricing.proto" {
		t.Fatalf("descriptor = %v (%v)", parsed.GetName(), err)
	}

	missing := ask(&rpb.ServerReflectionRequest{MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: "no.Such"}})
	if missing.GetErrorResponse() == nil || missing.GetErrorResponse().GetErrorCode() != int32(codes.NotFound) {
		t.Fatalf("an unknown symbol must be NotFound: %+v", missing)
	}
}

func TestTheCompiledIndexIsKeptUntilTheSuiteChanges(t *testing.T) {
	suite := testSuite()
	server := New(&suiteReader{suite: suite}, &fakeEngine{}, token)
	first, err := server.indexFor("demo")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := server.indexFor("demo")
	if first != again {
		t.Fatal("the protos were compiled again for an unchanged suite")
	}
	suite.SourceFiles[0].Content += "\n// edited\n"
	changed, _ := server.indexFor("demo")
	if changed == first {
		t.Fatal("a changed proto was not compiled again")
	}
}

func TestPascalCase(t *testing.T) {
	for in, want := range map[string]string{"quote-refund": "QuoteRefund", "assign_route": "AssignRoute", "watch": "Watch", "a.b c": "ABC"} {
		if got := pascalCase(in); got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}
