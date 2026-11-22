package suites

import (
	"strings"
	"testing"
)

func resolveSecuritySuite(t *testing.T, body string) ([]TopologyNode, error) {
	t.Helper()
	suite := Definition{
		ID:    "sec-suite",
		Title: "Sec Suite",
		SuiteStar: `load("@babelsuite/runtime", "mock", "service", "security")
` + body,
	}
	return ResolveTopology(suite, []Definition{suite})
}

func securityNode(t *testing.T, nodes []TopologyNode, name string) TopologyNode {
	t.Helper()
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("no node %q in %+v", name, nodes)
	return TopologyNode{}
}

func TestSecurityCallRefusesAnArgumentItDoesNotKnow(t *testing.T) {
	t.Parallel()
	_, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
flood = security.flood(name="flood", after=[api], expect_throtle=True)`)
	if err == nil || !strings.Contains(err.Error(), `unknown argument "expect_throtle"`) || !strings.Contains(err.Error(), "expect_throttle") {
		t.Fatalf("a typo must be named and the allowed arguments listed, got %v", err)
	}
}

func TestSecurityCallRefusesAnArgumentOfAnotherCall(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`security.probe(name="p", after=[api], technique="sqli")`,
		`security.headers(name="h", after=[api], rate=10.0)`,
		`security.fuzz(name="f", after=[api], expect_throttle=True)`,
		`security.cors(name="c", after=[api], thresholds=[])`,
	} {
		_, err := resolveSecuritySuite(t, "api = mock.api(name=\"api\")\n"+body)
		if err == nil || !strings.Contains(err.Error(), "unknown argument") {
			t.Fatalf("%s: expected the argument to be refused, got %v", body, err)
		}
	}
}

func TestSecurityCallAcceptsItsOwnArguments(t *testing.T) {
	t.Parallel()
	nodes, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
fuzz  = security.fuzz(name="fuzz", after=[api], technique="sqli", target="https://api.example.test", fail_on="high", exports=[{"path": "/findings/fuzz.json", "on": "always"}])
flood = security.flood(name="flood", after=[api], path="/api/v1/resource", rate=50.0, duration=5.0, expect_throttle=True, continue_on_failure=True)`)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	fuzz := securityNode(t, nodes, "fuzz").Security
	if fuzz.Technique != "sqli" || fuzz.FailOn != "high" || fuzz.Target != "https://api.example.test" {
		t.Fatalf("unexpected fuzz spec %+v", fuzz)
	}
	flood := securityNode(t, nodes, "flood").Security
	if flood.FloodPath != "/api/v1/resource" || flood.FloodRate != 50 || flood.FloodDuration != 5 || !flood.FloodThrottle {
		t.Fatalf("unexpected flood spec %+v", flood)
	}
}

func TestSecurityFailOnMustBeALevel(t *testing.T) {
	t.Parallel()
	_, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
probe = security.probe(name="probe", after=[api], fail_on="severe")`)
	if err == nil || !strings.Contains(err.Error(), `fail_on "severe" is not one of low, medium, high, critical`) {
		t.Fatalf("expected the level to be refused, got %v", err)
	}
	for _, level := range []string{"low", "Medium", "HIGH", "critical"} {
		if _, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
probe = security.probe(name="probe", after=[api], fail_on="`+level+`")`); err != nil {
			t.Fatalf("fail_on=%q must be accepted: %v", level, err)
		}
	}
}

func TestSecurityTechniqueMustBeAKnownFamily(t *testing.T) {
	t.Parallel()
	_, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
fuzz = security.fuzz(name="fuzz", after=[api], technique="nosql")`)
	if err == nil || !strings.Contains(err.Error(), `technique "nosql" is not one of sqli, xss, traversal, header-injection`) {
		t.Fatalf("expected the technique to be refused, got %v", err)
	}
	for _, technique := range SecurityTechniques {
		if _, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
fuzz = security.fuzz(name="fuzz", after=[api], technique="`+technique+`")`); err != nil {
			t.Fatalf("technique=%q must be accepted: %v", technique, err)
		}
	}
}

func TestSecurityFloodIsBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ args, want string }{
		{`rate=500.0`, "rate must be above 0 and at most 100"},
		{`rate=0.0`, "rate must be above 0 and at most 100"},
		{`duration=600.0`, "duration must be above 0 and at most 60"},
		{`duration=-1.0`, "duration must be above 0 and at most 60"},
	} {
		_, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
flood = security.flood(name="flood", after=[api], `+tc.args+`)`)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected %q, got %v", tc.args, tc.want, err)
		}
	}
	if _, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
flood = security.flood(name="flood", after=[api], rate=100.0, duration=60.0)`); err != nil {
		t.Fatalf("the caps themselves must be accepted: %v", err)
	}
}

func TestSecurityStepWithoutATargetNeedsAMock(t *testing.T) {
	t.Parallel()
	_, err := resolveSecuritySuite(t, `probe = security.probe(name="probe")`)
	if err == nil || !strings.Contains(err.Error(), `security step "probe" needs target=`) {
		t.Fatalf("a scan of nothing must not load, got %v", err)
	}
	if _, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
probe = security.probe(name="probe", after=[api])`); err != nil {
		t.Fatalf("a suite with a mock scans it by default: %v", err)
	}
	if _, err := resolveSecuritySuite(t, `probe = security.probe(name="probe", target="https://api.example.test")`); err != nil {
		t.Fatalf("an explicit target needs no mock: %v", err)
	}
}

func TestSecurityTargetThatIsNoServiceOfTheSuiteIsRefused(t *testing.T) {
	t.Parallel()
	_, err := resolveSecuritySuite(t, `
api = mock.api(name="api")
probe = security.probe(name="probe", after=[api], target="http://ghost:8000")`)
	if err == nil || !strings.Contains(err.Error(), `security step "probe" targets host "ghost"`) {
		t.Fatalf("a bare host nothing provides must be refused, got %v", err)
	}
}

func TestSecurityTargetThatIsAServiceOfTheSuiteIsAccepted(t *testing.T) {
	t.Parallel()
	if _, err := resolveSecuritySuite(t, `
vuln = service.run(name="vuln", image="python:3.12-alpine", commands=["true"])
probe = security.probe(name="probe", after=[vuln], target="http://vuln:8000")`); err != nil {
		t.Fatalf("a service of the suite is a valid target: %v", err)
	}
}
