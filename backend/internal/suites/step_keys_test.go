package suites

import (
	"strings"
	"testing"
)

// A keyword a call does not read is an error that lists the ones it does, on
// every kind of step. mock.api(definition=...) did nothing for as long as it was
// in the examples, and a misspelled option simply was not there.
func TestAKeywordACallDoesNotReadIsAnError(t *testing.T) {
	t.Parallel()
	for call, star := range map[string]string{
		"service.run":      `service.run(image="alpine:3.19", ports={"80": 80})`,
		"task.run":         `task.run(image="alpine:3.19", commands=["true"], retries=3)`,
		"test.run":         `test.run(image="alpine:3.19", commands=["true"], continue_on_failur=True)`,
		"mock.api":         `mock.api(name="m", definition="mock/m")`,
		"suite.run":        `suite.run(ref="x", env={"A": "b"}, version="1")`,
		"traffic.baseline": `traffic.baseline(target="http://api:8080", plan="p.star", users=10)`,
		"log.info":         `log.info("hello", level="debug")`,
	} {
		_, err := evalStarlarkTopology("load(\"@babelsuite/runtime\", \"service\", \"task\", \"test\", \"mock\", \"suite\", \"traffic\", \"log\")\n"+star, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown argument") || !strings.Contains(err.Error(), "allowed:") {
			t.Errorf("%s: err = %v, want an unknown-argument error that lists the allowed ones", call, err)
		}
	}
}

func TestEveryKeywordACallReadsStillWorks(t *testing.T) {
	t.Parallel()
	star := `load("@babelsuite/runtime", "service", "task", "test", "mock", "traffic", "log")
db   = service.run(name="db", image="postgres:16", env={"A": "b"}, commands=["true"], exports=[], expect_exit=0, expect_logs=["x"], fail_on_logs=["y"], continue_on_failure=True, message="m")
m    = mock.api(name="m", harden=True, after=[db], on_failure=[db])
t1   = task.run(name="t1", image="alpine:3.19", file="a.sh", reset_mocks=[m], after=[m])
t2   = test.run(name="t2", image="alpine:3.19", commands=["true"], reset_mocks=[m], after=[t1])
tr   = traffic.baseline(name="tr", target="http://db:5432", plan="p.star", rps=10, arrival_rate=2, min_users=1, after=[t2])
l    = log.info("done", after=[tr])
`
	if _, err := evalStarlarkTopology(star, nil); err != nil {
		t.Fatalf("keywords the calls read were rejected: %v", err)
	}
}
