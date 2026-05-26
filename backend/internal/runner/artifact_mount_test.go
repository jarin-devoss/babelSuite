package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadArtifactFromMountPlainPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := []byte("junit output")
	if err := os.WriteFile(filepath.Join(dir, "junit.xml"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readArtifactFromMount(dir, "junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadArtifactFromMountSubdir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "reports")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("<coverage>...</coverage>")
	if err := os.WriteFile(filepath.Join(sub, "coverage.xml"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readArtifactFromMount(dir, "reports/coverage.xml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadArtifactFromMountPathTraversalRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := readArtifactFromMount(dir, "../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestReadArtifactFromMountMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := readArtifactFromMount(dir, "nonexistent.xml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadArtifactFromMountGlobSingleMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "coverage")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("<cobertura>...</cobertura>")
	if err := os.WriteFile(filepath.Join(sub, "cov.xml"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readArtifactFromMount(dir, "coverage/*.xml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadArtifactFromMountGlobMultipleMatchesReturnsFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "reports")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.xml", "b.xml", "c.xml"} {
		if err := os.WriteFile(filepath.Join(sub, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readArtifactFromMount(dir, "reports/*.xml")
	if err != nil {
		t.Fatal(err)
	}
	// filepath.Glob returns results in lexical order; first match is a.xml.
	if string(got) != "a.xml" {
		t.Fatalf("expected first lexical match a.xml, got %q", got)
	}
}

func TestReadArtifactFromMountGlobNoMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := readArtifactFromMount(dir, "reports/*.xml")
	if err == nil {
		t.Fatal("expected error for glob with no matches, got nil")
	}
}

func TestArtifactTriggerMatchesStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		trigger string
		status  string
		want    bool
	}{
		{"", "success", true},
		{"success", "success", true},
		{"success", "failure", false},
		{"failure", "failure", true},
		{"failure", "success", false},
		{"always", "success", true},
		{"always", "failure", true},
		{"unknown", "success", false},
	}
	for _, tc := range tests {
		t.Run(tc.trigger+"/"+tc.status, func(t *testing.T) {
			t.Parallel()
			if got := artifactTriggerMatchesStatus(tc.trigger, tc.status); got != tc.want {
				t.Fatalf("artifactTriggerMatchesStatus(%q, %q) = %v, want %v", tc.trigger, tc.status, got, tc.want)
			}
		})
	}
}
