package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/testutil/dockerdb"
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
	container, port := dockerdb.Start(t, "postgres:16", 5432, "POSTGRES_PASSWORD=pw", "POSTGRES_DB=babelsuite")
	dockerdb.WaitForLog(t, container, "database system is ready to accept connections", 2, 2*time.Minute)
	url := fmt.Sprintf("postgres://postgres:pw@127.0.0.1:%d/babelsuite?sslmode=disable", port)

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

	var (
		application *App
		err         error
	)
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
