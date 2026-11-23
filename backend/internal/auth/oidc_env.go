package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

const oidcStatePurpose = "babelsuite-oidc-state"

// OIDCConfigFromEnv reads the documented OIDC_* variables (see
// docs/configuration.md). getenv is os.Getenv in the server.
//
// With OIDC_ENABLED unset or false it returns a disabled config. With it true,
// an incomplete configuration is an error that names what is missing: a server
// that starts with a half-configured single sign-on button, or none at all while
// the operator believes it is on, is worse than one that refuses to start.
//
// The state cookie is signed with AUTH_STATE_SECRET. When that is not set it is
// derived from jwtSecret, so it differs for every deployment and is the same after
// a restart, and needs no setting of its own.
func OIDCConfigFromEnv(getenv func(string) string, jwtSecret, frontendURL string) (OIDCConfig, error) {
	value := func(key string) string { return strings.TrimSpace(getenv(key)) }

	enabled, err := parseBoolEnv(value("OIDC_ENABLED"), false, "OIDC_ENABLED")
	if err != nil {
		return OIDCConfig{}, err
	}
	if !enabled {
		return OIDCConfig{}, nil
	}

	var missing []string
	for _, key := range []string{"OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_REDIRECT_URL"} {
		if value(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return OIDCConfig{}, fmt.Errorf("OIDC_ENABLED is true but %s is not set", strings.Join(missing, ", "))
	}

	pkce, err := parseBoolEnv(value("OIDC_PKCE_ENABLED"), true, "OIDC_PKCE_ENABLED")
	if err != nil {
		return OIDCConfig{}, err
	}

	stateSecret := value("AUTH_STATE_SECRET")
	if stateSecret == "" {
		mac := hmac.New(sha256.New, []byte(jwtSecret))
		mac.Write([]byte(oidcStatePurpose))
		stateSecret = fmt.Sprintf("%x", mac.Sum(nil))
	}

	callback := value("OIDC_FRONTEND_CALLBACK_URL")
	if callback == "" {
		callback = strings.TrimRight(frontendURL, "/") + "/auth/callback"
	}

	return OIDCConfig{
		Enabled:             true,
		ProviderID:          value("OIDC_PROVIDER_ID"),
		ProviderName:        value("OIDC_PROVIDER_NAME"),
		IssuerURL:           value("OIDC_ISSUER_URL"),
		ClientID:            value("OIDC_CLIENT_ID"),
		ClientSecret:        value("OIDC_CLIENT_SECRET"),
		RedirectURL:         value("OIDC_REDIRECT_URL"),
		FrontendCallbackURL: callback,
		Scopes:              splitList(value("OIDC_SCOPES")),
		PKCEEnabled:         pkce,
		StateSecret:         []byte(stateSecret),
		EmailClaim:          value("OIDC_EMAIL_CLAIM"),
		NameClaim:           value("OIDC_NAME_CLAIM"),
		GroupsClaim:         value("OIDC_GROUPS_CLAIM"),
		AdminGroups:         splitList(value("OIDC_ADMIN_GROUPS")),
	}, nil
}

func parseBoolEnv(raw string, fallback bool, name string) (bool, error) {
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, got %q", name, raw)
	}
	return parsed, nil
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
