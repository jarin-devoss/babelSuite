package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDatabaseDriverSelection(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		config  Config
		want    string
		wantErr string
	}{
		"default is mongo":              {Config{}, DriverMongo, ""},
		"mongo, any case":               {Config{DBDriver: " Mongo "}, DriverMongo, ""},
		"postgres with a url":           {Config{DBDriver: "postgres", PostgresURL: "postgres://x"}, DriverPostgres, ""},
		"postgres without a url":        {Config{DBDriver: "postgres"}, "", "POSTGRES_URL"},
		"an unknown driver is an error": {Config{DBDriver: "mysql"}, "", "not supported"},
	} {
		got, err := tc.config.databaseDriver()
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%s: err = %v, want one naming %q", name, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

// The server runs on PostgreSQL when DB_DRIVER says so, and /healthz reports the
// driver it is really using.
func TestTheServerRunsOnPostgres(t *testing.T) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker is not available")
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD=pw", "-e", "POSTGRES_DB=babelsuite", "postgres:16").CombinedOutput()
	if err != nil {
		t.Fatalf("start postgres: %v\n%s", err, out)
	}
	container := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	portOut, err := exec.Command("docker", "port", container, "5432/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	hostPort := strings.TrimSpace(strings.Split(strings.TrimSpace(string(portOut)), "\n")[0])
	hostPort = hostPort[strings.LastIndex(hostPort, ":")+1:]
	url := fmt.Sprintf("postgres://postgres:pw@127.0.0.1:%s/babelsuite?sslmode=disable", hostPort)

	settings := filepath.Join(t.TempDir(), "configuration.yaml")
	if err := os.WriteFile(settings, []byte(`agents:
  - agentId: local-docker
    name: Local Docker
    type: local
    enabled: true
    default: true
    apisixSidecar:
      image: apache/apisix:3.9.0-debian
      configMountPath: /usr/local/apisix/conf/apisix.yaml
      listenPort: 9080
registries:
  - registryId: zot
    name: Zot
    provider: zot
    registryUrl: localhost:5000
    allowLocalNetwork: true
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var application *App
	deadline := time.Now().Add(60 * time.Second)
	for {
		application, err = New(context.Background(), Config{
			JWTSecret: "driver-test-secret", DBDriver: "postgres", PostgresURL: url,
			PlatformSettingsFile: settings,
			ProfilesFile:         filepath.Join(t.TempDir(), "profiles.yaml"),
			AgentRuntimeFile:     filepath.Join(t.TempDir(), "agents.yaml"),
			AdminEmail:           "admin@example.com", AdminPassword: "Passw0rd!", PasswordAuthEnabled: true,
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not start on postgres: %v", err)
		}
		time.Sleep(time.Second)
	}
	t.Cleanup(func() { _ = application.Close(context.Background()) })

	recorder := httptest.NewRecorder()
	application.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"dbDriver":"postgres"`) {
		t.Fatalf("/healthz = %d %s, want dbDriver postgres", recorder.Code, recorder.Body.String())
	}

	// The admin seeded at startup was written to PostgreSQL: sign in works.
	signIn := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in", strings.NewReader(`{"email":"admin@example.com","password":"Passw0rd!"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-test")
	request.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-test"})
	application.ServeHTTP(signIn, request)
	if signIn.Code != http.StatusOK {
		t.Fatalf("sign-in against postgres: %d %s", signIn.Code, signIn.Body.String())
	}
}
