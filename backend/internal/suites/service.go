package suites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/babelsuite/babelsuite/internal/examplefs"
)

type Service struct {
	mu     sync.RWMutex
	suites map[string]Definition
}

func NewService() *Service {
	return &Service{suites: map[string]Definition{}}
}

// NewWorkspaceService loads suites from the local examples workspace.
// Use this in tests and dev tools only — the production server reads suites
// exclusively from the OCI catalog.
func NewWorkspaceService() *Service {
	return &Service{suites: hydrateSuites(loadWorkspaceSuites())}
}

func (s *Service) List() []Definition {
	s.mu.RLock()
	defer s.mu.RUnlock()

	catalog := suiteCatalog(s.suites)
	result := make([]Definition, 0, len(s.suites))
	for _, suite := range catalog {
		result = append(result, cloneDefinition(ResolveDefinitionTopology(suite, catalog)))
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Title) < strings.ToLower(result[j].Title)
	})
	return result
}

func (s *Service) Get(id string) (*Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	suite, ok := s.suites[strings.TrimSpace(id)]
	if !ok {
		return nil, ErrNotFound
	}

	catalog := suiteCatalog(s.suites)
	clone := cloneDefinition(ResolveDefinitionTopology(suite, catalog))
	return &clone, nil
}

// Resolve finds a suite by fuzzy-matching a raw OCI ref against suite IDs and
// repositories, using the same normalisation rules as the frontend.
func (s *Service) Resolve(ref string) (*Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalizedRef := normalizeSuiteRef(ref)
	pathRef := repositorySuitePath(ref)
	if normalizedRef == "" {
		return nil, ErrNotFound
	}

	for _, suite := range s.suites {
		id := strings.TrimSpace(suite.ID)
		if id == normalizedRef || id == pathRef {
			catalog := suiteCatalog(s.suites)
			clone := cloneDefinition(ResolveDefinitionTopology(suite, catalog))
			return &clone, nil
		}
		suiteNorm := normalizeSuiteRef(suite.Repository)
		suitePath := repositorySuitePath(suite.Repository)
		if (suiteNorm != "" && suiteNorm == normalizedRef) || (suitePath != "" && suitePath == pathRef) {
			catalog := suiteCatalog(s.suites)
			clone := cloneDefinition(ResolveDefinitionTopology(suite, catalog))
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

// normalizeSuiteRef strips the digest and tag from a repository ref so that
// refs like "registry.io/team/suite:v1.0" compare equal to "registry.io/team/suite".
func normalizeSuiteRef(ref string) string {
	value := strings.TrimRight(strings.TrimSpace(ref), "/")
	if value == "" {
		return ""
	}
	if i := strings.Index(value, "@"); i >= 0 {
		value = value[:i]
	}
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon > lastSlash {
		value = value[:lastColon]
	}
	return value
}

// repositorySuitePath strips the registry host from a normalized ref so that
// "registry.io/team/suite" and "team/suite" can match.
func repositorySuitePath(ref string) string {
	value := normalizeSuiteRef(ref)
	if value == "" {
		return ""
	}
	firstSlash := strings.Index(value, "/")
	if firstSlash < 0 {
		return value
	}
	head := value[:firstSlash]
	if head == "localhost" || strings.ContainsAny(head, ".:") {
		return value[firstSlash+1:]
	}
	return value
}

func suiteCatalog(items map[string]Definition) []Definition {
	result := make([]Definition, 0, len(items))
	for _, suite := range items {
		result = append(result, suite)
	}
	return result
}


func (s *Service) Register(req RegisterRequest) (Definition, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return Definition{}, fmt.Errorf("suite ID is required")
	}
	if !isValidSuiteID(id) {
		return Definition{}, fmt.Errorf("suite ID must contain only lowercase letters, digits, hyphens, or underscores")
	}
	suiteStar := strings.TrimSpace(req.SuiteStar)
	if suiteStar == "" {
		return Definition{}, fmt.Errorf("suite.star content is required")
	}
	if _, err := parseRawTopology(suiteStar); err != nil {
		return Definition{}, fmt.Errorf("invalid suite topology: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.suites[id]; exists {
		return Definition{}, ErrAlreadyExists
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = humanizeIdentifier(id)
	}
	owner := strings.TrimSpace(req.Owner)
	if owner == "" {
		owner = "Workspace"
	}
	definition := Definition{
		ID:          id,
		Title:       title,
		Repository:  "workspace/" + id,
		Owner:       owner,
		Provider:    "Workspace",
		Version:     "workspace",
		Tags:        []string{"workspace"},
		Description: strings.TrimSpace(req.Description),
		Status:      "Installed",
		SuiteStar:   suiteStar,
		Contracts:   extractLoadContracts(suiteStar),
	}

	_ = persistSuiteToDisk(id, suiteStar)
	s.suites[id] = definition
	return cloneDefinition(definition), nil
}

func isValidSuiteID(id string) bool {
	if id == "" {
		return false
	}
	for _, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func persistSuiteToDisk(id, suiteStar string) error {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid suite id")
	}
	dir := filepath.Join(examplefs.ResolveRoot(), "oci-suites", id)
	// Only update suite.star for suites that already have a proper workspace structure.
	// This prevents transient test registrations from creating incomplete workspace entries.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, "suite.star"), []byte(suiteStar), 0o644)
}


var loadStmtRe = regexp.MustCompile(`(?m)^\s*load\s*\(\s*"([^"]+)"`)

func extractLoadContracts(suiteStar string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, m := range loadStmtRe.FindAllStringSubmatch(suiteStar, -1) {
		module := strings.TrimSpace(m[1])
		if module == "" {
			continue
		}
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = struct{}{}
		result = append(result, module)
	}
	return result
}

func humanizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Workspace"
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == '/' || r == '.'
	})
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, " ")
}
