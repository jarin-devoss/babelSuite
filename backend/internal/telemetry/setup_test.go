package telemetry

import (
	"testing"
)

func TestNormalizeCollectorEndpoint(t *testing.T) {
	tests := []struct {
		input      string
		wantHost   string
		wantScheme bool
	}{
		{"http://localhost:4318", "localhost:4318", true},
		{"https://collector.example.com:4318", "collector.example.com:4318", false},
		{"localhost:4318", "localhost:4318", false},
		{"collector.internal:4317", "collector.internal:4317", false},
		{"http://collector.internal:4318/", "collector.internal:4318/", true},
	}

	for _, tc := range tests {
		host, scheme := normalizeCollectorEndpoint(tc.input)
		if host != tc.wantHost {
			t.Errorf("normalizeCollectorEndpoint(%q) host = %q, want %q", tc.input, host, tc.wantHost)
		}
		if scheme != tc.wantScheme {
			t.Errorf("normalizeCollectorEndpoint(%q) schemeHint = %v, want %v", tc.input, scheme, tc.wantScheme)
		}
	}
}

func TestShouldSkipTLS(t *testing.T) {
	tests := []struct {
		endpoint   string
		schemeHint bool
		envVal     string
		want       bool
	}{
		{"localhost:4317", false, "", true},
		{"127.0.0.1:4317", false, "", true},
		{"[::1]:4317", false, "", true},
		{"collector.example.com:4317", false, "", false},
		{"collector.example.com:4317", true, "", true},
		{"collector.example.com:4317", false, "true", true},
		{"collector.example.com:4317", false, "false", false},
		{"localhost:4317", false, "false", false},
	}

	for _, tc := range tests {
		t.Run(tc.endpoint+"/hint="+boolStr(tc.schemeHint)+"/env="+tc.envVal, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", tc.envVal)
			}
			got := shouldSkipTLS(tc.endpoint, tc.schemeHint)
			if got != tc.want {
				t.Errorf("shouldSkipTLS(%q, %v) = %v, want %v", tc.endpoint, tc.schemeHint, got, tc.want)
			}
		})
	}
}

func TestReadCollectorHeaders(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    map[string]string
		wantNil bool
	}{
		{
			name:    "empty env",
			env:     "",
			wantNil: true,
		},
		{
			name: "single header",
			env:  "Authorization=Bearer token123",
			want: map[string]string{"Authorization": "Bearer token123"},
		},
		{
			name: "multiple headers",
			env:  "X-Tenant=abc,X-Region=us-east-1",
			want: map[string]string{"X-Tenant": "abc", "X-Region": "us-east-1"},
		},
		{
			name:    "malformed entry no equals",
			env:     "badvalue",
			wantNil: true,
		},
		{
			name: "mixed valid and malformed",
			env:  "X-Api-Key=secret,badentry,X-Tenant=abc",
			want: map[string]string{"X-Api-Key": "secret", "X-Tenant": "abc"},
		},
		{
			name:    "whitespace only",
			env:     "  ,  ,  ",
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", tc.env)
			got := readCollectorHeaders()
			if tc.wantNil {
				if got != nil {
					t.Errorf("readCollectorHeaders() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Errorf("readCollectorHeaders() len = %d, want %d", len(got), len(tc.want))
				return
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("readCollectorHeaders()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestBuildSampler(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		arg     string
	}{
		{"default", "", ""},
		{"always_off", "always_off", ""},
		{"traceidratio", "traceidratio", "0.5"},
		{"traceidratio invalid arg", "traceidratio", "notanumber"},
		{"parentbased_always_off", "parentbased_always_off", ""},
		{"parentbased_traceidratio", "parentbased_traceidratio", "0.25"},
		{"unknown falls to default", "unknown_sampler", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tc.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.arg)
			s := buildSampler()
			if s == nil {
				t.Error("buildSampler() returned nil")
			}
		})
	}
}

func TestParseSamplerRatio(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"0.5", 0.5},
		{"1", 1.0},
		{"0", 0.0},
		{"0.25", 0.25},
		{"", 1.0},
		{"invalid", 1.0},
		{"-0.1", 1.0},
		{"1.5", 1.0},
	}

	for _, tc := range tests {
		got := parseSamplerRatio(tc.input)
		if got != tc.want {
			t.Errorf("parseSamplerRatio(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestPipelineEnabled(t *testing.T) {
	if (*Pipeline)(nil).Enabled() {
		t.Error("nil pipeline should not be enabled")
	}
	if (&Pipeline{}).Enabled() {
		t.Error("empty pipeline should not be enabled")
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
