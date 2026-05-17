package app

import (
	"context"
	"strings"

	"github.com/babelsuite/babelsuite/internal/catalog"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// catalogSuiteReader lists suites from the OCI catalog so that all pages
// (profiles, execution dialog) agree on which suites exist.  For Get/Resolve
// it prefers the workspace reader (which has the full suite.star content
// required for actual execution) and falls back to catalog metadata only.
type catalogSuiteReader struct {
	catalog   catalog.Reader
	workspace suiteReaderFull
}

type suiteReaderFull interface {
	List() []suites.Definition
	Get(id string) (*suites.Definition, error)
	Resolve(ref string) (*suites.Definition, error)
}

func (r *catalogSuiteReader) List() []suites.Definition {
	packages, err := r.catalog.ListPackages(context.Background())
	if err != nil {
		return r.workspace.List()
	}

	seen := make(map[string]struct{}, len(packages))
	result := make([]suites.Definition, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Kind != "suite" {
			continue
		}
		seen[pkg.ID] = struct{}{}
		if def, err := r.workspace.Get(pkg.ID); err == nil {
			result = append(result, *def)
			continue
		}
		result = append(result, suites.Definition{
			ID:          pkg.ID,
			Title:       pkg.Title,
			Description: pkg.Description,
			Repository:  pkg.Repository,
			Provider:    pkg.Provider,
			Status:      pkg.Status,
		})
	}
	for _, def := range r.workspace.List() {
		if _, ok := seen[def.ID]; !ok {
			result = append(result, def)
		}
	}
	return result
}

func (r *catalogSuiteReader) Get(id string) (*suites.Definition, error) {
	if def, err := r.workspace.Get(strings.TrimSpace(id)); err == nil {
		return def, nil
	}
	pkg, err := r.catalog.GetPackage(context.Background(), strings.TrimSpace(id))
	if err != nil {
		return nil, suites.ErrNotFound
	}
	def := suites.Definition{
		ID:          pkg.ID,
		Title:       pkg.Title,
		Description: pkg.Description,
		Repository:  pkg.Repository,
		Provider:    pkg.Provider,
		Status:      pkg.Status,
	}
	return &def, nil
}

func (r *catalogSuiteReader) Resolve(ref string) (*suites.Definition, error) {
	if def, err := r.workspace.Resolve(ref); err == nil {
		return def, nil
	}
	return r.Get(ref)
}
