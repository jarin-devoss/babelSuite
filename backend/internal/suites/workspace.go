package suites

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/babelsuite/babelsuite/internal/examplefs"
	"gopkg.in/yaml.v3"
)

type workspaceProfileDocument struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Default     bool   `yaml:"default"`
	Runtime     struct {
		Suite       string `yaml:"suite"`
		Repository  string `yaml:"repository"`
		ProfileFile string `yaml:"profileFile"`
	} `yaml:"runtime"`
	Modules []string `yaml:"modules"`
}

type workspaceMetadataDocument struct {
	Name        string            `yaml:"name"`
	Title       string            `yaml:"title"`
	Description string            `yaml:"description"`
	Owner       string            `yaml:"owner"`
	Labels      map[string]string `yaml:"labels"`
	Tags        []string          `yaml:"tags"`
}

func loadWorkspaceSuites() map[string]Definition {
	root := filepath.Join(examplefs.ResolveRoot(), "oci-suites")
	entries, err := os.ReadDir(root)
	if err != nil {
		return map[string]Definition{}
	}

	result := make(map[string]Definition, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		definition, ok := loadWorkspaceSuite(root, entry.Name())
		if !ok {
			continue
		}
		result[definition.ID] = definition
	}
	return result
}

func loadWorkspaceSuite(root, suiteID string) (Definition, bool) {
	base := filepath.Join(root, suiteID)
	suiteStarBytes, err := os.ReadFile(filepath.Join(base, "suite.star"))
	if err != nil {
		return Definition{}, false
	}

	profiles, repository, modules := loadWorkspaceProfiles(base)
	title, description := loadWorkspaceReadme(base, suiteID)
	metadata := loadWorkspaceMetadata(base)
	title = firstWorkspaceNonEmpty(strings.TrimSpace(metadata.Title), title)
	description = firstWorkspaceNonEmpty(strings.TrimSpace(metadata.Description), description)
	rootSources := loadWorkspaceRootSourceFiles(base)
	if repository == "" {
		repository = "workspace/" + suiteID
	}
	owner := ownerFromRepository(repository)
	if value := firstWorkspaceNonEmpty(strings.TrimSpace(metadata.Owner), strings.TrimSpace(metadata.Labels["owner"])); value != "" {
		owner = value
	}

	definition := Definition{
		ID:          suiteID,
		Title:       title,
		Repository:  repository,
		Owner:       owner,
		Provider:    "Workspace",
		Version:     "workspace",
		Labels:      cloneStringMap(metadata.Labels),
		Tags:        mergeWorkspaceTags(metadata.Tags),
		Description: description,
		Modules:     modules,
		Status:      "Installed",
		Score:       0,
		PullCommand: workspaceRunCommand(repository),
		ForkCommand: workspaceForkCommand(repository),
		SuiteStar:   string(suiteStarBytes),
		Profiles:    profiles,
		Folders:     FoldersFromFiles(readWorkspaceFiles(base, allFolders(base)...)),
		SeedSources: rootSources,
		Contracts:   extractLoadContracts(string(suiteStarBytes)),
		APISurfaces: loadWorkspaceAPISurfaces(base),
	}

	return definition, true
}

func loadWorkspaceMetadata(base string) workspaceMetadataDocument {
	path := filepath.Join(base, "metadata.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return workspaceMetadataDocument{}
	}

	var metadata workspaceMetadataDocument
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return workspaceMetadataDocument{}
	}
	if metadata.Labels == nil {
		metadata.Labels = map[string]string{}
	}
	return metadata
}

func loadWorkspaceProfiles(base string) ([]ProfileOption, string, []string) {
	files := readWorkspaceFiles(base, "profiles")
	profiles := ProfilesFromFiles(files)

	// What only a directory suite declares: the repository and the modules its
	// profiles name.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	moduleSet := map[string]struct{}{}
	repository := ""
	for _, path := range paths {
		var profile workspaceProfileDocument
		if err := yaml.Unmarshal([]byte(files[path]), &profile); err != nil {
			continue
		}
		if repository == "" {
			repository = strings.TrimSpace(profile.Runtime.Repository)
		}
		for _, module := range profile.Modules {
			if trimmed := strings.TrimSpace(module); trimmed != "" {
				moduleSet[trimmed] = struct{}{}
			}
		}
	}
	modules := make([]string, 0, len(moduleSet))
	for module := range moduleSet {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	return profiles, repository, modules
}

func loadWorkspaceReadme(base, suiteID string) (string, string) {
	data, err := os.ReadFile(filepath.Join(base, "README.md"))
	if err != nil {
		title := humanizeIdentifier(suiteID)
		return title, title + " installed from the local workspace."
	}

	lines := strings.Split(string(data), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}
	title := humanizeIdentifier(suiteID)
	if len(parts) > 0 {
		title = parts[0]
	}
	description := title + " installed from the local workspace."
	if len(parts) > 1 {
		description = parts[1]
	}
	return title, description
}

// allFolders names the folders of a suite directory.
func allFolders(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func loadWorkspaceRootSourceFiles(base string) []SourceFile {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	files := make([]SourceFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.EqualFold(name, "suite.star") || strings.EqualFold(name, "README.md") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		files = append(files, SourceFile{
			Path:     name,
			Language: DetectSourceLanguage(name),
			Content:  string(content),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

func folderRole(name string) string {
	switch strings.TrimSpace(name) {
	case "profiles":
		return "Configuration"
	case "api":
		return "Contracts"
	case "mock":
		return "Mocking"
	case "services":
		return "Infrastructure"
	case "tasks":
		return "Operations"
	case "tests":
		return "Verification"
	case "resources":
		return "Assets"
	case "scripts":
		return "Operations"
	case "traffic":
		return "Performance"
	case "scenarios":
		return "Verification"
	case "fixtures":
		return "Data"
	case "certs":
		return "Assets"
	default:
		return humanizeIdentifier(name)
	}
}

func folderDescription(name string) string {
	switch strings.TrimSpace(name) {
	case "profiles":
		return "Environment-specific runtime overrides and launch profiles."
	case "api":
		return "Contracts and schemas shipped with the suite."
	case "mock":
		return "Mock schemas, metadata, and compatibility assets."
	case "services":
		return "Background infrastructure assets and compatibility service files."
	case "tasks":
		return "Short-lived setup, seed, and migration jobs."
	case "tests":
		return "Verification, smoke, regression, and browser assertions."
	case "resources":
		return "Passive static assets such as certificates, datasets, and large blobs."
	case "scripts":
		return "Legacy short-lived setup and migration jobs."
	case "traffic":
		return "Native traffic plans and protocol-safe workload definitions."
	case "scenarios":
		return "Legacy smoke, regression, and attack-path tests."
	case "fixtures":
		return "Static seed data and sample payloads."
	case "certs":
		return "Static certificates and supporting trust material."
	default:
		return humanizeIdentifier(name) + " files."
	}
}

func ownerFromRepository(repository string) string {
	trimmed := strings.TrimSpace(repository)
	if trimmed == "" {
		return "Workspace"
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return "Workspace"
	}
	return humanizeIdentifier(parts[len(parts)-2])
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	return ext == ".yaml" || ext == ".yml"
}

func firstWorkspaceNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func workspaceLabelFromFileName(fileName string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(fileName), filepath.Ext(strings.TrimSpace(fileName)))
	return humanizeIdentifier(trimmed)
}

func workspaceRunCommand(repository string) string {
	version := "workspace"
	return "babelctl run " + strings.TrimSpace(repository) + ":" + version
}

func workspaceForkCommand(repository string) string {
	version := "workspace"
	name := strings.TrimSpace(repository)
	if parts := strings.Split(strings.Trim(name, "/"), "/"); len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	return "babelctl fork " + strings.TrimSpace(repository) + ":" + version + " ./" + name
}

func mergeWorkspaceTags(tags []string) []string {
	seen := map[string]struct{}{
		"workspace": {},
	}
	merged := []string{"workspace"}
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		merged = append(merged, trimmed)
	}
	sort.Strings(merged[1:])
	return merged
}

func loadWorkspaceAPISurfaces(base string) []APISurface {
	return SurfacesFromFiles(readWorkspaceFiles(base, "mock", "api"))
}

// readWorkspaceFiles reads the files under the named folders of a suite
// directory into the map the package-file parsers take (path relative to the
// suite, with forward slashes).
func readWorkspaceFiles(base string, folders ...string) map[string]string {
	files := map[string]string{}
	for _, folder := range folders {
		root := filepath.Join(base, folder)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // a folder that cannot be read has no files
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil //nolint:nilerr // skip a file that cannot be read
			}
			rel, relErr := filepath.Rel(base, path)
			if relErr != nil {
				return nil //nolint:nilerr // skip a path that cannot be made relative
			}
			files[filepath.ToSlash(rel)] = string(data)
			return nil
		})
	}
	return files
}

// NewWorkspaceService loads suites from the local examples directory.
// Only for tests — the production server reads suites from the OCI catalog.
func NewWorkspaceService() *Service {
	return &Service{suites: hydrateSuites(loadWorkspaceSuites(), workspaceSourceFileLoader)}
}

func workspaceSourceFileLoader(suiteID, path string) (string, bool) {
	target := examplefs.SuiteFilePath(suiteID, path)
	data, err := os.ReadFile(target)
	if err != nil {
		return "", false
	}
	return string(data), true
}
