package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionWorkspaceDirFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		executionID  string
		wantSuffix   string
	}{
		{"abc123", "babel-workspace/abc123"},
		{"ABC-123", "babel-workspace/abc-123"},
		{"UPPER", "babel-workspace/upper"},
		{"a!b@c#d", "babel-workspace/a-b-c-d"},
	}
	for _, tc := range tests {
		t.Run(tc.executionID, func(t *testing.T) {
			t.Parallel()
			got := ExecutionWorkspaceDir(tc.executionID)
			if !strings.HasSuffix(filepath.ToSlash(got), tc.wantSuffix) {
				t.Fatalf("ExecutionWorkspaceDir(%q) = %q, want suffix %q", tc.executionID, got, tc.wantSuffix)
			}
		})
	}
}

func TestExecutionWorkspaceDirDistinctPerExecution(t *testing.T) {
	t.Parallel()
	a := ExecutionWorkspaceDir("exec-aaa")
	b := ExecutionWorkspaceDir("exec-bbb")
	if a == b {
		t.Fatalf("expected distinct dirs for different executions, both got %q", a)
	}
}

func TestExecutionWorkspaceDirStableForSameExecution(t *testing.T) {
	t.Parallel()
	id := "execution-xyz-999"
	first := ExecutionWorkspaceDir(id)
	second := ExecutionWorkspaceDir(id)
	if first != second {
		t.Fatalf("ExecutionWorkspaceDir(%q) returned different values: %q and %q", id, first, second)
	}
}

func TestExecutionWorkspaceDirUnderTempDir(t *testing.T) {
	t.Parallel()
	got := ExecutionWorkspaceDir("some-exec")
	if !strings.HasPrefix(got, os.TempDir()) {
		t.Fatalf("expected workspace dir under TempDir, got %q", got)
	}
}

func TestExecutionWorkspaceDirCreatable(t *testing.T) {
	t.Parallel()
	dir := ExecutionWorkspaceDir("test-create-exec")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	defer os.RemoveAll(dir)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected dir to exist after MkdirAll: %v", err)
	}
}
