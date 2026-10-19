package main

import (
	"os"
	"path/filepath"
	"testing"
)

// On a fresh checkout there is no settings file — it is not in git — so the
// server creates one from the demo template at the repo root, whether it was
// started from there or from backend/, and leaves an existing one alone.
func TestPlatformSettingsPathSeedsFromTheDemoTemplate(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	template := []byte("mode: local\n")
	if err := os.WriteFile(filepath.Join(repo, "demo", "configuration.demo.yaml"), template, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(repo, "backend"))

	got := platformSettingsPath("configuration.yaml")
	if want := filepath.Join("..", "configuration.yaml"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(repo, "configuration.yaml"))
	if err != nil || string(data) != string(template) {
		t.Fatalf("seeded file = %q, %v; want the template", data, err)
	}

	// An existing file is used as it is, never overwritten.
	if err := os.WriteFile(filepath.Join(repo, "configuration.yaml"), []byte("mode: hybrid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := platformSettingsPath("configuration.yaml"); got != filepath.Join("..", "configuration.yaml") {
		t.Fatalf("existing file: path = %q", got)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "configuration.yaml")); string(data) != "mode: hybrid\n" {
		t.Fatalf("an existing settings file was rewritten: %q", data)
	}
}

// A missing absolute path was named on purpose; it is reported, not seeded.
func TestPlatformSettingsPathLeavesAbsolutePathsAlone(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "settings.yaml")
	if got := platformSettingsPath(missing); got != missing {
		t.Fatalf("path = %q, want %q", got, missing)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("an absolute path was created: %v", err)
	}
}
