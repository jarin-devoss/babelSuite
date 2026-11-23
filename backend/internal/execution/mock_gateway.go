package execution

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/mocking/grpcmock"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// streamMocksOf lists the suite's TCP and UDP mocks and the ports the gateway
// answers them on, for a step to be told where they are.
func streamMocksOf(suite *suites.Definition) []runner.StreamMock {
	if suite == nil {
		return nil
	}
	endpoints := apisix.StreamEndpoints(suites.ApisixSuiteConfig(*suite))
	if len(endpoints) == 0 {
		return nil
	}
	out := make([]runner.StreamMock, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, runner.StreamMock{
			Surface:   endpoint.Surface,
			Operation: endpoint.Operation,
			Transport: endpoint.Transport,
			Port:      endpoint.Port,
		})
	}
	return out
}

// mockWarnings lists what is wrong with the suite's mocks that the gateway will
// not say on its own: operations it cannot serve, gRPC mocks whose protos do not
// compile, an example status a gRPC client will read differently than intended,
// and async mocks whose broker is not a service of the suite.
func mockWarnings(suite *suites.Definition) []string {
	if suite == nil {
		return nil
	}
	config := suites.ApisixSuiteConfig(*suite)
	var warnings []string

	for _, item := range apisix.UnservedSurfaces(config) {
		warnings = append(warnings, fmt.Sprintf("Mock %s/%s (%s) is not served: %s.", item.Surface, item.Operation, item.Transport, item.Reason))
	}

	if apisix.ServesGRPC(config) {
		for _, problem := range grpcmock.Check(suite) {
			warnings = append(warnings, fmt.Sprintf("gRPC mock %s/%s cannot be served: %s.", problem.Surface, problem.Operation, problem.Reason))
		}
		for _, surface := range suite.APISurfaces {
			for _, operation := range surface.Operations {
				if !isGRPCOperation(surface, operation) {
					continue
				}
				for _, exchange := range operation.Exchanges {
					status := strings.TrimSpace(exchange.ResponseStatus)
					code, err := strconv.Atoi(status)
					if status == "" || err != nil || code == 200 || (code >= 0 && code <= 16) {
						continue
					}
					warnings = append(warnings, fmt.Sprintf("gRPC mock %s/%s has an example with status %s: a gRPC example's status is a gRPC code (0-16), so a client will see a nearby code, not %s.", surface.ID, operation.ID, status, status))
					break
				}
			}
		}
	}

	services := map[string]struct{}{}
	for _, node := range suite.Topology {
		if addressableKind(node.Kind) {
			services[strings.ToLower(networkAlias(node.Name))] = struct{}{}
		}
	}
	for _, publication := range apisix.AsyncPublications(config) {
		if _, found := services[publication.Host]; !found {
			warnings = append(warnings, fmt.Sprintf("Async mock %s/%s publishes to %s %s:%d, which is not a service of this suite, so nothing is published.", publication.Surface, publication.Operation, publication.Kind, publication.Host, publication.Port))
		}
	}
	return warnings
}

func isGRPCOperation(surface suites.APISurface, operation suites.APIOperation) bool {
	return strings.EqualFold(strings.TrimSpace(surface.Protocol), "grpc") ||
		strings.EqualFold(strings.TrimSpace(operation.MockMetadata.Adapter), "grpc")
}

// warnAboutMocks tells a run what its mock surfaces cannot do, once, when the
// first mock node starts.
func (s *Service) warnAboutMocks(executionID string, suite *suites.Definition, node topologyNode) {
	if node.Kind != suites.NodeKindMock || suite == nil {
		return
	}
	for _, other := range suite.Topology {
		if other.Kind == suites.NodeKindMock {
			if other.ID != node.ID {
				return // another mock node comes first and says it
			}
			break
		}
	}
	for index, text := range mockWarnings(suite) {
		s.appendEvent(executionID, ExecutionEvent{
			ID:        fmt.Sprintf("%s-mock-warning-%d", node.ID, index),
			Source:    node.ID,
			Timestamp: s.nextTimestamp(executionID),
			Text:      fmt.Sprintf("[%s] %s", node.Name, text),
			Status:    "running",
			Level:     "warn",
		})
	}
}

// publishAsyncMocks has the gateway publish the suite's async mock messages to a
// broker service whose step has just succeeded.
func (s *Service) publishAsyncMocks(ctx context.Context, executionID string, suite *suites.Definition, profile string, backend runner.Backend, node topologyNode) {
	if suite == nil || node.Kind != suites.NodeKindService {
		return
	}
	publisher, ok := backend.(runner.AsyncPublisher)
	if !ok {
		return
	}
	alias := strings.ToLower(networkAlias(node.Name))
	for _, publication := range apisix.AsyncPublications(suites.ApisixSuiteConfig(*suite)) {
		if publication.Host != alias {
			continue
		}
		err := publisher.PublishAsyncMock(ctx, suite.ID, profile, executionID, node.ID, runner.AsyncMessage{
			Surface:   publication.Surface,
			Operation: publication.Operation,
			Kind:      publication.Kind,
			Port:      publication.Port,
			Topic:     publication.Topic,
		})
		event := ExecutionEvent{
			ID:        fmt.Sprintf("%s-publish-%s-%s", node.ID, publication.Surface, publication.Operation),
			Source:    node.ID,
			Timestamp: s.nextTimestamp(executionID),
			Status:    "running",
		}
		if err != nil {
			event.Level = "warn"
			event.Text = fmt.Sprintf("[%s] Could not publish async mock %s/%s to %s: %v.", node.Name, publication.Surface, publication.Operation, publication.Topic, err)
		} else {
			event.Level = "info"
			event.Text = fmt.Sprintf("[%s] Published async mock %s/%s to %s %s.", node.Name, publication.Surface, publication.Operation, publication.Kind, publication.Topic)
		}
		s.appendEvent(executionID, event)
	}
}
