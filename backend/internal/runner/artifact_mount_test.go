package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMountFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func collectOne(t *testing.T, dir, export string) ArtifactResult {
	t.Helper()
	results := collectMountArtifacts(dir, []ArtifactExport{{Path: export}})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	return results[0]
}

func TestCollectMountArtifactsPlainPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMountFile(t, dir, "junit.xml", "junit output")
	result := collectOne(t, dir, "junit.xml")
	if result.Missing || result.Err != nil || len(result.Files) != 1 || string(result.Files[0].Content) != "junit output" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestCollectMountArtifactsSubdirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMountFile(t, dir, "reports/coverage.xml", "<coverage/>")
	result := collectOne(t, dir, "reports/coverage.xml")
	if len(result.Files) != 1 || result.Files[0].Path != "reports/coverage.xml" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestCollectMountArtifactsRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	result := collectOne(t, t.TempDir(), "../../etc/passwd")
	if result.Err == nil && !result.Missing {
		t.Fatalf("a path that escapes the workspace must not be read: %+v", result)
	}
	if len(result.Files) != 0 {
		t.Fatalf("a path that escapes the workspace must not return files: %+v", result)
	}
}

func TestCollectMountArtifactsMissingFile(t *testing.T) {
	t.Parallel()
	result := collectOne(t, t.TempDir(), "nonexistent.xml")
	if !result.Missing || result.Err != nil || len(result.Files) != 0 {
		t.Fatalf("expected a missing result, got %+v", result)
	}
}

func TestCollectMountArtifactsDirectoryIsAReadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := collectOne(t, dir, "reports")
	if result.Err == nil || result.Missing {
		t.Fatalf("a directory where a file belongs must be a read error, not a missing file: %+v", result)
	}
}

func TestCollectMountArtifactsGlobReturnsEveryMatchInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"c.xml", "a.xml", "b.xml"} {
		writeMountFile(t, dir, "reports/"+name, name)
	}
	writeMountFile(t, dir, "reports/notes.txt", "skipped by the pattern")
	result := collectOne(t, dir, "reports/*.xml")
	if len(result.Files) != 3 {
		t.Fatalf("expected 3 files, got %+v", result.Files)
	}
	for index, want := range []string{"reports/a.xml", "reports/b.xml", "reports/c.xml"} {
		if result.Files[index].Path != want {
			t.Fatalf("file %d = %q, want %q", index, result.Files[index].Path, want)
		}
	}
}

func TestCollectMountArtifactsGlobWithNoMatchIsMissing(t *testing.T) {
	t.Parallel()
	result := collectOne(t, t.TempDir(), "reports/*.xml")
	if !result.Missing || result.Err != nil {
		t.Fatalf("expected a missing result, got %+v", result)
	}
}

func TestCollectMountArtifactsGlobSkipsDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "reports", "nested.xml"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeMountFile(t, dir, "reports/a.xml", "a")
	result := collectOne(t, dir, "reports/*.xml")
	if len(result.Files) != 1 || result.Files[0].Path != "reports/a.xml" {
		t.Fatalf("expected only the regular file, got %+v", result)
	}
}

func TestCollectMountArtifactsFileOverTheLimitIsTruncatedNotRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMountFile(t, dir, "big.xml", strings.Repeat("x", maxArtifactBytes+1))
	result := collectOne(t, dir, "big.xml")
	if len(result.Files) != 1 || !result.Files[0].Truncated || len(result.Files[0].Content) != 0 {
		t.Fatalf("a file over the limit must be reported truncated with no content, got %+v", result.Files)
	}
}

func TestCollectMountArtifactsFileExactlyAtTheLimitIsRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMountFile(t, dir, "exact.xml", strings.Repeat("x", maxArtifactBytes))
	result := collectOne(t, dir, "exact.xml")
	if len(result.Files) != 1 || result.Files[0].Truncated || len(result.Files[0].Content) != maxArtifactBytes {
		t.Fatalf("a file at the limit must be read whole, got truncated=%v", result.Files[0].Truncated)
	}
}

func TestCollectMountArtifactsGlobStopsAtTheFileLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for index := 0; index < maxArtifactFiles+3; index++ {
		writeMountFile(t, dir, fmt.Sprintf("r/%03d.xml", index), "x")
	}
	result := collectOne(t, dir, "r/*.xml")
	if len(result.Files) != maxArtifactFiles+3 {
		t.Fatalf("expected every match listed, got %d", len(result.Files))
	}
	truncated := 0
	for _, file := range result.Files {
		if file.Truncated {
			truncated++
		}
	}
	if truncated != 3 {
		t.Fatalf("expected the 3 files beyond the limit to be truncated, got %d", truncated)
	}
}

func TestCollectMountArtifactsReportsEveryExportWhateverItsTrigger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMountFile(t, dir, "late.xml", "<testsuite/>")
	results := collectMountArtifacts(dir, []ArtifactExport{
		{Path: "late.xml", On: "failure"},
		{Path: "absent.xml", On: "success"},
	})
	if len(results) != 2 || len(results[0].Files) != 1 || !results[1].Missing {
		t.Fatalf("every declared export must be reported, got %+v", results)
	}
}

func TestNotCollectedResultsNameTheReason(t *testing.T) {
	t.Parallel()
	results := notCollectedResults([]ArtifactExport{{Path: "a.xml"}, {Path: "b.xml"}}, "no way to read files")
	if len(results) != 2 || results[0].NotCollected != "no way to read files" || results[1].Export != "b.xml" {
		t.Fatalf("unexpected results %+v", results)
	}
}

func TestCollectPodArtifactsReadsListedFiles(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		k8sArtifactsMount + "/reports/a.xml": "A",
		k8sArtifactsMount + "/reports/b.xml": "B",
	}
	run := func(command []string) ([]byte, error) {
		target := command[len(command)-1]
		if strings.Contains(command[2], "printf") {
			var listed []string
			for _, name := range []string{"/reports/a.xml", "/reports/b.xml"} {
				if strings.HasSuffix(target, "*.xml") || strings.HasSuffix(target, name) {
					listed = append(listed, k8sArtifactsMount+name)
				}
			}
			return []byte(strings.Join(listed, "\n") + "\n"), nil
		}
		return []byte(files[target]), nil
	}
	results := collectPodArtifacts([]ArtifactExport{{Path: "reports/*.xml"}}, run)
	if len(results) != 1 || len(results[0].Files) != 2 || string(results[0].Files[1].Content) != "B" {
		t.Fatalf("unexpected results %+v", results)
	}
	if results[0].Files[0].Path != "reports/a.xml" {
		t.Fatalf("expected paths relative to the artifacts mount, got %q", results[0].Files[0].Path)
	}
}

func TestCollectPodArtifactsNothingListedIsMissing(t *testing.T) {
	t.Parallel()
	run := func([]string) ([]byte, error) { return nil, nil }
	results := collectPodArtifacts([]ArtifactExport{{Path: "reports/junit.xml"}}, run)
	if len(results) != 1 || !results[0].Missing {
		t.Fatalf("expected a missing result, got %+v", results)
	}
}

func TestCollectPodArtifactsExecFailureIsAReadError(t *testing.T) {
	t.Parallel()
	run := func([]string) ([]byte, error) { return nil, errors.New("exec refused") }
	results := collectPodArtifacts([]ArtifactExport{{Path: "reports/junit.xml"}}, run)
	if len(results) != 1 || results[0].Err == nil || results[0].Missing {
		t.Fatalf("expected a read error, got %+v", results)
	}
}

func TestCollectPodArtifactsFileOverTheLimitIsTruncated(t *testing.T) {
	t.Parallel()
	run := func(command []string) ([]byte, error) {
		if strings.Contains(command[2], "printf") {
			return []byte(k8sArtifactsMount + "/big.xml\n"), nil
		}
		return []byte(strings.Repeat("x", maxArtifactBytes+1)), nil
	}
	results := collectPodArtifacts([]ArtifactExport{{Path: "big.xml"}}, run)
	if len(results) != 1 || len(results[0].Files) != 1 || !results[0].Files[0].Truncated {
		t.Fatalf("expected a truncated file, got %+v", results)
	}
}
