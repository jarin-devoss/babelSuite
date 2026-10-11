package runner

import (
	"encoding/json"
	"testing"
)

// Lua's cjson emits {} for an empty table, so a plugin that found nothing
// sends findings as an object. That is the passing case, and before the
// decoder tolerated it the runner reported a parse error on every clean run.
func TestFindingsDecodeFromEveryShapeAPluginSends(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		body  string
		count int
	}{
		{"empty object from cjson", `{"passed":true,"findings":{}}`, 0},
		{"empty array", `{"passed":true,"findings":[]}`, 0},
		{"null", `{"passed":true,"findings":null}`, 0},
		{"omitted", `{"passed":true}`, 0},
		{"one finding", `{"passed":false,"findings":[{"label":"a","severity":"warn","detail":"d"}]}`, 1},
		{"several findings", `{"passed":false,"findings":[{"label":"a"},{"label":"b"},{"label":"c"}]}`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var result pluginResult
			if err := json.Unmarshal([]byte(tc.body), &result); err != nil {
				t.Fatalf("decode %s: %v", tc.body, err)
			}
			if len(result.Findings) != tc.count {
				t.Fatalf("findings = %d, want %d", len(result.Findings), tc.count)
			}
		})
	}
}

func TestFindingsPreserveFieldsAndPassedFlag(t *testing.T) {
	t.Parallel()

	var result pluginResult
	body := `{"passed":false,"summary":"s","level":"warn",
	          "findings":[{"label":"judge.failed","severity":"critical","detail":"why"}]}`
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Passed {
		t.Fatal("passed should be false")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
	got := result.Findings[0]
	if got.Label != "judge.failed" || got.Severity != "critical" || got.Detail != "why" {
		t.Fatalf("finding fields not preserved: %+v", got)
	}
}

// An object with keys is a genuine plugin mistake rather than cjson's empty
// table, so it should be reported instead of silently read as no findings.
func TestNonEmptyObjectForFindingsIsRejected(t *testing.T) {
	t.Parallel()

	var result pluginResult
	err := json.Unmarshal([]byte(`{"passed":true,"findings":{"label":"oops"}}`), &result)
	if err == nil {
		t.Fatal("expected an error for a non-empty object")
	}
}

func TestMalformedFindingsStillError(t *testing.T) {
	t.Parallel()

	var result pluginResult
	if err := json.Unmarshal([]byte(`{"findings":"a string"}`), &result); err == nil {
		t.Fatal("expected an error for a string")
	}
}
