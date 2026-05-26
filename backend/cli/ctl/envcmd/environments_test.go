package envcmd

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

	"github.com/babelsuite/babelsuite/cli/ctl/internal/support"
	"github.com/babelsuite/babelsuite/pkg/apiclient"
	"github.com/babelsuite/babelsuite/pkg/localconfig"
)

func newEnvServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/sandboxes" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(apiclient.SandboxesResponse{
				DockerAvailable: true,
				Sandboxes: []apiclient.SandboxRecord{
					{SandboxID: "sb-1", Status: "running", Suite: "payment-suite", Profile: "local"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/reap") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(apiclient.ReapResult{Scope: "sandbox", Target: "sb-1", RemovedContainers: 1})
		case r.URL.Path == "/api/v1/sandboxes/reap-all" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(apiclient.ReapResult{Scope: "all", RemovedContainers: 2})
		default:
			http.NotFound(w, r)
		}
	}))
}

func runtimeWith(t *testing.T, stdout, stderr *bytes.Buffer, serverURL string) *support.Runtime {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	cfg := &localconfig.Config{Server: serverURL, Token: "tok"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)
	return &support.Runtime{Stdout: stdout, Stderr: stderr, Store: localconfig.NewStore(cfgFile)}
}

func TestEnvRunDefaultsList(t *testing.T) {
	t.Parallel()
	srv := newEnvServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWith(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "sb-1") {
		t.Fatalf("expected sandbox id in output, got: %s", stdout.String())
	}
}

func TestEnvRunListJSON(t *testing.T) {
	t.Parallel()
	srv := newEnvServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWith(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{Output: "json"}, []string{"list"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON: %v; got: %s", err, stdout.String())
	}
}

func TestEnvRunHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in help, got: %s", stdout.String())
	}
}

func TestEnvRunUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"destroy-all"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestEnvRunReap(t *testing.T) {
	t.Parallel()
	srv := newEnvServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWith(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"reap", "sb-1"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
}

func TestEnvRunReapAll(t *testing.T) {
	t.Parallel()
	srv := newEnvServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWith(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"reap-all"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
}
