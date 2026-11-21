package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The APISIX image runs as uid 636 and rewrites its bind-mounted config.yaml in
// place. On a Linux host that only works if the file is writable by users other
// than the one that created it.
func TestWriteSidecarConfigIsWritableByTheContainerUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no unix permission bits to check")
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeSidecarConfig(path); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o006 != 0o006 {
		t.Fatalf("config.yaml mode is %v, want read and write for others so uid 636 can rewrite it", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "config_provider: yaml") {
		t.Fatalf("config.yaml does not select the yaml provider:\n%s", data)
	}
}
