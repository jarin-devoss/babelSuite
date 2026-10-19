package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/cachehub"
)

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
}

func intEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationOr(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func buildCacheHub() (*cachehub.Hub, error) {
	address := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if address == "" {
		return &cachehub.Hub{}, nil
	}

	index, err := strconv.Atoi(envOr("REDIS_DB", "0"))
	if err != nil {
		return nil, err
	}

	return cachehub.New(cachehub.Options{
		Address:  address,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       index,
		Prefix:   envOr("REDIS_PREFIX", "babelsuite"),
	})
}

// demoSettingsTemplate is the committed starting point for the platform
// settings file. The file itself is per-machine state — the server rewrites it
// whenever settings change — so it is not in git.
const demoSettingsTemplate = "demo/configuration.demo.yaml"

// platformSettingsPath resolves the settings file like resolveWorkspacePath,
// and on a fresh checkout, where it does not exist yet, creates it from the
// demo template. The new file goes beside the template's demo/ folder — the
// repo root — whether the server runs from there or from backend/, so the
// plugin installers find the same file. An absolute path is never seeded: one
// that is missing was named on purpose, and startup reports it.
func platformSettingsPath(path string) string {
	resolved := resolveWorkspacePath(path)
	if path == "" || filepath.IsAbs(path) {
		return resolved
	}
	if _, err := os.Stat(resolved); err == nil {
		return resolved
	}
	for _, base := range []string{".", ".."} {
		template, err := os.ReadFile(filepath.Join(base, demoSettingsTemplate))
		if err != nil {
			continue
		}
		target := filepath.Join(base, path)
		if err := os.WriteFile(target, template, 0o644); err != nil {
			slog.Warn("could not create the platform settings file from the demo template", "path", target, "error", err)
			return resolved
		}
		slog.Info("created the platform settings file from the demo template", "path", target, "template", filepath.Join(base, demoSettingsTemplate))
		return target
	}
	return resolved
}

func resolveWorkspacePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	parentPath := filepath.Join("..", path)
	if _, err := os.Stat(parentPath); err == nil {
		return parentPath
	}
	return path
}
