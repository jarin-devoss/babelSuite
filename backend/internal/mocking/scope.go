package mocking

import (
	"context"
	"net/http"
	"strings"
)

// Headers that tell the mock engine which run a request belongs to.
const (
	// HeaderExecution names the run explicitly. A step is given its run's id in
	// BABELSUITE_EXECUTION_ID.
	HeaderExecution = "X-Babelsuite-Execution"
	// HeaderIngress is set by the suite's gateway to the address the request
	// arrived on. The gateway joins every run's network, and each of those
	// networks gives it an address of its own, so the address names the run.
	// The gateway replaces whatever the client sent.
	HeaderIngress = "X-Babelsuite-Ingress"
	// HeaderStateScope reports which state a stateful mock used.
	HeaderStateScope = "X-Babelsuite-State-Scope"
)

const (
	// ScopeExecution means the state belongs to one run.
	ScopeExecution = "execution"
	// ScopeSuite means the request could not be tied to a run, so the state is
	// shared by every run of the suite.
	ScopeSuite = "suite"
)

func suiteScope(suiteID string) string         { return "suite:" + strings.TrimSpace(suiteID) }
func executionScope(executionID string) string { return "exec:" + strings.TrimSpace(executionID) }

func scopeKind(scope string) string {
	if strings.HasPrefix(scope, "exec:") {
		return ScopeExecution
	}
	return ScopeSuite
}

// RegisterExecution tells the engine a run of suiteID exists, so a request that
// names it is given that run's state.
func (s *Service) RegisterExecution(executionID, suiteID string) {
	executionID, suiteID = strings.TrimSpace(executionID), strings.TrimSpace(suiteID)
	if executionID == "" || suiteID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs == nil {
		s.runs = make(map[string]string)
	}
	s.runs[executionID] = suiteID
}

// RegisterIngress records the address the suite's gateway has on the run's
// network, so a request that arrives on it is attributed to the run.
func (s *Service) RegisterIngress(executionID, address string) {
	executionID, address = strings.TrimSpace(executionID), strings.TrimSpace(address)
	if executionID == "" || address == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ingress == nil {
		s.ingress = make(map[string]string)
	}
	s.ingress[address] = executionID
}

// DropExecutionState forgets a finished run: its state and its addresses.
func (s *Service) DropExecutionState(executionID string) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, executionScope(executionID))
	delete(s.runs, executionID)
	for address, owner := range s.ingress {
		if owner == executionID {
			delete(s.ingress, address)
		}
	}
}

// ResetExecutionState clears one run's mock state and nothing else, so another
// run of the same suite keeps its own. A run the engine was not told about has
// no scope of its own, and its reset clears the suite-wide state instead.
func (s *Service) ResetExecutionState(ctx context.Context, executionID, suiteID string) error {
	s.mu.RLock()
	known := s.runs[strings.TrimSpace(executionID)] == strings.TrimSpace(suiteID)
	s.mu.RUnlock()
	if !known {
		return s.ResetSuiteState(ctx, suiteID)
	}
	if _, err := s.suites.Get(strings.TrimSpace(suiteID)); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.state, executionScope(executionID))
	s.mu.Unlock()
	return nil
}

// scopeFor decides whose state a request reads and writes. Only a request the
// engine can tie to a registered run of the same suite is given that run's
// state; anything else, including a header naming a run that does not exist or
// belongs to another suite, gets the suite-wide state.
func (s *Service) scopeFor(suiteID string, request *http.Request) string {
	suiteID = strings.TrimSpace(suiteID)
	if request == nil {
		return suiteScope(suiteID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id := strings.TrimSpace(request.Header.Get(HeaderExecution)); id != "" && s.runs[id] == suiteID {
		return executionScope(id)
	}
	if address := strings.TrimSpace(request.Header.Get(HeaderIngress)); address != "" {
		if id := s.ingress[address]; id != "" && s.runs[id] == suiteID {
			return executionScope(id)
		}
	}
	return suiteScope(suiteID)
}
