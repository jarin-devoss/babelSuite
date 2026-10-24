package suites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// service.run starts a container, so it needs an image; mock.api is a fake
// API with no container.
func TestRuntimeNames(t *testing.T) {
	t.Parallel()

	nodes, err := evalStarlarkTopology(`
load("@babelsuite/runtime", "service", "mock", "task", "test")

stripe  = mock.api(name="stripe-mock")
db      = service.run(name="db", image="postgres:16")
migrate = task.run(file="migrate.py", image="python:3.12", after=[db])
smoke   = test.run(file="smoke.py", image="python:3.12", after=[migrate, stripe])
`, nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	want := map[string][2]string{
		"stripe-mock": {NodeKindMock, VariantMockAPI},
		"db":          {NodeKindService, VariantServiceRun},
		"migrate":     {NodeKindTask, "task.run"},
		"smoke":       {NodeKindTest, "test.run"},
	}
	for _, node := range nodes {
		if w, ok := want[node.Name]; ok && (node.Kind != w[0] || node.Variant != w[1]) {
			t.Errorf("%s: kind/variant = %s/%s, want %s/%s", node.Name, node.Kind, node.Variant, w[0], w[1])
		}
	}
	if len(nodes) != len(want) {
		t.Errorf("got %d nodes, want %d", len(nodes), len(want))
	}
}

func TestRuntimeNamesRejected(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ star, wantErr string }{
		{`api = service.run(name="api")`, "service.run needs image="},
		{`api = service.run(name="api", image="")`, "service.run needs image="},
		{`m = service.mock(name="m")`, "mock was renamed to mock.api"},
	} {
		_, err := evalStarlarkTopology("load(\"@babelsuite/runtime\", \"service\", \"mock\")\n"+tc.star, nil)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", tc.star, err, tc.wantErr)
		}
	}
}

// Every example suite evaluates against the current runtime, with the example
// modules and plugins it loads, so a rename cannot leave one behind.
func TestExampleSuitesEvaluate(t *testing.T) {
	t.Parallel()

	examples := filepath.Join("..", "..", "..", "examples")
	resolver := func(name string) (map[string]string, error) {
		for _, dir := range []string{filepath.Join(examples, "oci-modules", name), filepath.Join(examples, "oci-plugins", name)} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			files := map[string]string{}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".star") {
					data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
					if err != nil {
						return nil, err
					}
					files[entry.Name()] = string(data)
				}
			}
			return files, nil
		}
		return nil, nil
	}

	suites, err := filepath.Glob(filepath.Join(examples, "oci-suites", "*"))
	if err != nil || len(suites) == 0 {
		t.Fatalf("no example suites found: %v", err)
	}
	for _, suite := range suites {
		for _, file := range []string{"suite.star", "pre-hook.star"} {
			data, err := os.ReadFile(filepath.Join(suite, file))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := evalStarlarkTopology(string(data), resolver); err != nil {
				t.Errorf("%s/%s: %v", filepath.Base(suite), file, err)
			}
		}
	}
}
