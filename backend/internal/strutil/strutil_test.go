package strutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  []string
		want   string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"first non-empty", []string{"a", "b", "c"}, "a"},
		{"skip blanks", []string{"", "  ", "x"}, "x"},
		{"whitespace only is blank", []string{"   ", "\t", "ok"}, "ok"},
		{"single value", []string{"hello"}, "hello"},
		{"no args", []string{}, ""},
		{"last is non-empty", []string{"", "", "last"}, "last"},
		{"trims whitespace for check only", []string{"  val  "}, "  val  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FirstNonEmpty(tc.input...)
			if got != tc.want {
				t.Fatalf("FirstNonEmpty(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
