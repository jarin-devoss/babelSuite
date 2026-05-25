package cachehub

import (
	"context"
	"testing"
	"time"
)

// All tests below operate against a disabled hub (no Redis address supplied).
// The disabled path covers every public method with a real code path and makes
// these tests runnable in any environment without an external dependency.

func disabledHub() *Hub {
	h, _ := New(Options{})
	return h
}

func TestNewWithEmptyAddressReturnsDisabledHub(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if h.Enabled() {
		t.Fatal("hub with no address should be disabled")
	}
}

func TestNewWithUnreachableAddressReturnsError(t *testing.T) {
	t.Parallel()
	_, err := New(Options{Address: "localhost:19999"})
	if err == nil {
		t.Fatal("expected error connecting to unreachable Redis address")
	}
}

func TestEnabledReturnsFalseForNilHub(t *testing.T) {
	t.Parallel()
	var h *Hub
	if h.Enabled() {
		t.Fatal("nil hub should report disabled")
	}
}

func TestCloseOnDisabledHubIsNoOp(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if err := h.Close(); err != nil {
		t.Fatalf("close on disabled hub: %v", err)
	}
}

func TestPingOnDisabledHubIsNoOp(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if err := h.Ping(context.Background()); err != nil {
		t.Fatalf("ping on disabled hub: %v", err)
	}
}

func TestReadJSONOnDisabledHubReturnsMissAndNoError(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	var out map[string]string
	hit, err := h.ReadJSON(context.Background(), "k", &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("disabled hub should never report a cache hit")
	}
}

func TestWriteJSONOnDisabledHubIsNoOp(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	err := h.WriteJSON(context.Background(), "k", map[string]int{"x": 1}, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveOnDisabledHubIsNoOp(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if err := h.Remove(context.Background(), "k"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScopeStampOnDisabledHubReturnsZero(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if got := h.ScopeStamp(context.Background(), "catalogs"); got != "0" {
		t.Fatalf("expected \"0\", got %q", got)
	}
}

func TestBumpScopeOnDisabledHubIsNoOp(t *testing.T) {
	t.Parallel()
	h := disabledHub()
	if err := h.BumpScope(context.Background(), "catalogs"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKeyComposesPartsWithColon(t *testing.T) {
	t.Parallel()
	h := disabledHub()

	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{"catalog", "pkgs"}, "catalog:pkgs"},
		{[]string{"a", "b", "c"}, "a:b:c"},
		{[]string{"  a  ", " b "}, "a:b"},
		{[]string{"a", "", "b"}, "a:b"},
		{[]string{""}, ""},
		{[]string{}, ""},
	}

	for _, tc := range tests {
		got := h.Key(tc.parts...)
		if got != tc.want {
			t.Errorf("Key(%v) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}

func TestComposeAppliesPrefixWhenSet(t *testing.T) {
	t.Parallel()

	withPrefix := &Hub{prefix: "myapp"}
	if got := withPrefix.compose("foo:bar"); got != "myapp:foo:bar" {
		t.Fatalf("expected prefixed key, got %q", got)
	}

	withoutPrefix := &Hub{}
	if got := withoutPrefix.compose("foo:bar"); got != "foo:bar" {
		t.Fatalf("expected bare key, got %q", got)
	}
}

func TestScopeKeyBuildsExpectedKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scope string
		want  string
	}{
		{"catalogs", "scope:catalogs"},
		{"  catalogs  ", "scope:catalogs"},
		{"", "scope:default"},
		{"   ", "scope:default"},
	}

	for _, tc := range tests {
		got := scopeKey(tc.scope)
		if got != tc.want {
			t.Errorf("scopeKey(%q) = %q, want %q", tc.scope, got, tc.want)
		}
	}
}
