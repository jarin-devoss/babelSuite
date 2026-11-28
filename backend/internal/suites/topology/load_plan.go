package topology

import (
	"fmt"
	"strings"

	"github.com/babelsuite/babelsuite/internal/suites/plan"
)

func resolveLoadSpec(raw rawTopologyNode, sourceFiles []SourceFile, env map[string]string) (*LoadSpec, error) {
	target := strings.TrimSpace(raw.Target)
	if target == "" {
		return nil, fmt.Errorf("invalid suite topology: %s must declare target", raw.Variant)
	}

	spec := &LoadSpec{
		Variant:      raw.Variant,
		Target:       target,
		RequestsPerS: raw.RPS,
		ArrivalRate:  raw.ArrivalRate,
		MinUsers:     int(raw.MinUsers),
	}

	planPath := normalizeLoadPlanPath(raw.Plan)
	if planPath == "" {
		plan.ApplyDefaultShape(spec)
		if err := plan.Validate(spec); err != nil {
			return nil, err
		}
		return spec, nil
	}

	spec.PlanPath = planPath
	content, ok := sourceFileContent(sourceFiles, planPath)
	if !ok {
		if len(sourceFiles) == 0 {
			return spec, nil
		}
		return nil, fmt.Errorf("invalid suite topology: traffic plan %q could not be resolved", planPath)
	}
	if len(content) > plan.MaxPlanBytes {
		return nil, fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %d byte safety limit", planPath, plan.MaxPlanBytes)
	}
	return plan.Parse(spec, content, env)
}

func sourceFileContent(sourceFiles []SourceFile, path string) (string, bool) {
	normalized := strings.Trim(strings.TrimSpace(path), "/")
	for _, file := range sourceFiles {
		if strings.Trim(strings.TrimSpace(file.Path), "/") != normalized {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(file.Content), "# Missing example source") {
			return "", false
		}
		return file.Content, true
	}
	return "", false
}

func normalizeLoadPlanPath(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "/")
	trimmed = strings.TrimPrefix(trimmed, "./")
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "traffic/") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "load/") {
		return normalizeSourcePath("traffic", strings.TrimPrefix(trimmed, "load/"))
	}
	return normalizeSourcePath("traffic", trimmed)
}

