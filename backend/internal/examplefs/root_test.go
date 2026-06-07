package examplefs

import (
	"path/filepath"
	"testing"
)

func TestResolveRootFromRepoUsesEnvOverride(t *testing.T) {
	t.Setenv(RootEnvVar, filepath.Join("custom", "examples"))

	root := ResolveRootFromRepo(filepath.Join("repo", "root"))
	if root == "" {
		t.Fatal("expected non-empty root")
	}
}
