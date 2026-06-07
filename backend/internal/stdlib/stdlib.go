package stdlib

import (
	"embed"
	"io/fs"
)

//go:embed all:modules
var modulesFS embed.FS

// ModuleFS returns a filesystem rooted at the named module directory.
// For example, Open("postgres") gives access to postgres/_shared.star etc.
func ModuleFS(name string) (fs.FS, error) {
	return fs.Sub(modulesFS, "modules/"+name)
}

// Modules returns all embedded module names.
func Modules() []string {
	entries, err := modulesFS.ReadDir("modules")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}
