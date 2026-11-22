package suites

import (
	"sort"
	"strings"
)

type SecuritySpec struct {
	Variant        string              `json:"variant"`
	Target         string              `json:"target,omitempty"`
	Technique      string              `json:"technique,omitempty"`
	FloodPath      string              `json:"floodPath,omitempty"`
	FloodRate      float64             `json:"floodRate,omitempty"`
	FloodDuration  float64             `json:"floodDuration,omitempty"`
	FloodThrottle  bool                `json:"floodThrottle,omitempty"`
	// FailOn is the lowest finding severity that fails the step: low, medium,
	// high or critical. Empty means low, so any finding fails it.
	FailOn string `json:"failOn,omitempty"`
}

// SecurityTechniques are the payload families security.fuzz can be limited to.
var SecurityTechniques = []string{"sqli", "xss", "traversal", "header-injection"}

// SecurityFailLevels are the values fail_on= accepts, lowest first.
var SecurityFailLevels = []string{"low", "medium", "high", "critical"}

// Caps on security.flood: it is a load test against the target, so it is
// bounded like any other traffic.
const (
	MaxSecurityFloodRate     = 100.0
	MaxSecurityFloodDuration = 60.0
)

// securityCommonKeys are the arguments every security.* call accepts.
var securityCommonKeys = map[string]bool{
	"name": true, "name_or_id": true, "id": true, "message": true, "after": true,
	"on_failure": true, "continue_on_failure": true, "exports": true, "env": true,
	"target": true, "fail_on": true,
	"expect_exit": true, "expect_logs": true, "fail_on_logs": true,
}

// securityVariantKeys are the arguments only some security.* calls accept.
var securityVariantKeys = map[string][]string{
	"security.fuzz":  {"technique"},
	"security.flood": {"path", "rate", "duration", "expect_throttle"},
}

// securityKeyAllowed reports whether a security.* call accepts the argument.
// An argument it does not know is an error, because the check would otherwise
// run without it and look as if it had been honoured.
func securityKeyAllowed(variant, key string) bool {
	if securityCommonKeys[key] {
		return true
	}
	for _, allowed := range securityVariantKeys[variant] {
		if allowed == key {
			return true
		}
	}
	return false
}

// securityKeyList names the arguments a security.* call accepts, for the error
// that rejects one it does not.
func securityKeyList(variant string) string {
	keys := make([]string, 0, len(securityCommonKeys)+4)
	for key := range securityCommonKeys {
		if key == "name_or_id" || key == "id" {
			continue
		}
		keys = append(keys, key)
	}
	keys = append(keys, securityVariantKeys[variant]...)
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
