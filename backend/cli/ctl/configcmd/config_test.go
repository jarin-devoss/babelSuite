package configcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/cli/ctl/internal/support"
	"github.com/babelsuite/babelsuite/pkg/localconfig"
)

func newRT(t *testing.T, stdout, stderr *bytes.Buffer) *support.Runtime {
	t.Helper()
	return &support.Runtime{
		Stdout: stdout,
		Stderr: stderr,
		Store:  localconfig.NewStore(filepath.Join(t.TempDir(), "config.json")),
	}
}

func TestConfigShowEmpty(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(not set)") {
		t.Fatalf("expected '(not set)' for empty config, got: %s", stdout.String())
	}
}

func TestConfigShowWithValues(t *testing.T) {
	t.Parallel()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	cfg := &localconfig.Config{Server: "http://babelsuite.example.com", Email: "me@example.com", Workspace: "myws"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)

	var stdout, stderr bytes.Buffer
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"show"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "babelsuite.example.com") {
		t.Fatalf("expected server URL in output, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "me@example.com") {
		t.Fatalf("expected email in output, got: %s", stdout.String())
	}
}

func TestConfigShowJSON(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{Output: "json"}, []string{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if _, ok := out["server"]; !ok {
		t.Fatal("expected 'server' key in JSON output")
	}
}

func TestConfigSetServer(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"set", "server", "http://myserver:8090"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "server updated") {
		t.Fatalf("expected confirmation, got: %s", stdout.String())
	}

	cfg, _ := rt.Store.Load()
	if !strings.Contains(cfg.Server, "myserver") {
		t.Fatalf("expected server to be saved, got: %s", cfg.Server)
	}
}

func TestConfigSetToken(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"set", "token", "my-secret-token"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}

	cfg, _ := rt.Store.Load()
	if cfg.Token != "my-secret-token" {
		t.Fatalf("expected token to be saved, got: %s", cfg.Token)
	}
}

func TestConfigSetUnknownKey(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"set", "username", "alice"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown key, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown config key") {
		t.Fatalf("expected error about unknown key, got: %s", stderr.String())
	}
}

func TestConfigSetMissingArgs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"set", "server"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing value, got %d", code)
	}
}

func TestConfigGetServer(t *testing.T) {
	t.Parallel()
	cfgFile := filepath.Join(t.TempDir(), "config.json")
	cfg := &localconfig.Config{Server: "http://myserver:8090"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)

	var stdout, stderr bytes.Buffer
	rt := &support.Runtime{Stdout: &stdout, Stderr: &stderr, Store: localconfig.NewStore(cfgFile)}
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get", "server"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "myserver") {
		t.Fatalf("expected server in output, got: %s", stdout.String())
	}
}

func TestConfigGetUnknownKey(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get", "foo"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown key, got %d", code)
	}
}

func TestConfigGetMissingArg(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"get"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing key, got %d", code)
	}
}

func TestConfigHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"help"})
	if code != 0 {
		t.Fatalf("expected exit 0 for help, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage in help output, got: %s", stdout.String())
	}
}

func TestConfigUnknownSubcommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRT(t, &stdout, &stderr)
	code := Run(context.Background(), rt, support.GlobalOptions{}, []string{"reset"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown subcommand, got %d", code)
	}
}
