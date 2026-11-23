package execution

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

const vulnServer = `from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def handle_any(self):
        print("VULN-HIT", self.command, self.path, flush=True)
        self.send_response(200)
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"ok")

    do_GET = do_POST = do_PUT = do_DELETE = do_TRACE = handle_any

    def log_message(self, *args):
        pass


ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
`

// A security step on a target that is a service of the suite must reach it:
// before, the sidecar was not on the run's network, the name did not resolve and
// the service saw no request (FIX-PLAN #22). The step's report must also be in
// the execution's workspace, so a later step can read it.
func TestSecurityStepReachesAServiceOfTheSuiteAndLeavesItsReportInTheWorkspace(t *testing.T) {
	requireDocker(t)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", runner.SidecarContainerName("sec-run", "local.yaml")).Run()
	})
	source := staticSuiteSource{items: map[string]suites.Definition{
		"sec-run": {
			ID:    "sec-run",
			Title: "Sec Run",
			SuiteStar: strings.Join([]string{
				`load("@babelsuite/runtime", "service", "task", "test", "security")`,
				`vuln = service.run(name="vuln", image="python:3.12-alpine", file="vuln.py")`,
				`ready = test.run(name="ready", image="python:3.12-alpine", commands=["until python -c \"import urllib.request; urllib.request.urlopen('http://vuln:8000/ready')\"; do sleep 1; done"], after=[vuln])`,
				`probe = security.probe(name="probe", target="http://vuln:8000", continue_on_failure=True, after=[ready])`,
				`headers = security.headers(name="headers", target="http://vuln:8000", continue_on_failure=True, exports=[{"path": "/findings/headers.json", "on": "always"}], after=[ready])`,
				`gw = test.run(name="gw", image="python:3.12-alpine", commands=["echo GATEWAY-URL=$BABELSUITE_GATEWAY_URL", "wget -S -O /dev/null \"$BABELSUITE_GATEWAY_URL/\" 2>&1 | grep -m1 'HTTP/' | sed 's/^ */GATEWAY-STATUS /'"], after=[ready])`,
				`read = task.run(name="read", image="python:3.12-alpine", commands=["cat findings/headers.json"], after=[headers, probe])`,
			}, "\n"),
			SourceFiles: []suites.SourceFile{{Path: "tasks/vuln.py", Content: vulnServer}},
			Profiles:    []suites.ProfileOption{{FileName: "local.yaml", Default: true}},
		},
	}}
	// A security step runs in the suite's sidecar, which the execution starts from
	// the local agent's settings.
	service := NewServiceWithPlatform(source, stubPlatformSource{settings: &platform.PlatformSettings{
		Agents: []platform.ExecutionAgent{{
			AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true,
			APISIXSidecar: platform.APISIXSidecarConfig{
				Image:           "apache/apisix:3.9.0-debian",
				ConfigMountPath: "/usr/local/apisix/conf/apisix.yaml",
				ListenPort:      9080,
			},
		}},
	}})
	defer service.Close()

	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: "sec-run", Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	waitForExecutionTerminal(t, service, execution.ID)

	lines, err := service.SnapshotLogs(execution.ID, "")
	if err != nil {
		t.Fatalf("snapshot logs: %v", err)
	}
	var hits int
	var text strings.Builder
	for _, entry := range lines {
		text.WriteString(entry.Text)
		text.WriteString("\n")
		if strings.Contains(entry.Text, "VULN-HIT") && !strings.Contains(entry.Text, "/ready") {
			hits++
		}
	}
	all := text.String()

	// probe sends 15 requests and headers 3, so well over 15 must have arrived.
	if hits < 15 {
		name := runner.SidecarContainerName("sec-run", "local.yaml")
		state, _ := exec.Command("docker", "inspect", "--format", "{{.State.Status}} exit={{.State.ExitCode}} ports={{json .NetworkSettings.Ports}}", name).CombinedOutput()
		sidecarLog, _ := exec.Command("docker", "logs", "--tail", "60", name).CombinedOutput()
		t.Fatalf("the service saw %d scanner requests, want at least 15; the sidecar did not reach it.\nsidecar: %s\n%s\n%s", hits, state, sidecarLog, all)
	}
	if !strings.Contains(all, "Finding [admin-panel]") {
		t.Fatalf("the probe must report what it found on the service:\n%s", all)
	}
	if !strings.Contains(all, "GATEWAY-URL=http://babelsuite-gateway:9080") {
		t.Fatalf("a step must be given the gateway's address on the run's network, not the host's:\n%s", all)
	}
	if !strings.Contains(all, "GATEWAY-STATUS HTTP/") {
		t.Fatalf("a step must be able to reach the gateway and get an HTTP answer:\n%s", all)
	}
	if !strings.Contains(all, `"mode": "headers"`) || !strings.Contains(all, `"target": "http://vuln:8000"`) {
		t.Fatalf("the report must be in the workspace for the next step to read:\n%s", all)
	}
}
