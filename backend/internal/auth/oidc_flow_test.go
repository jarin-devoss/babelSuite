package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIdentityProvider is an in-process OpenID provider: discovery, a signing
// key, and a token endpoint that checks the PKCE verifier against the challenge
// the login sent, then answers with a signed ID token.
type fakeIdentityProvider struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	groups   []string

	mu         sync.Mutex
	challenges map[string]string // code -> code_challenge
}

func newFakeIdentityProvider(t *testing.T, clientID string, groups []string) *fakeIdentityProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdentityProvider{key: key, clientID: clientID, groups: groups, challenges: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := idp.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		challenge := idp.challenges[r.PostForm.Get("code")]
		idp.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if challenge == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		claims := jwt.MapClaims{
			"iss": idp.server.URL, "aud": idp.clientID, "sub": "user-1",
			"email": "sso.user@example.com", "name": "Sso User", "groups": idp.groups,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = "test-key"
		signed, err := token.SignedString(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": signed})
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// remember is what a real provider does at /authorize: keep the challenge the
// login sent, and answer with a code.
func (p *fakeIdentityProvider) remember(challenge string) string {
	code := "code-" + challenge[:8]
	p.mu.Lock()
	p.challenges[code] = challenge
	p.mu.Unlock()
	return code
}

func oidcTestHandler(t *testing.T, idp *fakeIdentityProvider) http.Handler {
	t.Helper()
	handler := NewHandler(newStubStore(), NewJWT("test-secret"), Config{
		FrontendURL:         "http://localhost:5173",
		PasswordAuthEnabled: true,
		SignUpEnabled:       true,
		OIDC: OIDCConfig{
			Enabled:             true,
			ProviderName:        "Company SSO",
			IssuerURL:           idp.server.URL,
			ClientID:            idp.clientID,
			ClientSecret:        "secret",
			RedirectURL:         "http://localhost:8090/api/v1/auth/oidc/callback",
			FrontendCallbackURL: "http://localhost:5173/auth/callback",
			PKCEEnabled:         true,
			StateSecret:         []byte("state-secret"),
			AdminGroups:         []string{"babel-admins"},
		},
	})
	mux := http.NewServeMux()
	handler.Register(mux)
	return mux
}

// With the OIDC variables read into the config, single sign-on works from end to
// end: the provider is listed, login redirects with PKCE, the callback creates the
// user, and a member of an admin group is an admin.
func TestSingleSignOnWorksEndToEnd(t *testing.T) {
	t.Parallel()
	idp := newFakeIdentityProvider(t, "babel-client", []string{"babel-admins"})
	mux := oidcTestHandler(t, idp)

	providers := httptest.NewRecorder()
	mux.ServeHTTP(providers, httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/sso/providers", nil))
	if !strings.Contains(providers.Body.String(), "Company SSO") || !strings.Contains(providers.Body.String(), `"enabled":true`) {
		t.Fatalf("the provider is not listed as enabled: %s", providers.Body.String())
	}

	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/oidc/login", nil))
	if login.Code != http.StatusFound {
		t.Fatalf("login: status %d, want 302: %s", login.Code, login.Body.String())
	}
	authorize, err := url.Parse(login.Header().Get("Location"))
	if err != nil || !strings.HasPrefix(authorize.String(), idp.server.URL+"/authorize") {
		t.Fatalf("login redirects to %q, want the provider's authorize endpoint", login.Header().Get("Location"))
	}
	query := authorize.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("state") == "" {
		t.Fatalf("the authorize request lacks PKCE or state: %v", query)
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login set no state cookie")
	}

	code := idp.remember(query.Get("code_challenge"))
	callbackRequest := httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/oidc/callback?code="+code+"&state="+url.QueryEscape(query.Get("state")), nil)
	for _, cookie := range cookies {
		callbackRequest.AddCookie(cookie)
	}
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, callbackRequest)
	if callback.Code != http.StatusFound {
		t.Fatalf("callback: status %d: %s", callback.Code, callback.Body.String())
	}
	location := callback.Header().Get("Location")
	if !strings.HasPrefix(location, "http://localhost:5173/auth/callback") || strings.Contains(location, "error=") {
		t.Fatalf("the callback redirects to %q", location)
	}
	fragment, _ := url.ParseQuery(strings.SplitN(location, "#", 2)[1])
	exchangeCode := fragment.Get("code")
	if exchangeCode == "" {
		t.Fatalf("no exchange code in %q", location)
	}

	exchange := httptest.NewRecorder()
	mux.ServeHTTP(exchange, httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/oidc/exchange?code="+exchangeCode, nil))
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange: status %d: %s", exchange.Code, exchange.Body.String())
	}
	var response authResponse
	if err := json.Unmarshal(exchange.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token == "" || response.User == nil || response.User.Email != "sso.user@example.com" {
		t.Fatalf("the session is wrong: %+v", response)
	}
	if !response.User.IsAdmin {
		t.Fatalf("a member of an admin group is not an admin: %+v", response.User)
	}
}

func TestSingleSignOnRefusesAWrongState(t *testing.T) {
	t.Parallel()
	idp := newFakeIdentityProvider(t, "babel-client", nil)
	mux := oidcTestHandler(t, idp)

	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/oidc/login", nil))
	authorize, _ := url.Parse(login.Header().Get("Location"))
	code := idp.remember(authorize.Query().Get("code_challenge"))

	request := httptest.NewRequest(http.MethodGet, "http://localhost:8090/api/v1/auth/oidc/callback?code="+code+"&state=forged", nil)
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, request)
	if !strings.Contains(callback.Header().Get("Location"), "error=") {
		t.Fatalf("a forged state was accepted: %q", callback.Header().Get("Location"))
	}
}

func TestOIDCConfigFromEnv(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	off, err := OIDCConfigFromEnv(env(nil), "jwt", "http://localhost:5173")
	if err != nil || off.Enabled {
		t.Fatalf("with OIDC_ENABLED unset: %+v, %v; want a disabled config", off, err)
	}

	_, err = OIDCConfigFromEnv(env(map[string]string{"OIDC_ENABLED": "true", "OIDC_CLIENT_ID": "x"}), "jwt", "")
	if err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER_URL") || !strings.Contains(err.Error(), "OIDC_REDIRECT_URL") {
		t.Fatalf("a half-configured OIDC_ENABLED must name what is missing, got %v", err)
	}

	_, err = OIDCConfigFromEnv(env(map[string]string{"OIDC_ENABLED": "yes please"}), "jwt", "")
	if err == nil {
		t.Fatal("an unreadable OIDC_ENABLED must be an error")
	}

	full := map[string]string{
		"OIDC_ENABLED": "true", "OIDC_ISSUER_URL": "https://issuer.example", "OIDC_CLIENT_ID": "id",
		"OIDC_REDIRECT_URL": "http://localhost:8090/api/v1/auth/oidc/callback", "OIDC_SCOPES": "openid, email",
		"OIDC_ADMIN_GROUPS": "a, b", "OIDC_PKCE_ENABLED": "false",
	}
	config, err := OIDCConfigFromEnv(env(full), "jwt", "http://localhost:5173/")
	if err != nil {
		t.Fatal(err)
	}
	if !config.OIDCConfigured() {
		t.Fatalf("a complete configuration is not usable: %+v", config)
	}
	if config.PKCEEnabled || len(config.Scopes) != 2 || len(config.AdminGroups) != 2 || config.FrontendCallbackURL != "http://localhost:5173/auth/callback" {
		t.Fatalf("the values were not read: %+v", config)
	}
	again, _ := OIDCConfigFromEnv(env(full), "jwt", "http://localhost:5173/")
	if string(again.StateSecret) != string(config.StateSecret) || len(config.StateSecret) == 0 {
		t.Fatal("the derived state secret must be stable across restarts")
	}
	other, _ := OIDCConfigFromEnv(env(full), "another-jwt-secret", "http://localhost:5173/")
	if string(other.StateSecret) == string(config.StateSecret) {
		t.Fatal("the derived state secret must differ between deployments")
	}
}

func (c OIDCConfig) OIDCConfigured() bool { return c.Enabled && c.IsConfigured() }
