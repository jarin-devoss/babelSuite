package suites

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"github.com/babelsuite/babelsuite/internal/suites/plan"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Golden files pin what the evaluator produces: the resolved step graph of every
// example suite and every whole suite.star in the docs, and the traffic plan of
// every plan file and plan block. The evaluator is refactored behind them, so a
// diff in a golden file is a behaviour change. UPDATE_GOLDEN=1 rewrites them.

const goldenDir = "testdata/golden"

func checkGolden(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(goldenDir, name+".json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden file %s: run with UPDATE_GOLDEN=1", path)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(data) {
		t.Errorf("%s differs from its golden file; run with UPDATE_GOLDEN=1 only if the change is intended", path)
	}
}

type goldenGraph struct {
	Error        string
	Topology     []TopologyNode
	Dependencies []ResolvedDependency
}

func TestStepGraphGolden(t *testing.T) {
	catalog := NewWorkspaceService().List()
	for _, suite := range catalog {
		suite := suite
		t.Run(suite.ID, func(t *testing.T) {
			resolved, err := ResolveRuntimeWithEnv(suite, catalog, docsModuleResolver, defaultProfileEnv(suite))
			graph := goldenGraph{Topology: resolved.Nodes, Dependencies: resolved.Dependencies}
			if err != nil {
				graph.Error = err.Error()
			}
			checkGolden(t, "graph/"+suite.ID, graph)
		})
	}

	// Every whole suite.star in the docs.
	var files []string
	for _, pattern := range []string{"../../../docs/*.md", "../../../examples/oci-modules/*/README.md"} {
		matches, _ := filepath.Glob(pattern)
		files = append(files, matches...)
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		for _, match := range pythonFence.FindAllStringSubmatch(text, -1) {
			block := strings.TrimSpace(match[1])
			if !strings.HasPrefix(block, "load(") || strings.Contains(strings.SplitN(block, "\n", 2)[0], `.star"`) || strings.Contains(block, "traffic.plan(") {
				continue
			}
			name := "docs/" + docBlockName(file, block)
			suite := Definition{ID: "doc", SuiteStar: block}
			resolved, err := ResolveRuntimeWithEnv(suite, nil, docsModuleResolver, nil)
			graph := goldenGraph{Topology: resolved.Nodes}
			if err != nil {
				graph.Error = err.Error()
			}
			checkGolden(t, "graph/"+name, graph)
		}
	}
}

var planFence = regexp.MustCompile("(?s)```python\n(.*?)```")

type goldenPlan struct {
	Error string
	Spec  *LoadSpec
}

func TestPlanGolden(t *testing.T) {
	// Every traffic/*.star of the examples.
	plans, _ := filepath.Glob(filepath.Join(examplesRoot, "*", "traffic", "*.star"))
	if len(plans) == 0 {
		t.Fatal("no example traffic plans found")
	}
	for _, planFile := range plans {
		data, err := os.ReadFile(planFile)
		if err != nil {
			t.Fatal(err)
		}
		suite := filepath.Base(filepath.Dir(filepath.Dir(planFile)))
		spec, err := plan.Parse(&LoadSpec{Variant: "traffic.baseline", Target: "http://target"}, strings.ReplaceAll(string(data), "\r\n", "\n"), nil)
		out := goldenPlan{Spec: spec}
		if err != nil {
			out.Error = err.Error()
		}
		checkGolden(t, "plan/"+suite+"-"+strings.TrimSuffix(filepath.Base(planFile), ".star"), out)
	}

	// Every plan block in the docs (a python fence that calls traffic.plan).
	files, _ := filepath.Glob("../../../docs/*.md")
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		for _, match := range planFence.FindAllStringSubmatch(text, -1) {
			block := strings.TrimSpace(match[1])
			if !strings.Contains(block, "traffic.plan(") {
				continue
			}
			spec, err := plan.Parse(&LoadSpec{Variant: "traffic.baseline", Target: "http://target"}, block, nil)
			out := goldenPlan{Spec: spec}
			if err != nil {
				out.Error = err.Error()
			}
			checkGolden(t, "plan/docs-"+docBlockName(file, block), out)
		}
	}
}

// docBlockName names a docs block by its file and a hash of its text, so adding a
// block to a page does not rename the golden files of the ones after it.
func docBlockName(file, block string) string {
	sum := sha1.Sum([]byte(block))
	base := strings.TrimSuffix(filepath.Base(file), ".md")
	if dir := filepath.Base(filepath.Dir(file)); dir != "docs" {
		base = dir
	}
	return base + "-" + hex.EncodeToString(sum[:4])
}
