package mocking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// gatewayTokenPurpose separates the token derived for the mock gateway from any
// other use of the same signing secret.
const gatewayTokenPurpose = "babelsuite-mock-gateway"

// GatewayToken is the secret a suite's gateway presents to the mock engine, and
// the engine requires on every call.
//
// It is the configured MOCK_SHARED_SECRET when there is one. Otherwise it is
// derived from the JWT signing secret, so it is different for every deployment,
// is the same after a restart (a running sidecar stays valid), and needs no
// setting of its own. It is empty only when neither secret exists, and an empty
// token makes the engine refuse every call.
func GatewayToken(sharedSecret, jwtSecret string) string {
	if shared := strings.TrimSpace(sharedSecret); shared != "" {
		return shared
	}
	signing := strings.TrimSpace(jwtSecret)
	if signing == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(signing))
	mac.Write([]byte(gatewayTokenPurpose))
	return hex.EncodeToString(mac.Sum(nil))
}
