package suitescmd

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

func newSuitesServer(t *testing.T) *httptest.Server {
	t.Helper()
	suites := []apiclient.SuiteDefinition{
		{ID: "suite-1", Title: "Payment Suite", Repository: "localhost:5000/payment", Status: "available", Version: "1.0.0"},
		{ID: "suite-2", Title: "Identity Broker", Repository: "localhost:5000/identity", Status: "available", Version: "2.1.0"},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/suites":
			_ = json.NewEncoder(w).Encode(map[string]any{"suites": suites})
		case "/api/v1/suites/suite-1":
			_ = json.NewEncoder(w).Encode(suites[0])
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

func TestSuitesRunList(t *testing.T) {
	t.Parallel()
	srv := newSuitesServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Payment Suite") {
		t.Fatalf("expected suite title in output, got: %s", stdout.String())
	}
}

func TestSuitesRunListJSON(t *testing.T) {
	t.Parallel()
	srv := newSuitesServer(t)
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

func TestSuitesRunGetSuccess(t *testing.T) {
	t.Parallel()
	srv := newSuitesServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get", "suite-1"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Payment Suite") {
		t.Fatalf("expected suite title in output, got: %s", stdout.String())
	}
}

func TestSuitesRunGetMissingArg(t *testing.T) {
	t.Parallel()
	srv := newSuitesServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing arg, got %d", code)
	}
}

func TestSuitesRunHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
}

func TestSuitesRunUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"delete"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}
