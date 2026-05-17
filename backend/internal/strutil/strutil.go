package strutil

import "strings"

// FirstNonEmpty returns the first non-blank string from the given values
// (blank means empty or whitespace-only), or an empty string if all values
// are blank.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
