package profilescmd

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

func newProfilesServer(t *testing.T) *httptest.Server {
	t.Helper()
	suites := []apiclient.SuiteDefinition{
		{ID: "suite-1", Title: "Payment Suite", Repository: "localhost:5000/payment"},
	}
	profiles := apiclient.SuiteProfilesResponse{
		SuiteID:                "suite-1",
		SuiteTitle:             "Payment Suite",
		Repository:             "localhost:5000/payment",
		DefaultProfileFileName: "local.yaml",
		Profiles: []apiclient.ProfileRecord{
			{ID: "p-1", Name: "local", FileName: "local.yaml", Default: true, Launchable: true},
			{ID: "p-2", Name: "ci", FileName: "ci.yaml"},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/suites":
			_ = json.NewEncoder(w).Encode(map[string]any{"suites": suites})
		case "/api/v1/profiles/suites/suite-1":
			_ = json.NewEncoder(w).Encode(profiles)
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

func TestProfilesRunHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in output, got: %s", stdout.String())
	}
}

func TestProfilesRunUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"publish"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}

func TestProfilesRunListMissingSuiteArg(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"list"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing suite arg, got %d", code)
	}
}

func TestProfilesRunListSuccess(t *testing.T) {
	t.Parallel()
	srv := newProfilesServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"list", "suite-1"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "local") {
		t.Fatalf("expected profile name in output, got: %s", stdout.String())
	}
}

func TestProfilesRunListJSON(t *testing.T) {
	t.Parallel()
	srv := newProfilesServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{Output: "json"}, []string{"list", "suite-1"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
}
