package suites

import (
	"github.com/babelsuite/babelsuite/internal/suites/plan"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pythonFence = regexp.MustCompile("(?s)```python\n(.*?)```")

// Every Starlark example in the docs that is a whole suite.star (it starts with a
// load()) is evaluated by the real evaluator, against the embedded modules. A doc
// example that calls a helper that does not exist, or passes a keyword nobody
// reads, then fails here instead of failing for the first reader who copies it.
func TestDocsStarlarkExamplesEvaluate(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../../../docs/*.md", "../../../examples/oci-modules/*/README.md"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	checked := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		for index, match := range pythonFence.FindAllStringSubmatch(text, -1) {
			block := strings.TrimSpace(match[1])
			if !strings.HasPrefix(block, "load(") {
				continue
			}
			// A module's own file loads its siblings by path (load("cluster.star", ...)).
			if strings.HasPrefix(block, `load("`) && strings.Contains(strings.SplitN(block, "\n", 2)[0], `.star"`) {
				continue
			}
			checked++
			if strings.Contains(block, "traffic.plan(") {
				// A traffic plan file, not a suite.star.
				if _, err := plan.Parse(&LoadSpec{Variant: "traffic.baseline", Target: "http://target"}, block, nil); err != nil {
					t.Errorf("%s: block %d is a plan that does not evaluate: %v\n%s", file, index+1, err, block)
				}
				continue
			}
			name := filepath.Base(file) + "#" + string(rune('a'+index%26))
			t.Run(name, func(t *testing.T) {
				_, err := evalStarlarkTopology(block, docsModuleResolver)
				// A block that only loads and defines (a module's own file, a helper)
				// registers no node: it is checked for syntax and for what it loads.
				if err != nil && !strings.Contains(err.Error(), "no topology nodes registered") {
					t.Errorf("%s: block %d does not evaluate: %v\n%s", file, index+1, err, block)
				}
			})
		}
	}
	if checked == 0 {
		t.Fatal("no Starlark example was found in the docs")
	}
}

// docsModuleResolver serves what the examples load that is not embedded: the
// plugins (`@plugins/<name>`, registered in platform settings in a real install)
// and the example modules, from the repository.
func docsModuleResolver(name string) (map[string]string, error) {
	root := "../../../examples"
	for _, dir := range []string{"oci-modules", "oci-plugins"} {
		folder := filepath.Join(root, dir, name)
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		files := map[string]string{}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".star" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(folder, entry.Name()))
			if err != nil {
				return nil, err
			}
			files[entry.Name()] = string(data)
		}
		if len(files) > 0 {
			return files, nil
		}
	}
	return nil, nil
}
