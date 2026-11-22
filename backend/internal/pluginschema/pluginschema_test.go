package pluginschema

import (
	"strings"
	"testing"
)

const sample = `
target:   string
max_lag:  int & >=0 | *1000
mode:     "BACKWARD" | "FORWARD" | *"BACKWARD"
timeout?: number & >0
`

func TestValidateAcceptsAConfigThatFitsTheSchema(t *testing.T) {
	t.Parallel()
	if err := Validate(sample, map[string]any{"target": "http://x", "max_lag": 5, "mode": "FORWARD", "timeout": 2.5}); err != nil {
		t.Fatalf("expected a valid config, got %v", err)
	}
}

func TestValidateAppliesDefaultsAndLeavesOptionalFieldsOut(t *testing.T) {
	t.Parallel()
	if err := Validate(sample, map[string]any{"target": "http://x"}); err != nil {
		t.Fatalf("defaults must fill in max_lag and mode: %v", err)
	}
}

func TestValidateNamesAMissingRequiredField(t *testing.T) {
	t.Parallel()
	err := Validate(sample, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "target is required") {
		t.Fatalf("expected the missing field to be named, got %v", err)
	}
}

func TestValidateNamesTheFieldAndTheValueThatBreaksIt(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		config map[string]any
		want   string
	}{
		"out of range": {map[string]any{"target": "x", "max_lag": -1}, "max_lag"},
		"wrong type":   {map[string]any{"target": "x", "max_lag": "5"}, "max_lag"},
		"not allowed":  {map[string]any{"target": "x", "mode": "SIDEWAYS"}, `mode: conflicting values "BACKWARD" and "SIDEWAYS"`},
		"optional bad": {map[string]any{"target": "x", "timeout": 0}, "timeout"},
	}
	for name, tc := range cases {
		err := Validate(sample, tc.config)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error containing %q, got %v", name, tc.want, err)
		}
		if strings.Contains(err.Error(), "errors in empty disjunction") {
			t.Fatalf("%s: the message must name the field, not CUE's disjunction header: %v", name, err)
		}
	}
}

func TestValidateAllowsSettingsTheSchemaDoesNotMention(t *testing.T) {
	t.Parallel()
	// The step's config also carries the profile's environment.
	if err := Validate(sample, map[string]any{"target": "x", "HOME": "/root", "SIM_URL": "http://sim"}); err != nil {
		t.Fatalf("unmentioned settings must pass: %v", err)
	}
}

func TestValidateReportsASchemaThatIsNotCUE(t *testing.T) {
	t.Parallel()
	if err := Validate("target: [", map[string]any{}); err == nil || !strings.Contains(err.Error(), "not valid CUE") {
		t.Fatalf("expected a schema error, got %v", err)
	}
	if err := Check("target: ["); err == nil {
		t.Fatal("Check must reject invalid CUE")
	}
	if err := Check(sample); err != nil {
		t.Fatalf("Check must accept valid CUE: %v", err)
	}
}

func TestValidateIsSafeToCallFromManySteps(t *testing.T) {
	t.Parallel()
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() { done <- Validate(sample, map[string]any{"target": "x", "max_lag": 3}) }()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent validation failed: %v", err)
		}
	}
}
