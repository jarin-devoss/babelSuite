package platform

import "strings"

// Deployment modes: which of the enabled execution agents a run may use.
const (
	// ModeLocal runs every suite on this host's Docker; remote agents and
	// Kubernetes backends are left idle even when enabled.
	ModeLocal = "local"
	// ModeRemote runs every suite on remote agents or Kubernetes, and never on
	// this host's Docker — for a control plane that should only schedule.
	ModeRemote = "remote"
	// ModeHybrid uses every enabled backend: a profile's agent.tags route it
	// to a backend carrying them, and the rest go to the default.
	ModeHybrid = "hybrid"
)

// IsLocalAgentType reports whether an agent type runs on this host's Docker.
func IsLocalAgentType(agentType string) bool {
	switch strings.ToLower(strings.TrimSpace(agentType)) {
	case "", "local", "docker":
		return true
	}
	return false
}

// ModeAllowsAgent reports whether a run may use an agent of agentType under
// the deployment mode. An unset mode is read as local, as normalize sets it.
func ModeAllowsAgent(mode, agentType string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeHybrid:
		return true
	case ModeRemote:
		return !IsLocalAgentType(agentType)
	default:
		return IsLocalAgentType(agentType)
	}
}

func validMode(mode string) bool {
	switch mode {
	case ModeLocal, ModeRemote, ModeHybrid:
		return true
	}
	return false
}
