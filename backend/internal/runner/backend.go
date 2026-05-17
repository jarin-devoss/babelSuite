package runner

import "github.com/babelsuite/babelsuite/internal/strutil"

const AutoBackend = "auto"

func normalizeBackendConfig(config BackendConfig, fallbackID, fallbackLabel, fallbackKind string) BackendConfig {
	config.ID = strutil.FirstNonEmpty(config.ID, fallbackID)
	config.Label = strutil.FirstNonEmpty(config.Label, fallbackLabel)
	config.Kind = strutil.FirstNonEmpty(config.Kind, fallbackKind)
	return config
}

