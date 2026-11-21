package suites

import (
	"strings"
	"testing"
)

func resolveExports(t *testing.T, exports string) ([]ArtifactExport, error) {
	t.Helper()
	suite := Definition{
		ID:    "export-suite",
		Title: "Export Suite",
		SuiteStar: `load("@babelsuite/runtime", "test")
smoke = test.run(name="smoke", image="busybox", commands=["true"], exports=[` + exports + `])
`,
	}
	topology, err := ResolveTopology(suite, []Definition{suite})
	if err != nil {
		return nil, err
	}
	return topology[0].ArtifactExports, nil
}

func TestExportRejectsAnUnknownFormat(t *testing.T) {
	t.Parallel()
	_, err := resolveExports(t, `{"path": "r.xml", "format": "xunit"}`)
	if err == nil || !strings.Contains(err.Error(), "not supported") || !strings.Contains(err.Error(), "junit, ctrf or cobertura") {
		t.Fatalf("expected a format error naming the supported ones, got %v", err)
	}
}

func TestExportAcceptsTheSupportedFormats(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"junit", "ctrf", "cobertura", ""} {
		if _, err := resolveExports(t, `{"path": "r.xml", "format": "`+format+`"}`); err != nil {
			t.Fatalf("format %q must be accepted: %v", format, err)
		}
	}
}

func TestExportEnforceFalseMarksTheExportNotEnforced(t *testing.T) {
	t.Parallel()
	exports, err := resolveExports(t, `{"path": "a.xml", "format": "junit", "enforce": False}, {"path": "b.json", "format": "ctrf", "enforce": True}, {"path": "c.xml", "format": "junit"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !exports[0].NotEnforced || exports[1].NotEnforced || exports[2].NotEnforced {
		t.Fatalf("only enforce=False must mark an export not enforced: %+v", exports)
	}
}

func TestExportEnforceMustBeABool(t *testing.T) {
	t.Parallel()
	_, err := resolveExports(t, `{"path": "a.xml", "format": "junit", "enforce": "no"}`)
	if err == nil || !strings.Contains(err.Error(), "enforce must be True or False") {
		t.Fatalf("expected a type error, got %v", err)
	}
}

func TestExportEnforceOnlyAppliesToTestReports(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"cobertura", ""} {
		_, err := resolveExports(t, `{"path": "a.xml", "format": "`+format+`", "enforce": False}`)
		if err == nil || !strings.Contains(err.Error(), "applies only to a junit or ctrf export") {
			t.Fatalf("format %q: expected enforce to be refused, got %v", format, err)
		}
	}
}

func TestExportRejectsAnUnknownKey(t *testing.T) {
	t.Parallel()
	_, err := resolveExports(t, `{"path": "a.xml", "fromat": "junit"}`)
	if err == nil || !strings.Contains(err.Error(), `unknown key "fromat"`) {
		t.Fatalf("expected a typo to be named, got %v", err)
	}
}
