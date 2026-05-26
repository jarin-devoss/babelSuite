package envloader

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadSetsVariables(t *testing.T) {
	t.Setenv("ENVLOADER_TEST_A", "")
	os.Unsetenv("ENVLOADER_TEST_A")
	f := writeEnvFile(t, "ENVLOADER_TEST_A=hello\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_A"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
}

func TestLoadStripsQuotes(t *testing.T) {
	os.Unsetenv("ENVLOADER_TEST_QUOTED")
	f := writeEnvFile(t, `ENVLOADER_TEST_QUOTED="quoted value"`)
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_QUOTED"); got != "quoted value" {
		t.Fatalf("expected quoted value, got %q", got)
	}
}

func TestLoadSkipsComments(t *testing.T) {
	os.Unsetenv("ENVLOADER_TEST_COMMENT")
	f := writeEnvFile(t, "# ENVLOADER_TEST_COMMENT=should-not-set\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_COMMENT"); got != "" {
		t.Fatalf("comment line must not set variable, got %q", got)
	}
}

func TestLoadDoesNotOverrideExisting(t *testing.T) {
	t.Setenv("ENVLOADER_TEST_EXISTING", "original")
	f := writeEnvFile(t, "ENVLOADER_TEST_EXISTING=overwrite\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_EXISTING"); got != "original" {
		t.Fatalf("existing value must not be overridden, got %q", got)
	}
}

func TestLoadMissingFileIsNoop(t *testing.T) {
	Load("/nonexistent/path/.env")
}

func TestLoadDefaultPathWhenNoneGiven(t *testing.T) {
	Load()
}

func TestLoadSkipsBlankLines(t *testing.T) {
	os.Unsetenv("ENVLOADER_TEST_BLANK")
	f := writeEnvFile(t, "\n\n\nENVLOADER_TEST_BLANK=set\n\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_BLANK"); got != "set" {
		t.Fatalf("expected set, got %q", got)
	}
}

func TestLoadSkipsLinesWithoutEquals(t *testing.T) {
	os.Unsetenv("ENVLOADER_TEST_NOEQ")
	f := writeEnvFile(t, "ENVLOADER_TEST_NOEQ\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_NOEQ"); got != "" {
		t.Fatalf("line without = must not set variable, got %q", got)
	}
}

func TestLoadSingleQuotedValue(t *testing.T) {
	os.Unsetenv("ENVLOADER_TEST_SINGLE")
	f := writeEnvFile(t, "ENVLOADER_TEST_SINGLE='single quoted'\n")
	Load(f)
	if got := os.Getenv("ENVLOADER_TEST_SINGLE"); got != "single quoted" {
		t.Fatalf("expected single quoted, got %q", got)
	}
}
