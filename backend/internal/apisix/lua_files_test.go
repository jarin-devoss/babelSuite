package apisix

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The plugin files are bind-mounted read-only into a container that runs as the
// unprivileged user apisix (uid 636), not as the user that wrote them. On a
// Linux host a 0600 file is unreadable there, so APISIX cannot load the plugin
// and the sidecar fails to start.
func TestWriteLuaPluginFilesAreReadableByOtherUsers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no unix permission bits to check")
	}

	dir := t.TempDir()
	custom := CustomPluginConfig{Name: "readable-check", Trigger: "/x", Lua: "return {}"}
	if err := WriteLuaPluginFiles(dir, custom); err != nil {
		t.Fatalf("write: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.lua"))
	if err != nil || len(files) < 3 {
		t.Fatalf("want the two built-in plugins and the custom one, got %v (err %v)", files, err)
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat %s: %v", file, err)
		}
		if info.Mode().Perm()&0o004 == 0 {
			t.Errorf("%s has mode %v, want it readable by others", filepath.Base(file), info.Mode().Perm())
		}
	}
}
