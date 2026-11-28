package suites

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The packages under suites form layers, each importing only the ones below it:
//
//	model, starkit   import nothing of the repository
//	eval             model, starkit
//	plan             model, starkit
//	topology         model, eval, plan
//
// suites itself assembles them (loading, the registry) and re-exports the data
// types under their old names. A lower layer never imports suites or a layer
// above it, so evaluating, resolving and planning can be used and tested without
// the loaders and the registry.
const modulePrefix = "github.com/babelsuite/babelsuite/internal/"

var allowedImports = map[string][]string{
	"model":    {},
	"starkit":  {},
	"eval":     {"suites/model", "suites/starkit", "stdlib"},
	"plan":     {"suites/model", "suites/starkit", "strutil"},
	"topology": {"suites/model", "suites/eval", "suites/plan", "strutil"},
}

func TestSuitesPackagesKeepTheirLayering(t *testing.T) {
	for pkg, allowed := range allowedImports {
		files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no files (%v)", pkg, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(path, modulePrefix) {
					continue
				}
				rel := strings.TrimPrefix(path, modulePrefix)
				ok := false
				for _, a := range allowed {
					ok = ok || rel == a
				}
				if !ok {
					t.Errorf("%s imports %s, which is not below it", file, rel)
				}
			}
		}
	}
	// Every sub-package is covered by the table, so a new one must be placed in it.
	entries, _ := os.ReadDir(".")
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "testdata" {
			if _, ok := allowedImports[entry.Name()]; !ok {
				t.Errorf("suites/%s is not in the layering table", entry.Name())
			}
		}
	}
}
