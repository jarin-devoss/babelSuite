package catalogcmd

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

func catalogPackages() []apiclient.CatalogPackage {
	return []apiclient.CatalogPackage{
		{ID: "pkg-kafka", Kind: "stdlib", Title: "Kafka", Version: "1.0.0", Provider: "BabelSuite", Repository: "localhost:5000/kafka", Starred: true},
		{ID: "pkg-payment", Kind: "suite", Title: "Payment Suite", Version: "2.0.0", Provider: "BabelSuite", Repository: "localhost:5000/payment"},
	}
}

func newCatalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/catalog/packages":
			_ = json.NewEncoder(w).Encode(map[string]any{"packages": catalogPackages()})
		case "/api/v1/catalog/packages/pkg-kafka":
			_ = json.NewEncoder(w).Encode(catalogPackages()[0])
		default:
			http.NotFound(w, r)
		}
	}))
}

func runtimeWithToken(t *testing.T, stdout, stderr *bytes.Buffer, serverURL string) *support.Runtime {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	cfg := &localconfig.Config{Server: serverURL, Token: "test-token"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)
	return &support.Runtime{Stdout: stdout, Stderr: stderr, Store: localconfig.NewStore(cfgFile)}
}

func TestCatalogRunDefaultsList(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Kafka") {
		t.Fatalf("expected 'Kafka' in table output, got: %s", stdout.String())
	}
}

func TestCatalogRunListKindFilter(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"list", "--kind", "suite"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.Contains(stdout.String(), "Kafka") {
		t.Fatal("expected Kafka to be filtered out by kind=suite")
	}
	if !strings.Contains(stdout.String(), "Payment") {
		t.Fatalf("expected Payment Suite in output, got: %s", stdout.String())
	}
}

func TestCatalogRunListStarredFilter(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"list", "--starred"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Kafka") {
		t.Fatalf("expected starred Kafka in output, got: %s", stdout.String())
	}
}

func TestCatalogRunListJSON(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{Output: "json"}, []string{"list"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}
}

func TestCatalogRunInspect(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"inspect", "pkg-kafka"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Kafka") {
		t.Fatalf("expected package title in output, got: %s", stdout.String())
	}
}

func TestCatalogRunInspectMissingArg(t *testing.T) {
	t.Parallel()
	srv := newCatalogServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := runtimeWithToken(t, &stdout, &stderr, srv.URL)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"inspect"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing package arg, got %d", code)
	}
}

func TestCatalogRunHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
}

func TestCatalogRunUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"frobnicate"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", code)
	}
}
