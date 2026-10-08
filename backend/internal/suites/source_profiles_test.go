package suites

import (
	"strings"
	"testing"
)

func TestRegisterKeepsSourceFilesAndDerivesProfiles(t *testing.T) {
	service := NewService()

	definition, err := service.Register(RegisterRequest{
		ID:        "authored-suite",
		SuiteStar: `smoke = test.run(file="smoke.py", image="python:3.12")`,
		SourceFiles: []SourceFile{
			{Path: "tests/smoke.py", Content: "print('hi')"},
			{Path: "profiles/local.yaml", Content: "name: Local Debug\ndescription: Verbose\ndefault: true\n"},
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if len(definition.SourceFiles) != 2 {
		t.Fatalf("expected the source files to be kept, got %d", len(definition.SourceFiles))
	}
	// A step declaring file="smoke.py" has nothing to run unless the file
	// travels with the suite.
	var smoke *SourceFile
	for i := range definition.SourceFiles {
		if definition.SourceFiles[i].Path == "tests/smoke.py" {
			smoke = &definition.SourceFiles[i]
		}
	}
	if smoke == nil || smoke.Content != "print('hi')" {
		t.Fatalf("smoke test file was not stored: %+v", definition.SourceFiles)
	}
	if smoke.Language == "" {
		t.Error("expected the language to be detected from the path")
	}

	if len(definition.Profiles) != 1 {
		t.Fatalf("expected one derived profile, got %d", len(definition.Profiles))
	}
	profile := definition.Profiles[0]
	if profile.FileName != "local.yaml" || profile.Label != "Local Debug" || !profile.Default {
		t.Fatalf("profile was not derived from the YAML: %+v", profile)
	}
}

func TestRegisterRejectsAnInvalidSourcePath(t *testing.T) {
	service := NewService()

	_, err := service.Register(RegisterRequest{
		ID:          "bad-suite",
		SuiteStar:   `db = service.run(image="postgres:16")`,
		SourceFiles: []SourceFile{{Path: "../escape.py", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected a path escaping the suite root to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes the suite root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterWithoutSourceFilesWorksWhenNoStepNamesAFile(t *testing.T) {
	service := NewService()

	definition, err := service.Register(RegisterRequest{
		ID:        "shell-suite",
		SuiteStar: `db = service.run(image="postgres:16")`,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(definition.SourceFiles) != 0 || len(definition.Profiles) != 0 {
		t.Fatalf("expected an empty package, got %d files and %d profiles",
			len(definition.SourceFiles), len(definition.Profiles))
	}
}

func TestProfilesFromSourceFilesIgnoresNonProfileFiles(t *testing.T) {
	profiles := ProfilesFromSourceFiles([]SourceFile{
		{Path: "tests/smoke.py", Content: "print('hi')"},
		{Path: "profiles/README.md", Content: "not a profile"},
		{Path: "profiles/ci.yaml", Content: "name: CI\n"},
	})

	if len(profiles) != 1 || profiles[0].FileName != "ci.yaml" {
		t.Fatalf("expected only the YAML profile, got %+v", profiles)
	}
}
