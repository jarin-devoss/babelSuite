package mocking

import (
	"context"
	"net/http"
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites"
)

func scopedRequest(headers map[string]string) *http.Request {
	request, _ := http.NewRequest(http.MethodPost, "http://mock.test/orders", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

func TestStateIsKeptPerRunNotPerSuite(t *testing.T) {
	t.Parallel()
	s := newTestService(suites.Definition{ID: "shop"})
	s.RegisterExecution("run-a", "shop")
	s.RegisterExecution("run-b", "shop")
	s.RegisterIngress("run-a", "172.20.0.2")
	s.RegisterIngress("run-b", "172.21.0.2")

	scopeA := s.scopeFor("shop", scopedRequest(map[string]string{HeaderIngress: "172.20.0.2"}))
	scopeB := s.scopeFor("shop", scopedRequest(map[string]string{HeaderIngress: "172.21.0.2"}))
	if scopeA == scopeB {
		t.Fatalf("two runs share the scope %q", scopeA)
	}

	s.storeState(scopeA, "order", map[string]string{"count": "1"})
	if got := s.loadState(scopeB, "order", &suites.MockState{})["count"]; got != "" {
		t.Fatalf("run B sees run A's state: count=%q", got)
	}
	if got := s.loadState(scopeA, "order", &suites.MockState{})["count"]; got != "1" {
		t.Fatalf("run A lost its own state: count=%q", got)
	}
}

func TestResetExecutionStateClearsOnlyThatRun(t *testing.T) {
	t.Parallel()
	s := newTestService(suites.Definition{ID: "shop"})
	s.RegisterExecution("run-a", "shop")
	s.RegisterExecution("run-b", "shop")
	s.storeState(executionScope("run-a"), "k", map[string]string{"v": "a"})
	s.storeState(executionScope("run-b"), "k", map[string]string{"v": "b"})
	s.storeState(suiteScope("shop"), "k", map[string]string{"v": "shared"})

	if err := s.ResetExecutionState(context.Background(), "run-a", "shop"); err != nil {
		t.Fatal(err)
	}
	cfg := &suites.MockState{}
	if got := s.loadState(executionScope("run-a"), "k", cfg)["v"]; got != "" {
		t.Errorf("run A's state survived its reset: %q", got)
	}
	if got := s.loadState(executionScope("run-b"), "k", cfg)["v"]; got != "b" {
		t.Errorf("run B's state was cleared by run A's reset: %q", got)
	}
	if got := s.loadState(suiteScope("shop"), "k", cfg)["v"]; got != "shared" {
		t.Errorf("the shared state was cleared by a run's reset: %q", got)
	}
}

func TestResetByARunTheEngineWasNotToldAboutClearsTheSharedState(t *testing.T) {
	t.Parallel()
	s := newTestService(suites.Definition{ID: "shop"})
	s.storeState(suiteScope("shop"), "k", map[string]string{"v": "shared"})
	if err := s.ResetExecutionState(context.Background(), "run-remote", "shop"); err != nil {
		t.Fatal(err)
	}
	if got := s.loadState(suiteScope("shop"), "k", &suites.MockState{})["v"]; got != "" {
		t.Errorf("a run with no scope of its own must clear the shared state, got %q", got)
	}
}

func TestARequestThatCannotBeTiedToARunUsesTheSuiteState(t *testing.T) {
	t.Parallel()
	s := newTestService(suites.Definition{ID: "shop"})
	s.RegisterExecution("run-a", "shop")
	s.RegisterExecution("run-other", "other-suite")
	s.RegisterIngress("run-a", "172.20.0.2")
	s.RegisterIngress("run-other", "172.30.0.2")

	cases := map[string]*http.Request{
		"no headers":                  scopedRequest(nil),
		"nil request":                 nil,
		"unknown address":             scopedRequest(map[string]string{HeaderIngress: "10.9.9.9"}),
		"unknown run id":              scopedRequest(map[string]string{HeaderExecution: "run-nope"}),
		"a run of another suite":      scopedRequest(map[string]string{HeaderExecution: "run-other"}),
		"an address of another suite": scopedRequest(map[string]string{HeaderIngress: "172.30.0.2"}),
	}
	for name, request := range cases {
		if got := s.scopeFor("shop", request); got != suiteScope("shop") {
			t.Errorf("%s: scope = %q, want the suite's", name, got)
		}
	}

	if got := s.scopeFor("shop", scopedRequest(map[string]string{HeaderExecution: "run-a"})); got != executionScope("run-a") {
		t.Errorf("an explicit run id of this suite: scope = %q", got)
	}
}

func TestDropExecutionStateForgetsTheRun(t *testing.T) {
	t.Parallel()
	s := newTestService(suites.Definition{ID: "shop"})
	s.RegisterExecution("run-a", "shop")
	s.RegisterIngress("run-a", "172.20.0.2")
	s.storeState(executionScope("run-a"), "k", map[string]string{"v": "a"})

	s.DropExecutionState("run-a")

	if got := s.scopeFor("shop", scopedRequest(map[string]string{HeaderIngress: "172.20.0.2"})); got != suiteScope("shop") {
		t.Errorf("a dropped run still owns its address: %q", got)
	}
	s.mu.RLock()
	_, kept := s.state[executionScope("run-a")]
	s.mu.RUnlock()
	if kept {
		t.Error("a finished run's state was kept")
	}
}

func TestScopeKind(t *testing.T) {
	t.Parallel()
	if scopeKind(executionScope("r")) != ScopeExecution || scopeKind(suiteScope("s")) != ScopeSuite {
		t.Fatal("scopeKind does not tell a run's state from a suite's")
	}
}
