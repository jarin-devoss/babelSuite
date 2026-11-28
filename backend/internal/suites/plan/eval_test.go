package plan

import (
	"github.com/babelsuite/babelsuite/internal/suites/model"
	"strings"
	"testing"
)

const planTail = `
traffic.plan(users=users, shape=traffic.stages([traffic.stage(duration="5s", users=2, spawn_rate=2)]))
`

func planPaths(t *testing.T, source string, env map[string]string) []string {
	t.Helper()
	spec, err := Parse(&model.LoadSpec{Variant: "traffic.baseline", Target: "http://x"}, source, env)
	if err != nil {
		t.Fatalf("parseLoadPlan: %v", err)
	}
	var paths []string
	for _, user := range spec.Users {
		for _, task := range user.Tasks {
			paths = append(paths, task.Request.Path)
		}
	}
	return paths
}

// A plan file is a Starlark program: a loop builds users, a variable is a path,
// env.get() reads the launch profile.
func TestAPlanIsAStarlarkProgram(t *testing.T) {
	t.Parallel()
	loop := `
users = []
for path in ["/a", "/b"]:
    users.append(traffic.user(name="u" + path, weight=1, wait=traffic.constant(0.1),
        tasks=[traffic.task(name="t", request=traffic.get(path, name="t"))]))
` + planTail
	if got := planPaths(t, loop, nil); strings.Join(got, ",") != "/a,/b" {
		t.Errorf("loop: paths = %v, want /a,/b", got)
	}

	variable := `
PATH = "/health"
users = [traffic.user(name="probe", weight=1, wait=traffic.constant(0.1),
    tasks=[traffic.task(name="t", request=traffic.get(PATH, name="t"))])]
` + planTail
	if got := planPaths(t, variable, nil); strings.Join(got, ",") != "/health" {
		t.Errorf("variable: paths = %v, want /health", got)
	}

	fromEnv := `
users = [traffic.user(name="probe", weight=1, wait=traffic.constant(0.1),
    tasks=[traffic.task(name="t", request=traffic.get(env.get("PROBE_PATH", "/default"), name="t"))])]
` + planTail
	if got := planPaths(t, fromEnv, map[string]string{"PROBE_PATH": "/from-profile"}); strings.Join(got, ",") != "/from-profile" {
		t.Errorf("env: paths = %v, want /from-profile", got)
	}
	if got := planPaths(t, fromEnv, nil); strings.Join(got, ",") != "/default" {
		t.Errorf("env default: paths = %v, want /default", got)
	}
}

// A keyword a plan builder does not read is an error that names it, instead of
// being dropped.
func TestAPlanBuilderRejectsAKeywordItDoesNotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"traffic.plan":      `traffic.plan(users=users, treshold_typo=[1])`,
		"traffic.user":      `users.append(traffic.user(name="u", wieght=2, tasks=[traffic.task(request=traffic.get("/x"))]))`,
		"traffic.get":       `users.append(traffic.user(name="u", tasks=[traffic.task(request=traffic.get("/x", haders={}))]))`,
		"traffic.threshold": `traffic.threshold("http.error_rate", "<=", 0, sampel="x")`,
	}
	for call, body := range cases {
		source := "users = []\n" + body + "\n"
		_, err := Parse(&model.LoadSpec{Variant: "traffic.baseline", Target: "http://x"}, source, nil)
		if err == nil || !strings.Contains(err.Error(), call+": unknown argument") || !strings.Contains(err.Error(), "allowed:") {
			t.Errorf("%s: err = %v, want an unknown-argument error that lists the allowed ones", call, err)
		}
	}
}

func TestAPlanMustDeclareItselfOnce(t *testing.T) {
	t.Parallel()
	base := &model.LoadSpec{Variant: "traffic.baseline", Target: "http://x", PlanPath: "traffic/p.star"}
	if _, err := Parse(base, `x = 1`, nil); err == nil || !strings.Contains(err.Error(), "must declare traffic.plan") {
		t.Errorf("no plan: err = %v", err)
	}
	user := `users = [traffic.user(name="u", tasks=[traffic.task(request=traffic.get("/x"))])]` + "\n"
	if _, err := Parse(base, user+"traffic.plan(users=users)\ntraffic.plan(users=users)\n", nil); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("two plans: err = %v", err)
	}
	if _, err := Parse(base, `load("other.star", "x")`, nil); err == nil {
		t.Errorf("a plan loading another file was accepted")
	}
}
