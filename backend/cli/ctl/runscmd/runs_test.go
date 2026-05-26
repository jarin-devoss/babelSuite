package runscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/cli/ctl/internal/support"
	"github.com/babelsuite/babelsuite/pkg/apiclient"
	"github.com/babelsuite/babelsuite/pkg/localconfig"
)

func newRunsServer(t *testing.T) *httptest.Server {
	t.Helper()
	executions := []apiclient.ExecutionSummary{
		{ID: "exec-1", SuiteTitle: "Payment Suite", Profile: "local", Status: "passed", StartedAt: time.Now().Add(-5 * time.Minute)},
		{ID: "exec-2", SuiteTitle: "Payment Suite", Profile: "ci", Status: "running", StartedAt: time.Now()},
	}
	detail := apiclient.ExecutionRecord{
		ID:     "exec-1",
		Status: "passed",
		Suite:  apiclient.ExecutionSuite{ID: "suite-1", Title: "Payment Suite"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/executions" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"executions": executions})
		case r.URL.Path == "/api/v1/executions/exec-1" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(detail)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runtimeWithToken(t *testing.T, stdout, stderr *bytes.Buffer, serverURL string) *support.Runtime {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	cfg := &localconfig.Config{Server: serverURL, Token: "tok"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)
	return &support.Runtime{Stdout: stdout, Stderr: stderr, Store: localconfig.NewStore(cfgFile)}
}

func TestRunsRunList(t *testing.T) {
	t.Parallel()
	srv := newRunsServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "exec-1") {
		t.Fatalf("expected execution id in output, got: %s", stdout.String())
	}
}

func TestRunsRunListJSON(t *testing.T) {
	t.Parallel()
	srv := newRunsServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{Output: "json"}, []string{"list"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out []any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON array: %v", err)
	}
}

func TestRunsRunGetSuccess(t *testing.T) {
	t.Parallel()
	srv := newRunsServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get", "exec-1"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "exec-1") {
		t.Fatalf("expected execution id in output, got: %s", stdout.String())
	}
}

func TestRunsRunHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
}

func TestRunsRunUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"delete"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestRunsRunGetMissingArg(t *testing.T) {
	t.Parallel()
	srv := newRunsServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing execution id, got %d", code)
	}
}
