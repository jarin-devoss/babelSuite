package profiles

import (
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
	"gopkg.in/yaml.v3"
)

// seedSuiteProfiles builds the initial profile records for a suite from the
// profiles already parsed out of its OCI layer (definition.Profiles) using
// the raw YAML content stored in definition.SourceFiles.
func seedSuiteProfiles(definition suites.Definition) []Record {
	records := make([]Record, 0, len(definition.Profiles))
	for _, profile := range definition.Profiles {
		content := profileYAMLFromSourceFiles(definition.SourceFiles, profile.FileName)
		scope := scopeFromFileName(profile.FileName)
		records = append(records, Record{
			ID:          profileIDFromFileName(profile.FileName),
			Name:        strutil.FirstNonEmpty(strings.TrimSpace(profile.Label), labelFromFileName(profile.FileName)),
			FileName:    profile.FileName,
			Description: strings.TrimSpace(profile.Description),
			Scope:       scope,
			YAML:        content,
			SecretRefs:  ExtractSecretRefsFromYAML(content),
			ExtendsID:   extendsIDFromYAML(content),
			Default:     profile.Default,
			Launchable:  scope != "Base",
			UpdatedAt:   time.Now().UTC(),
		})
	}
	normalizeRecords(records)
	return records
}

func extendsIDFromYAML(content string) string {
	var doc struct {
		ExtendsID string `yaml:"extendsId"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.ExtendsID)
}

func profileYAMLFromSourceFiles(sourceFiles []suites.SourceFile, fileName string) string {
	target := "profiles/" + strings.TrimSpace(fileName)
	for _, file := range sourceFiles {
		if file.Path == target {
			return file.Content
		}
	}
	return ""
}
