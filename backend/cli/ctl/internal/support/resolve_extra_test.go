package support

import (
	"testing"

	"github.com/babelsuite/babelsuite/pkg/apiclient"
)

func TestSplitReferenceNoTag(t *testing.T) {
	t.Parallel()
	repo, version := SplitReference("localhost:5000/qa/suite")
	if repo != "localhost:5000/qa/suite" {
		t.Fatalf("expected full ref as repository, got %q", repo)
	}
	if version != "" {
		t.Fatalf("expected empty version, got %q", version)
	}
}

func TestSplitReferenceEmptyString(t *testing.T) {
	t.Parallel()
	repo, version := SplitReference("")
	if repo != "" || version != "" {
		t.Fatalf("expected empty strings for empty input, got %q %q", repo, version)
	}
}

func TestRepositoryPathStripsHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"localhost:5000/qa/suite", "qa/suite"},
		{"registry.example.com/team/suite", "team/suite"},
		{"just-a-name", "just-a-name"},
		{"", ""},
	}
	for _, tc := range cases {
		got := RepositoryPath(tc.input)
		if got != tc.want {
			t.Errorf("RepositoryPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLastRepositorySegment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"localhost:5000/qa/storefront-suite", "storefront-suite"},
		{"registry.example.com/suite", "suite"},
		{"suite", "suite"},
		{"", ""},
	}
	for _, tc := range cases {
		got := LastRepositorySegment(tc.input)
		if got != tc.want {
			t.Errorf("LastRepositorySegment(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveLaunchTargetNotFound(t *testing.T) {
	t.Parallel()
	_, err := ResolveLaunchTarget("nonexistent/suite", []apiclient.LaunchSuite{
		{ID: "other-suite", Repository: "localhost:5000/qa/other-suite"},
	})
	if err == nil {
		t.Fatal("expected error for unresolvable target")
	}
}

func TestResolveLaunchTargetByID(t *testing.T) {
	t.Parallel()
	item, err := ResolveLaunchTarget("payment-suite", []apiclient.LaunchSuite{
		{ID: "payment-suite", Repository: "localhost:5000/qa/payment-suite"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID != "payment-suite" {
		t.Fatalf("expected payment-suite, got %q", item.ID)
	}
}

func TestResolveCatalogTargetNotFound(t *testing.T) {
	t.Parallel()
	_, err := ResolveCatalogTarget("nonexistent", []apiclient.CatalogPackage{
		{ID: "real-suite", Repository: "localhost:5000/qa/real-suite"},
	})
	if err == nil {
		t.Fatal("expected error for unresolvable catalog target")
	}
}

func TestWriteSuiteFilesCreatesFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []apiclient.SuiteSourceFile{
		{Path: "suite.star", Content: "# placeholder"},
		{Path: "subdir/config.yaml", Content: "key: value"},
	}
	written, err := WriteSuiteFiles(dir, files, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 2 {
		t.Fatalf("expected 2 written files, got %d", written)
	}
}

func TestWriteSuiteFilesRejectsOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []apiclient.SuiteSourceFile{{Path: "suite.star", Content: "v1"}}
	if _, err := WriteSuiteFiles(dir, files, false); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	_, err := WriteSuiteFiles(dir, files, false)
	if err == nil {
		t.Fatal("expected error when overwriting without force flag")
	}
}

func TestWriteSuiteFilesAllowsOverwriteWithForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []apiclient.SuiteSourceFile{{Path: "suite.star", Content: "v1"}}
	if _, err := WriteSuiteFiles(dir, files, false); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	files[0].Content = "v2"
	if _, err := WriteSuiteFiles(dir, files, true); err != nil {
		t.Fatalf("force overwrite failed: %v", err)
	}
}
