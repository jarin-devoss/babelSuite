package model

type SecuritySpec struct {
	Variant       string  `json:"variant"`
	Target        string  `json:"target,omitempty"`
	Technique     string  `json:"technique,omitempty"`
	FloodPath     string  `json:"floodPath,omitempty"`
	FloodRate     float64 `json:"floodRate,omitempty"`
	FloodDuration float64 `json:"floodDuration,omitempty"`
	FloodThrottle bool    `json:"floodThrottle,omitempty"`
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
