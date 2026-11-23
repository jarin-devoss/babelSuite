package profiles

import (
	"errors"
	"fmt"
	"strings"

	"github.com/babelsuite/babelsuite/internal/strutil"
	"time"

	"gopkg.in/yaml.v3"
)

func applyUpsertRequest(record *Record, request UpsertRequest, current []Record, currentID string) error {
	record.Name = strings.TrimSpace(request.Name)
	record.FileName = strings.TrimSpace(request.FileName)
	record.Description = strings.TrimSpace(request.Description)
	record.Scope = strutil.FirstNonEmpty(strings.TrimSpace(request.Scope), scopeFromFileName(request.FileName))
	record.YAML = strings.TrimSpace(request.YAML)
	record.SecretRefs = mergeSecretRefs(ExtractSecretRefsFromYAML(record.YAML), request.SecretRefs)
	record.Default = request.Default && record.Launchable
	record.ExtendsID = strings.TrimSpace(request.ExtendsID)
	record.UpdatedAt = time.Now().UTC()

	if record.ExtendsID == "" && record.Launchable {
		record.ExtendsID = "base"
	}
	if !record.Launchable {
		record.Default = false
	}

	return validateRecord(*record, current, currentID)
}

func validateRecord(record Record, current []Record, currentID string) error {
	if record.Name == "" {
		return errors.New("Profile name is required.")
	}
	if record.FileName == "" {
		return errors.New("Profile file name is required.")
	}
	if !strings.HasSuffix(strings.ToLower(record.FileName), ".yaml") && !strings.HasSuffix(strings.ToLower(record.FileName), ".yml") {
		return errors.New("Profile file names must end with .yaml or .yml.")
	}
	if record.YAML == "" {
		return errors.New("Profile YAML is required.")
	}

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(record.YAML), &parsed); err != nil {
		return fmt.Errorf("Invalid YAML: %v", err)
	}
	if block, ok := parsed["network"].(map[string]any); ok {
		if mode, ok := block["mode"].(string); ok {
			if err := ValidateNetworkMode(mode); err != nil {
				return err
			}
		}
	}

	for _, existing := range current {
		if existing.ID == currentID {
			continue
		}
		if strings.EqualFold(existing.FileName, record.FileName) {
			return errors.New("A profile with that file name already exists for this suite.")
		}
	}

	if record.ExtendsID != "" && record.ExtendsID != "base" && indexOfProfile(current, record.ExtendsID) == -1 && record.ExtendsID != currentID {
		return errors.New("The selected base profile was not found.")
	}

	for _, secretRef := range record.SecretRefs {
		if secretRef.Key == "" || secretRef.Provider == "" || secretRef.Ref == "" {
			return errors.New("Secret references need a key, provider, and reference.")
		}
	}

	return nil
}

// ValidateNetworkMode accepts the one network mode a run has: its own network,
// which every step joins and reaches the others on by name. The shipped modules
// and the suite's gateway address steps that way, so a profile-wide "host" or
// "none" would leave a suite with a service unable to run; refusing them says so
// instead of accepting a value that changes nothing.
func ValidateNetworkMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "execution":
		return nil
	}
	return fmt.Errorf("network.mode %q is not supported: every run gets its own network, which its steps reach each other on by name; remove the key or use \"execution\"", strings.TrimSpace(mode))
}
