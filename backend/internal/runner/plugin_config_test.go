package runner

import (
	"reflect"
	"testing"
)

func TestMergePluginConfig(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env    map[string]string
		kwargs map[string]any
		want   map[string]any
	}{
		// electronics-lab: the profile sets sim_url, the wrapper forwards "".
		"empty kwarg keeps the profile value": {
			env:    map[string]string{"sim_url": "http://spice-service:9080"},
			kwargs: map[string]any{"sim_url": "", "netlist": "R1 1 2 1k"},
			want:   map[string]any{"sim_url": "http://spice-service:9080", "netlist": "R1 1 2 1k"},
		},
		"None kwarg keeps the profile value": {
			env:    map[string]string{"max_rise_ms": "3.0"},
			kwargs: map[string]any{"max_rise_ms": nil},
			want:   map[string]any{"max_rise_ms": "3.0"},
		},
		"a set kwarg overrides the profile": {
			env:    map[string]string{"severity": "warn"},
			kwargs: map[string]any{"severity": "critical"},
			want:   map[string]any{"severity": "critical"},
		},
		"an unset kwarg with no profile value is still sent": {
			env:    nil,
			kwargs: map[string]any{"api_key": ""},
			want:   map[string]any{"api_key": ""},
		},
		"non-string values override": {
			env:    map[string]string{"threshold": "0.5"},
			kwargs: map[string]any{"threshold": 0.0},
			want:   map[string]any{"threshold": 0.0},
		},
	} {
		if got := mergePluginConfig(tc.env, tc.kwargs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got  %#v\n want %#v", name, got, tc.want)
		}
	}
}
