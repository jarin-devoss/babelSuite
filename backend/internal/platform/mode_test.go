package platform

import (
	"strings"
	"testing"
)

func TestModeAllowsAgent(t *testing.T) {
	for _, tc := range []struct {
		mode, agentType string
		want            bool
	}{
		{ModeLocal, "local", true},
		{ModeLocal, "kubernetes", false},
		{ModeLocal, "remote-agent", false},
		{"", "docker", true},
		{ModeRemote, "local", false},
		{ModeRemote, "remote-agent", true},
		{ModeRemote, "kubernetes", true},
		{ModeHybrid, "local", true},
		{ModeHybrid, "kubernetes", true},
	} {
		if got := ModeAllowsAgent(tc.mode, tc.agentType); got != tc.want {
			t.Errorf("ModeAllowsAgent(%q, %q) = %v, want %v", tc.mode, tc.agentType, got, tc.want)
		}
	}
}

// A mode that leaves no enabled backend would refuse every launch, so it is
// not saved.
func TestValidateRejectsAModeWithNoBackend(t *testing.T) {
	settings := DefaultSettings() // local Docker enabled; remote and Kubernetes off
	for _, tc := range []struct{ mode, wantErr string }{
		{ModeLocal, ""},
		{ModeHybrid, ""},
		{ModeRemote, "Remote mode needs"},
		{"cloud", "must be local, remote, or hybrid"},
	} {
		settings.Mode = tc.mode
		normalize(&settings)
		err := validate(&settings)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("mode %q: %v", tc.mode, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("mode %q: err = %v, want %q", tc.mode, err, tc.wantErr)
		}
	}
}
