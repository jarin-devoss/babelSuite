package runner

import "testing"

func TestWorkspaceVolumeNameFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		executionID string
		want        string
	}{
		{"abc123", "babel-ws-abc123"},
		{"ABC-123", "babel-ws-abc-123"},
		{"exec_id_with_underscores", "babel-ws-exec-id-with-underscores"},
		{"UPPER", "babel-ws-upper"},
		{"a!b@c#d", "babel-ws-a-b-c-d"},
	}
	for _, tc := range tests {
		t.Run(tc.executionID, func(t *testing.T) {
			t.Parallel()
			got := workspaceVolumeName(tc.executionID)
			if got != tc.want {
				t.Fatalf("workspaceVolumeName(%q) = %q, want %q", tc.executionID, got, tc.want)
			}
		})
	}
}

func TestWorkspaceVolumeNameTruncation(t *testing.T) {
	t.Parallel()
	long := "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz"
	got := workspaceVolumeName(long)
	sanitized := sanitizeID(long)
	want := "babel-ws-" + sanitized
	if got != want {
		t.Fatalf("workspaceVolumeName(long) = %q, want %q", got, want)
	}
}

func TestWorkspaceVolumeNameDistinctPerExecution(t *testing.T) {
	t.Parallel()
	a := workspaceVolumeName("exec-aaa")
	b := workspaceVolumeName("exec-bbb")
	if a == b {
		t.Fatalf("expected distinct volume names for different executions, both got %q", a)
	}
}

func TestWorkspaceVolumeNameStableForSameExecution(t *testing.T) {
	t.Parallel()
	id := "execution-xyz-999"
	if workspaceVolumeName(id) != workspaceVolumeName(id) {
		t.Fatal("expected same volume name for same execution ID")
	}
}
