package authcmd

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
	"github.com/babelsuite/babelsuite/pkg/localconfig"
)

func newRuntime(t *testing.T, stdout, stderr *bytes.Buffer) *support.Runtime {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	return &support.Runtime{
		Stdout: stdout,
		Stderr: stderr,
		Store:  localconfig.NewStore(cfgFile),
	}
}

func TestRunLoginMissingFlags(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	code := RunLogin(context.Background(), rt, support.GlobalOptions{}, []string{})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing flags, got %d", code)
	}
}

func TestRunLoginSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/sign-in" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "test-token",
				"user": map[string]any{
					"email":    "test@example.com",
					"fullName": "Test User",
				},
				"workspace": map[string]any{"name": "default"},
				"expiresAt": time.Now().Add(time.Hour),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	opts := support.GlobalOptions{Server: srv.URL}
	code := RunLogin(context.Background(), rt, opts, []string{"--email", "test@example.com", "--password", "secret"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Signed in as") {
		t.Fatalf("expected signed-in confirmation, got: %s", stdout.String())
	}
}

func TestRunLoginServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	opts := support.GlobalOptions{Server: srv.URL}
	code := RunLogin(context.Background(), rt, opts, []string{"--email", "bad@example.com", "--password", "wrong"})
	if code != 1 {
		t.Fatalf("expected exit 1 on server error, got %d", code)
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("expected error in stderr, got: %s", stderr.String())
	}
}

func TestRunLoginJSONOutput(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     "tok",
			"user":      map[string]any{"email": "u@x.com"},
			"workspace": map[string]any{"name": "ws"},
			"expiresAt": time.Now().Add(time.Hour),
		})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	opts := support.GlobalOptions{Server: srv.URL, Output: "json"}
	code := RunLogin(context.Background(), rt, opts, []string{"--email", "u@x.com", "--password", "p"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}
	if out["server"] == nil {
		t.Fatal("expected 'server' key in JSON output")
	}
}

func TestRunLogoutNoSession(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	code := RunLogout(rt, support.GlobalOptions{})
	if code != 0 {
		t.Fatalf("expected exit 0 on logout with no session, got %d; stderr: %s", code, stderr.String())
	}
}

func TestRunLogoutWritesSignedOut(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	rt := newRuntime(t, &stdout, &stderr)
	code := RunLogout(rt, support.GlobalOptions{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Signed out") {
		t.Fatalf("expected 'Signed out' message, got: %s", stdout.String())
	}
}

func TestRunWhoAmINoSession(t *testing.T) {
	t.Parallel()
	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	var stdout, stderr bytes.Buffer
	rt := &support.Runtime{
		Stdout: &stdout,
		Stderr: &stderr,
		Store:  localconfig.NewStore(cfgFile),
	}
	code := RunWhoAmI(context.Background(), rt, support.GlobalOptions{})
	if code != 1 {
		t.Fatalf("expected exit 1 with no session, got %d", code)
	}
}

func TestRunWhoAmISuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/me" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email":    "me@example.com",
				"fullName": "Me User",
				"isAdmin":  false,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgFile := filepath.Join(t.TempDir(), "babelctl.json")
	cfg := &localconfig.Config{Server: srv.URL, Token: "tok"}
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(cfgFile, data, 0600)

	var stdout, stderr bytes.Buffer
	rt := &support.Runtime{
		Stdout: &stdout,
		Stderr: &stderr,
		Store:  localconfig.NewStore(cfgFile),
	}
	code := RunWhoAmI(context.Background(), rt, support.GlobalOptions{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "me@example.com") {
		t.Fatalf("expected email in output, got: %s", stdout.String())
	}
}
