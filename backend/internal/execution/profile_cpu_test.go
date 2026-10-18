package execution

import "testing"

// CPU limits come from a profile with the Kubernetes CPU grammar. Anything
// that is not a positive amount means "no limit", never a throttle.
func TestParseCPUQuantity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  any
		want int64
	}{
		{"whole cores", "2", 2000},
		{"fractional cores", "1.5", 1500},
		{"millicores", "500m", 500},
		{"surrounding space", "  250m ", 250},
		{"plain int from YAML", 2, 2000},
		{"float from YAML", 0.5, 500},
		{"nil", nil, 0},
		{"empty", "", 0},
		{"zero", "0", 0},
		{"negative", -1, 0},
		{"garbage", "two cores", 0},
		{"nested map", map[string]any{"limit": "2"}, 0},
	} {
		if got := parseCPUQuantity(tc.raw); got != tc.want {
			t.Errorf("%s: parseCPUQuantity(%#v) = %d, want %d", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestProfileCPUParsesAndInherits(t *testing.T) {
	t.Parallel()

	base, err := parseManagedProfileYAML("services:\n  ollama:\n    cpu: \"2\"\n  worker:\n    cpu: 500m\n")
	if err != nil {
		t.Fatal(err)
	}
	if base.Services["ollama"].MilliCPU != 2000 || base.Services["worker"].MilliCPU != 500 {
		t.Fatalf("parsed cpu = %+v", base.Services)
	}

	child, err := parseManagedProfileYAML("services:\n  ollama:\n    cpu: \"4\"\n    memory: 2Gi\n")
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeServiceSpecs(base.Services, child.Services)
	if merged["ollama"].MilliCPU != 4000 {
		t.Errorf("overridden cpu = %d, want 4000", merged["ollama"].MilliCPU)
	}
	if merged["worker"].MilliCPU != 500 {
		t.Errorf("inherited cpu = %d, want 500", merged["worker"].MilliCPU)
	}
	if merged["ollama"].Memory == 0 {
		t.Error("memory set beside cpu was lost")
	}
}
