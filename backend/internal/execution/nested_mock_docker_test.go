package execution

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/mocking"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// mapSuiteReader serves every suite of a map to the mock engine.
type mapSuiteReader map[string]suites.Definition

func (r mapSuiteReader) Get(id string) (*suites.Definition, error) {
	suite, ok := r[id]
	if !ok {
		return nil, suites.ErrNotFound
	}
	clone := suite
	return &clone, nil
}

// A suite that imports another reaches the imported suite's mock: by the host it
// answers on and by the name of its mock node, through the one gateway of the
// run. Its mock nodes were in the run's topology and answered nowhere.
func TestAnImportedSuitesMockAnswersToTheSuiteThatImportsIt(t *testing.T) {
	requireDocker(t)

	child := suites.Definition{
		ID: "nested-child", Title: "Nested Child", Repository: "localhost:5000/core/nested-child", Version: "workspace",
		SuiteStar: `api = mock.api(name="child-api")`,
		Profiles:  []suites.ProfileOption{{FileName: "local.yaml", Default: true}},
		APISurfaces: []suites.APISurface{{
			ID: "api", Protocol: "rest", MockHost: "http://nested-child.mock.internal",
			Operations: []suites.APIOperation{{
				ID: "ping", Method: "GET", Name: "/ping", MockMetadata: suites.MockOperationMetadata{Adapter: "rest"},
				Exchanges: []suites.ExchangeExample{{Name: "ok", ResponseStatus: "200", ResponseMediaType: "application/json", ResponseBody: `{"from":"nested-child"}`}},
			}},
		}},
	}
	parent := suites.Definition{
		ID: "nested-parent", Title: "Nested Parent", Repository: "localhost:5000/core/nested-parent", Version: "workspace",
		SuiteStar: strings.Join([]string{
			`load("@babelsuite/runtime", "suite", "test")`,
			`child = suite.run(ref="child-module")`,
			`probe = test.run(name="probe", image="busybox", after=[child], commands=[`,
			`  "echo BYHOST=$(wget -qO- --header 'Host: nested-child.mock.internal' http://babelsuite-gateway:9080/ping)",`,
			`  "echo BYNAME=$(wget -qO- http://child-api:9080/ping)",`,
			`])`,
		}, "\n"),
		Profiles: []suites.ProfileOption{{FileName: "local.yaml", Default: true}},
		SourceFiles: []suites.SourceFile{{Path: "dependencies.yaml", Content: strings.TrimSpace(`
dependencies:
  child-module:
    ref: localhost:5000/core/nested-child
    version: workspace
    profile: local.yaml
`)}},
	}
	all := map[string]suites.Definition{child.ID: child, parent.ID: parent}

	const token = "nested-test-token"
	runner.ConfigureSidecarSecrets(token)
	t.Cleanup(func() { runner.ConfigureSidecarSecrets("") })
	engine := mocking.NewService(mapSuiteReader(all))
	mux := http.NewServeMux()
	mocking.NewHandler(engine, token).Register(mux)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))

	sidecar := runner.SidecarContainerName(parent.ID, "local.yaml")
	_ = exec.Command("docker", "rm", "-f", sidecar).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })

	service := NewServiceWithPlatform(staticSuiteSource{items: all},
		stubPlatformSource{settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{{
				AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true,
				APISIXSidecar: platform.APISIXSidecarConfig{Image: "apache/apisix:3.9.0-debian", ConfigMountPath: "/usr/local/apisix/conf/apisix.yaml", ListenPort: 9080},
			}},
		}})
	service.ConfigureMockResetter(engine)
	defer service.Close()

	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: parent.ID, Profile: "local.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := waitForExecutionTerminal(t, service, execution.ID)

	var logs strings.Builder
	if lines, err := service.SnapshotLogs(execution.ID, ""); err == nil {
		for _, entry := range lines {
			logs.WriteString(entry.Text + "\n")
		}
	}
	if record, err := service.GetExecution(execution.ID, ""); err == nil {
		for _, event := range record.Events {
			logs.WriteString("[" + event.Level + "] " + event.Text + "\n")
		}
	}
	if snapshot.Status != "Healthy" {
		t.Fatalf("the run ended %s:\n%s", snapshot.Status, logs.String())
	}
	for _, want := range []string{`BYHOST={"from":"nested-child"}`, `BYNAME={"from":"nested-child"}`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the step's output lacks %q:\n%s", want, logs.String())
		}
	}
}
