package execution

import (
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// A step pulled in through a suite dependency carries its own package, and its
// file= names a script that exists there rather than in the suite being run.
// Resolving against the parent only meant every dependency step's file=
// resolved to nothing, which the runner then reported as a missing file even
// though the script was present in the dependency.
func TestNodeFileContentResolvesAgainstTheOwningPackage(t *testing.T) {
	t.Parallel()

	suite := &suites.Definition{
		ID: "composite-readiness",
		SourceFiles: []suites.SourceFile{
			{Path: "tests/readiness_smoke.py", Content: "print('parent')"},
			// The parent has a file of the same name, so a test that passed by
			// accident would be indistinguishable from one that works.
			{Path: "tasks/seed_realms.sh", Content: "echo parent-copy"},
		},
		ResolvedDependencies: []suites.ResolvedDependency{
			{
				SuiteID: "identity-broker",
				SourceFiles: []suites.SourceFile{
					{Path: "tasks/seed_realms.sh", Content: "echo dependency-copy"},
				},
			},
			{
				SuiteID: "payment-suite",
				SourceFiles: []suites.SourceFile{
					{Path: "tasks/seed.sh", Content: "echo payment-seed"},
				},
			},
		},
	}

	for _, tc := range []struct {
		name string
		node topologyNode
		want string
	}{
		{
			name: "parent node reads the parent package",
			node: topologyNode{File: "readiness_smoke.py", SourceSuiteID: "composite-readiness"},
			want: "print('parent')",
		},
		{
			name: "node with no owner falls back to the parent",
			node: topologyNode{File: "readiness_smoke.py"},
			want: "print('parent')",
		},
		{
			name: "dependency node reads the dependency package, not the parent",
			node: topologyNode{File: "tasks/seed_realms.sh", SourceSuiteID: "identity-broker"},
			want: "echo dependency-copy",
		},
		{
			name: "a second dependency reads its own package",
			node: topologyNode{File: "seed.sh", SourceSuiteID: "payment-suite"},
			want: "echo payment-seed",
		},
		{
			name: "unknown owner falls back to the parent rather than resolving nothing",
			node: topologyNode{File: "readiness_smoke.py", SourceSuiteID: "never-resolved"},
			want: "print('parent')",
		},
		{
			name: "a file absent everywhere still resolves to nothing",
			node: topologyNode{File: "absent.py", SourceSuiteID: "identity-broker"},
			want: "",
		},
		{
			name: "no file means no content",
			node: topologyNode{SourceSuiteID: "identity-broker"},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveNodeFileContent(tc.node, suite); got != tc.want {
				t.Fatalf("resolveNodeFileContent(%+v) = %q, want %q", tc.node, got, tc.want)
			}
		})
	}
}

// A dependency whose package did not come through carries no files, and the
// lookup must not treat that as authoritative — otherwise a node that merely
// names an unresolved dependency stops finding anything at all.
func TestNodeSourceFilesIgnoresADependencyWithNoPackage(t *testing.T) {
	t.Parallel()

	suite := &suites.Definition{
		ID:          "parent",
		SourceFiles: []suites.SourceFile{{Path: "tasks/x.sh", Content: "echo parent"}},
		ResolvedDependencies: []suites.ResolvedDependency{
			{SuiteID: "empty-dep"},
		},
	}

	node := topologyNode{File: "x.sh", SourceSuiteID: "empty-dep"}
	if got := resolveNodeFileContent(node, suite); got != "echo parent" {
		t.Fatalf("expected the parent package to be searched, got %q", got)
	}
}
