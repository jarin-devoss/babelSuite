package grpcmock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// SuiteReader finds a suite by id.
type SuiteReader interface {
	Get(id string) (*suites.Definition, error)
}

// Engine is the mock engine a call is resolved by.
type Engine interface {
	InvokeAdapter(ctx context.Context, suiteID, surfaceID, operationID, adapter string, request *http.Request) (*mocking.Result, error)
}

// Metadata keys a suite's gateway sets on every call it forwards.
const (
	metaSuite = "x-babelsuite-suite"
	metaAuth  = "authorization"
)

// Server is the gRPC mock front end.
type Server struct {
	suites SuiteReader
	engine Engine
	token  string
	server *grpc.Server

	mu    sync.Mutex
	cache map[string]cached // suite id -> compiled index
}

type cached struct {
	digest string
	index  *Index
}

// New builds the front end. token is what every call must present (the same
// secret the HTTP mock endpoints require); with an empty token every call is
// refused.
func New(suites SuiteReader, engine Engine, token string) *Server {
	s := &Server{suites: suites, engine: engine, token: strings.TrimSpace(token), cache: map[string]cached{}}
	s.server = grpc.NewServer(grpc.UnknownServiceHandler(s.handle))
	return s
}

// Serve accepts calls on the listener until Stop.
func (s *Server) Serve(listener net.Listener) error { return s.server.Serve(listener) }

// Stop ends the server and its open calls.
func (s *Server) Stop() { s.server.Stop() }

// indexFor returns the compiled index of a suite, compiling again only when its
// protos or operations changed.
func (s *Server) indexFor(suiteID string) (*Index, error) {
	suite, err := s.suites.Get(suiteID)
	if err != nil {
		return nil, err
	}
	sum := digest(suite)
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.cache[suiteID]; ok && entry.digest == sum {
		return entry.index, nil
	}
	index := Build(suite)
	s.cache[suiteID] = cached{digest: sum, index: index}
	return index, nil
}

func (s *Server) handle(_ any, stream grpc.ServerStream) error {
	ctx := stream.Context()
	incoming, _ := metadata.FromIncomingContext(ctx)

	if !mocking.TokenMatches(s.token, first(incoming, metaAuth)) {
		return status.Error(codes.Unauthenticated, "gRPC mocks are called through the suite's gateway; this call did not carry its token")
	}
	suiteID := first(incoming, metaSuite)
	if suiteID == "" {
		return status.Error(codes.InvalidArgument, "the call does not say which suite it is for (x-babelsuite-suite)")
	}
	fullMethod, ok := grpc.Method(ctx)
	if !ok {
		return status.Error(codes.Internal, "the call has no method")
	}

	index, err := s.indexFor(suiteID)
	if err != nil {
		if errors.Is(err, suites.ErrNotFound) {
			return status.Errorf(codes.NotFound, "suite %q does not exist", suiteID)
		}
		return status.Errorf(codes.Internal, "could not load suite %q: %v", suiteID, err)
	}

	if strings.HasPrefix(fullMethod, "/grpc.reflection.v1.ServerReflection/") {
		return serveReflection(stream, index)
	}

	entry, found := index.lookup(fullMethod)
	if !found {
		return unknownMethod(suiteID, fullMethod, index)
	}
	if entry.desc.IsStreamingClient() {
		return status.Errorf(codes.Unimplemented, "%s streams requests, and streaming requests are not mocked: only unary and server-streaming methods are", fullMethod)
	}

	request := dynamicpb.NewMessage(entry.desc.Input())
	if err := stream.RecvMsg(request); err != nil {
		return status.Errorf(codes.InvalidArgument, "could not read the request: %v", err)
	}
	body, err := protojson.MarshalOptions{UseProtoNames: false}.Marshal(request)
	if err != nil {
		return status.Errorf(codes.Internal, "could not describe the request as JSON: %v", err)
	}

	result, err := s.engine.InvokeAdapter(ctx, suiteID, entry.surface, entry.operation, "grpc", engineRequest(ctx, fullMethod, body, incoming))
	if err != nil {
		return status.Errorf(codes.Internal, "the mock engine could not answer %s: %v", fullMethod, err)
	}

	code, message := statusOf(result)
	if code != codes.OK {
		return status.Error(code, message)
	}

	response := dynamicpb.NewMessage(entry.desc.Output())
	if text := strings.TrimSpace(string(result.Body)); text != "" {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(result.Body, response); err != nil {
			return status.Errorf(codes.Internal, "the mocked answer for %s does not fit its response message %s: %v", fullMethod, entry.desc.Output().FullName(), err)
		}
	}
	if header := responseMetadata(result); len(header) > 0 {
		_ = stream.SetHeader(header)
	}
	return stream.SendMsg(response)
}

// unknownMethod says why a call matched no operation: the method is not mocked,
// or the suite's protos do not compile.
func unknownMethod(suiteID, fullMethod string, index *Index) error {
	if problems := index.Problems(); len(problems) > 0 {
		var reasons []string
		for _, problem := range problems {
			reasons = append(reasons, fmt.Sprintf("%s/%s: %s", problem.Surface, problem.Operation, problem.Reason))
		}
		return status.Errorf(codes.FailedPrecondition, "suite %q has gRPC mocks that cannot be served, so %s may be one of them: %s", suiteID, fullMethod, strings.Join(reasons, "; "))
	}
	return status.Errorf(codes.Unimplemented, "%s is not a gRPC mock of suite %q", fullMethod, suiteID)
}

// engineRequest is the call as the mock engine sees it: a JSON POST whose
// headers are the call's metadata, so constraints, state keys and the run's
// scope work exactly as for HTTP.
func engineRequest(ctx context.Context, fullMethod string, body []byte, incoming metadata.MD) *http.Request {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, fullMethod, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	for key, values := range incoming {
		if strings.HasPrefix(key, ":") || key == metaAuth || key == "content-type" || key == "te" || key == "user-agent" {
			continue
		}
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	return request
}

// statusOf turns the engine's answer into a gRPC status. A mock example's status
// is a gRPC code (0-16); the engine reads a 0 as "unset" and reports 200, and
// reports its own refusals (no example matched, a constraint failed) as HTTP
// statuses, which are mapped to the nearest code.
func statusOf(result *mocking.Result) (codes.Code, string) {
	if result == nil {
		return codes.Internal, "the mock engine returned no answer"
	}
	switch {
	case result.Status == 200:
		return codes.OK, ""
	case result.Status >= 1 && result.Status <= 16:
		return codes.Code(result.Status), messageOf(result.Body, codes.Code(result.Status).String())
	default:
		code := codeForHTTP(result.Status)
		return code, messageOf(result.Body, "the mock answered with status "+strconv.Itoa(result.Status))
	}
}

func codeForHTTP(statusCode int) codes.Code {
	switch statusCode {
	case 400, 422:
		return codes.InvalidArgument
	case 401:
		return codes.Unauthenticated
	case 403:
		return codes.PermissionDenied
	case 404:
		return codes.NotFound
	case 409:
		return codes.AlreadyExists
	case 429:
		return codes.ResourceExhausted
	case 501:
		return codes.Unimplemented
	case 503:
		return codes.Unavailable
	case 504:
		return codes.DeadlineExceeded
	case 500:
		return codes.Internal
	}
	if statusCode >= 200 && statusCode < 300 {
		return codes.OK
	}
	return codes.Unknown
}

// messageOf takes the "message" of a JSON error body, or else the fallback.
func messageOf(body []byte, fallback string) string {
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		for _, key := range []string{"message", "error", "detail"} {
			if text, ok := parsed[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return fallback
}

// responseMetadata passes the answer's x-* headers on as response metadata.
func responseMetadata(result *mocking.Result) metadata.MD {
	out := metadata.MD{}
	for key, values := range result.Headers {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-") {
			out[lower] = append(out[lower], values...)
		}
	}
	return out
}

func first(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}
