package execution

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

type mockSuiteReader struct{ suite suites.Definition }

func (r mockSuiteReader) Get(id string) (*suites.Definition, error) {
	if id != r.suite.ID {
		return nil, suites.ErrNotFound
	}
	clone := r.suite
	return &clone, nil
}

// Two runs of one suite overlap and both call a stateful mock. Each must see
// its own state: the second request of a run answers previousAttempts "1" however
// many requests the other run made in between (FIX-PLAN #25; before, the state
// was one map per suite, so a run saw the other's attempts and a reset by one
// wiped the other's).
func TestOverlappingRunsKeepTheirOwnMockState(t *testing.T) {
	requireDocker(t)

	base, err := suites.NewWorkspaceService().Get("storefront-browser-lab")
	if err != nil {
		t.Fatalf("load the storefront suite: %v", err)
	}
	definition := *base
	definition.SuiteStar = strings.Join([]string{
		`load("@babelsuite/runtime", "mock", "test")`,
		`m = mock.api(name="storefront-api")`,
		`reset = test.run(name="reset", image="busybox", commands=["echo reset"], reset_mocks=[m], after=[m])`,
		`worker = test.run(name="worker", image="busybox", after=[reset], commands=[`,
		`  "post() { wget -q -S -O - --header 'Content-Type: application/json' --header 'x-browser-suite: playwright' --post-data '{\"email\":\"shopper@demo.test\",\"quantity\":1,\"sku\":\"sku_1001\"}' 'http://storefront-browser-lab.mock.internal:9080/orders?scenario=happy-path' 2>&1; }",`,
		`  "echo \"VERIFY $BABELSUITE_EXECUTION_ID first=$(post)\"",`,
		`  "sleep 8",`,
		`  "echo \"VERIFY $BABELSUITE_EXECUTION_ID second=$(post)\"",`,
		`])`,
	}, "\n")
	definition.Profiles = []suites.ProfileOption{{FileName: "local.yaml", Default: true}}

	// The sidecar proxies mocks to the control plane's mock engine, so a real one
	// answers on a port the sidecar can reach.
	engine := mocking.NewService(mockSuiteReader{suite: definition})
	mux := http.NewServeMux()
	mocking.NewHandler(engine, "").Register(mux)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))

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

	first, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: definition.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create the first execution: %v", err)
	}
	// Both start at once: the two runs boot together and both ask for the suite's
	// sidecar, which used to fail one of them with a name conflict.
	second, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: definition.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create the second execution: %v", err)
	}
	for _, run := range []string{first.ID, second.ID} {
		if snapshot := waitForExecutionTerminal(t, service, run); snapshot.Status != "Healthy" {
			t.Fatalf("%s ended %s, want Healthy", run, snapshot.Status)
		}
	}

	attempts := regexp.MustCompile(`"previousAttempts":\s*"(\d+)"`)
	for _, run := range []string{first.ID, second.ID} {
		lines, err := service.SnapshotLogs(run, "")
		if err != nil {
			t.Fatalf("logs of %s: %v", run, err)
		}
		var text strings.Builder
		for _, entry := range lines {
			text.WriteString(entry.Text + "\n")
		}
		all := text.String()

		firstAt, secondAt := strings.Index(all, run+" first="), strings.Index(all, run+" second=")
		if firstAt < 0 || secondAt < 0 {
			t.Fatalf("%s: the step did not print both answers:\n%s", run, all)
		}
		firstAnswer := attempts.FindStringSubmatch(all[firstAt:secondAt])
		secondAnswer := attempts.FindStringSubmatch(all[secondAt:])
		if firstAnswer == nil || secondAnswer == nil {
			t.Fatalf("%s: no previousAttempts in the answers:\n%s", run, all)
		}
		if firstAnswer[1] != "0" || secondAnswer[1] != "1" {
			t.Errorf("%s: previousAttempts were %s then %s, want 0 then 1 (the other run's requests leaked in)\n%s", run, firstAnswer[1], secondAnswer[1], all)
		}
		if !strings.Contains(strings.ToLower(all[secondAt:]), "x-babelsuite-state-scope: execution") {
			t.Errorf("%s: the answer must say it used this run's state:\n%s", run, all[secondAt:])
		}
	}
}
