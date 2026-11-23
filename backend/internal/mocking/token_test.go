package mocking

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites"
)

func TestTheGatewayTokenIsTheSharedSecretOrDerivedFromTheJWTSecret(t *testing.T) {
	if got := GatewayToken("shared-value", "jwt"); got != "shared-value" {
		t.Fatalf("a configured MOCK_SHARED_SECRET must be the token, got %q", got)
	}

	derived := GatewayToken("", "jwt-secret-one")
	if len(derived) != 64 {
		t.Fatalf("derived token = %q, want 64 hex characters", derived)
	}
	if derived != GatewayToken("  ", "jwt-secret-one") {
		t.Fatal("the derived token must not change between calls: a running sidecar would stop being accepted after a restart")
	}
	if derived == GatewayToken("", "jwt-secret-two") {
		t.Fatal("two deployments with different secrets got the same token")
	}
	if strings.Contains(derived, "jwt-secret-one") {
		t.Fatal("the derived token leaks the signing secret")
	}
	if GatewayToken("", "") != "" {
		t.Fatal("with no secret at all the token must be empty, which refuses every call")
	}
}

func TestTokenMatchesOnlyTheExactBearer(t *testing.T) {
	for name, tc := range map[string]struct {
		token, header string
		want          bool
	}{
		"exact":                {"abc", "Bearer abc", true},
		"padding":              {"abc", "  Bearer abc  ", true},
		"wrong":                {"abc", "Bearer abd", false},
		"longer":               {"abc", "Bearer abcd", false},
		"missing":              {"abc", "", false},
		"the old constant":     {"abc", "Bearer babelsuite-internal", false},
		"empty token, empty":   {"", "", false},
		"empty token, bearer":  {"", "Bearer ", false},
		"empty token, a value": {"", "Bearer anything", false},
	} {
		if got := TokenMatches(tc.token, tc.header); got != tc.want {
			t.Errorf("%s: TokenMatches(%q, %q) = %v, want %v", name, tc.token, tc.header, got, tc.want)
		}
	}
}

func callMock(t *testing.T, handlerToken, authorization string) int {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(NewService(suites.NewWorkspaceService()), handlerToken).Register(mux)
	request := httptest.NewRequest("POST", "/internal/mock-data/returns-control-plane/returns-api/create-return", strings.NewReader(`{}`))
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder.Code
}

// With no secret set the mock endpoints used to be open to anyone who could reach
// the control plane; with one set, every call from the gateway was refused,
// because the gateway always sent the same constant.
func TestTheMockEndpointsAlwaysRequireTheToken(t *testing.T) {
	const token = "gateway-token"
	for name, tc := range map[string]struct {
		handlerToken, authorization string
		refused                     bool
	}{
		"the right token":                   {token, "Bearer " + token, false},
		"no header":                         {token, "", true},
		"a wrong token":                     {token, "Bearer nope", true},
		"the old constant":                  {token, "Bearer babelsuite-internal", true},
		"no token configured, none sent":    {"", "", true},
		"no token configured, constant":     {"", "Bearer babelsuite-internal", true},
		"no token configured, empty bearer": {"", "Bearer ", true},
	} {
		code := callMock(t, tc.handlerToken, tc.authorization)
		if refused := code == http.StatusUnauthorized; refused != tc.refused {
			t.Errorf("%s: status %d, refused=%v want %v", name, code, refused, tc.refused)
		}
	}
}
