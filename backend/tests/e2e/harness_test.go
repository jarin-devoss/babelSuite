//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/app"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	testAdminEmail    = "admin@e2e.local"
	testAdminPassword = "e2e-test-password"
)

// harness holds a running in-process server and a pre-authenticated admin client.
type harness struct {
	url   string
	admin *client
}

// newHarness starts a fresh App with its own isolated MongoDB database and returns
// a harness with a signed-in admin client. All resources are cleaned up via t.Cleanup.
func newHarness(t *testing.T) *harness {
	t.Helper()
	r := require.New(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "configuration.yaml"), `agents:
  - agentId: local-docker
    name: Local Docker
    type: local
    enabled: true
    default: true
    status: Ready
    routingTags: [default, local]
    dockerSocket: /var/run/docker.sock
registries:
  - registryId: local-zot
    name: Local Zot
    provider: Zot
    registryUrl: http://localhost:5000
    repositoryScope: "*"
`)
	writeFile(t, filepath.Join(dir, "babelsuite-profiles.yaml"), fmt.Sprintf(`suites:
  e2e-fixture-suite:
    profiles:
      - id: ci
        name: CI
        fileName: ci.yaml
        scope: suite
        launchable: true
        default: true
        yaml: |
          env:
            ADMIN_EMAIL: %s
`, testAdminEmail))

	mongoURI := envOrDefault("MONGO_URI", "mongodb://localhost:27017")
	mongoDBName := "babelsuite_e2e_" + randHex()

	a, err := app.New(context.Background(), app.Config{
		JWTSecret:     "e2e-jwt-secret-" + randHex(),
		AdminEmail:    testAdminEmail,
		AdminPassword: testAdminPassword,

		MongoURI: mongoURI,
		MongoDB:  mongoDBName,

		PlatformSettingsFile: filepath.Join(dir, "configuration.yaml"),
		ProfilesFile:         filepath.Join(dir, "babelsuite-profiles.yaml"),

		PasswordAuthEnabled: true,
		SignUpEnabled:       true,
	})
	r.NoError(err)

	srv := httptest.NewServer(a)
	t.Cleanup(func() {
		srv.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(shutdownCtx)

		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if mc, err := mongo.Connect(options.Client().ApplyURI(mongoURI)); err == nil {
			_ = mc.Database(mongoDBName).Drop(dropCtx)
			_ = mc.Disconnect(dropCtx)
		}
	})

	adminCli := newClient(srv.URL)
	var tokenBody struct {
		Token string `json:"token"`
	}
	r.NoError(adminCli.postJSON(t, "/api/v1/auth/sign-in", map[string]string{
		"email":    testAdminEmail,
		"password": testAdminPassword,
	}, &tokenBody))
	r.NotEmpty(tokenBody.Token, "sign-in returned empty token")
	adminCli.token = tokenBody.Token

	return &harness{
		url:   srv.URL,
		admin: adminCli,
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func randHex() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func uniqueID(prefix string) string {
	return prefix + "-" + randHex()
}

func minimalSuiteStar() string {
	return `
probe = task.run(name = "probe", image = "alpine:3.19")
`
}

// client is a thin HTTP client that sends JSON and holds an auth token.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(base string) *client {
	return &client{base: base, http: &http.Client{}}
}

func (c *client) withToken(token string) *client {
	clone := *c
	clone.token = token
	return &clone
}

func (c *client) noAuth() *client {
	clone := *c
	clone.token = ""
	return &clone
}

func (c *client) do(method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

func (c *client) getJSON(t *testing.T, path string, out any) {
	t.Helper()
	resp, err := c.do(http.MethodGet, path, nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireOK(t, resp)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

func (c *client) postJSON(t *testing.T, path string, body, out any) error {
	resp, err := c.do(http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return &httpError{code: resp.StatusCode, body: string(b)}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) mustPostJSON(t *testing.T, path string, body, out any) {
	t.Helper()
	require.NoError(t, c.postJSON(t, path, body, out))
}

func (c *client) putJSON(t *testing.T, path string, body, out any) {
	t.Helper()
	resp, err := c.do(http.MethodPut, path, body)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireOK(t, resp)
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
}

func (c *client) getRaw(path string) (*http.Response, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *client) postRaw(path string, body any) (*http.Response, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *client) deleteRaw(path string) (*http.Response, error) {
	return c.do(http.MethodDelete, path, nil)
}

func requireOK(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 2xx, got HTTP %d: %s", resp.StatusCode, body)
	}
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected HTTP %d, got %d: %s", want, resp.StatusCode, body)
	}
}

type httpError struct {
	code int
	body string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.code, e.body)
}

func httpStatusCode(err error) int {
	if e, ok := err.(*httpError); ok {
		return e.code
	}
	return 0
}
