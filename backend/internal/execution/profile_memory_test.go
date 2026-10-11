package execution

import "testing"

// Memory ceilings come from a profile as hand-written YAML, so every shape an
// operator might reasonably type has to land on either a usable byte count or
// zero — never a value Docker would reject. Zero means "leave the step on the
// default ceiling".
func TestParseMemoryQuantityAcceptsTheShapesOperatorsWrite(t *testing.T) {
	t.Parallel()

	const gi = int64(1024 * 1024 * 1024)

	for _, tc := range []struct {
		name string
		raw  any
		want int64
	}{
		// The binary suffixes are what the docs and the llm-judge-live profile use.
		{"gibibytes", "2Gi", 2 * gi},
		{"mebibytes", "512Mi", 512 * 1024 * 1024},
		{"kibibytes", "64Ki", 64 * 1024},
		// Decimal suffixes mean powers of ten, not two — the distinction the
		// Kubernetes quantity grammar exists to keep straight.
		{"decimal giga", "2G", 2_000_000_000},
		{"decimal mega", "512M", 512_000_000},
		{"surrounding space is tolerated", "  256Mi  ", 256 * 1024 * 1024},

		// A bare number is a byte count, whichever way YAML decoded it.
		{"plain int", 1048576, int64(1048576)},
		{"int64", int64(2048), int64(2048)},
		{"float", float64(4096), int64(4096)},
		{"quoted byte count", "1048576", int64(1048576)},

		// Everything below means "unset" rather than a limit.
		{"nil", nil, 0},
		{"empty string", "", 0},
		{"whitespace only", "   ", 0},
		{"zero", 0, 0},
		{"negative int", -5, 0},
		{"negative quantity", "-2Gi", 0},
		{"unparseable", "two gigs", 0},
		{"wrong type", true, 0},
		{"nested block", map[string]any{"limit": "2Gi"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseMemoryQuantity(tc.raw); got != tc.want {
				t.Fatalf("parseMemoryQuantity(%#v) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// The value has to survive the whole profile parse, not just the helper: a
// services entry carrying only a memory ceiling still has to be stored, since
// the condition that decides whether to keep an entry used to look at env and
// devices alone.
func TestProfileMemoryReachesTheServiceSpec(t *testing.T) {
	t.Parallel()

	runtime, err := parseManagedProfileYAML(`
name: Local
services:
  ollama:
    memory: 2Gi
  worker:
    env:
      MODE: fast
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	ollama, ok := runtime.Services["ollama"]
	if !ok {
		t.Fatal("a service whose only setting is memory should still be stored")
	}
	if want := int64(2 * 1024 * 1024 * 1024); ollama.Memory != want {
		t.Fatalf("ollama memory = %d, want %d", ollama.Memory, want)
	}

	// A service that sets no ceiling must report zero so the runner keeps its
	// own default rather than reading a stray value.
	if runtime.Services["worker"].Memory != 0 {
		t.Fatalf("worker memory = %d, want 0", runtime.Services["worker"].Memory)
	}
}

// A child profile overrides a parent's ceiling only when it sets a real one,
// matching how devices already merge. Without this a parent's deliberate
// ceiling could be silently dropped by a child that never mentions memory.
func TestMemoryMergeOnlyOverridesWhenSet(t *testing.T) {
	t.Parallel()

	base := map[string]serviceSpec{
		"ollama": {Memory: 2 * 1024 * 1024 * 1024},
		"worker": {Memory: 256 * 1024 * 1024},
	}
	overlay := map[string]serviceSpec{
		"ollama": {Memory: 4 * 1024 * 1024 * 1024},
		"worker": {Env: map[string]string{"MODE": "fast"}},
	}

	merged := mergeServiceSpecs(base, overlay)

	if want := int64(4 * 1024 * 1024 * 1024); merged["ollama"].Memory != want {
		t.Fatalf("overridden memory = %d, want %d", merged["ollama"].Memory, want)
	}
	if want := int64(256 * 1024 * 1024); merged["worker"].Memory != want {
		t.Fatalf("inherited memory = %d, want %d", merged["worker"].Memory, want)
	}
}
