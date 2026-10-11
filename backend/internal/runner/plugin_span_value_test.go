package runner

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Attribute values come from plugin JSON, so they are UTF-8 and need not be
// ASCII. Capping them on a byte index alone can cut a rune in half and export
// an attribute that is not valid UTF-8 at all — worse than a value that is
// merely too long, because the damage is invisible until a trace backend
// rejects or mangles it.
func TestPluginSpanValueTruncatesOnARuneBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
	}{
		{"ascii under the cap", "judge scored 0.67"},
		{"ascii exactly at the cap", strings.Repeat("a", maxPluginSpanValueLength)},
		{"ascii over the cap", strings.Repeat("a", maxPluginSpanValueLength*2)},
		// The byte at the cap index is the lead byte of a 3-byte rune, so a
		// plain slice keeps one third of it.
		{"multi-byte rune straddles the cap", strings.Repeat("a", maxPluginSpanValueLength-1) + "€tail"},
		// Every byte after the cap is a continuation byte, forcing the walk
		// back to cover more than one byte.
		{"4-byte rune straddles the cap", strings.Repeat("a", maxPluginSpanValueLength-2) + "😀tail"},
		{"all multi-byte", strings.Repeat("→", maxPluginSpanValueLength)},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncatePluginSpanValue(tc.value)

			if !utf8.ValidString(got) {
				t.Fatalf("result is not valid UTF-8: % x", got)
			}
			if len(got) > maxPluginSpanValueLength {
				t.Fatalf("result is %d bytes, over the %d cap", len(got), maxPluginSpanValueLength)
			}
			if !strings.HasPrefix(tc.value, got) {
				t.Fatalf("result is not a prefix of the input: %q", got)
			}
			// Nothing should be dropped from a value that already fits.
			if len(tc.value) <= maxPluginSpanValueLength && got != tc.value {
				t.Fatalf("a value within the cap was modified: %q -> %q", tc.value, got)
			}
			// Truncation should cost at most the width of one rune, so the cap
			// is still doing its job rather than discarding half the value.
			if len(tc.value) > maxPluginSpanValueLength && len(got) < maxPluginSpanValueLength-utf8.UTFMax {
				t.Fatalf("truncated too aggressively: %d bytes from %d", len(got), len(tc.value))
			}
		})
	}
}
