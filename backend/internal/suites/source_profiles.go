package suites

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ProfilesFromSourceFiles derives the launchable profile options from a
// package's profiles/*.yaml files, so a suite registered through the API
// advertises the same profiles it would if it had been pulled from a registry.
func ProfilesFromSourceFiles(files []SourceFile) []ProfileOption {
	var doc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Default     bool   `yaml:"default"`
		Runtime     struct {
			ProfileFile string `yaml:"profileFile"`
		} `yaml:"runtime"`
	}

	profiles := make([]ProfileOption, 0)
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if !strings.HasPrefix(path, "profiles/") {
			continue
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			continue
		}
		fileName := strings.TrimPrefix(path, "profiles/")

		doc.Name, doc.Description, doc.Default, doc.Runtime.ProfileFile = "", "", false, ""
		if err := yaml.Unmarshal([]byte(file.Content), &doc); err != nil {
			continue
		}

		label := strings.TrimSpace(doc.Name)
		if label == "" {
			label = humanizeIdentifier(strings.TrimSuffix(strings.TrimSuffix(fileName, ".yaml"), ".yml"))
		}
		profileFile := strings.TrimSpace(doc.Runtime.ProfileFile)
		if profileFile == "" {
			profileFile = fileName
		}

		profiles = append(profiles, ProfileOption{
			FileName:    profileFile,
			Label:       label,
			Description: strings.TrimSpace(doc.Description),
			Default:     doc.Default,
			Content:     file.Content,
		})
	}
	return profiles
}
