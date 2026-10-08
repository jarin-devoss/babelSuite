package main

import (
	"context"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

//go:embed skills/*.md
var skillFS embed.FS

// skillSummary is the frontmatter an agent sees when choosing a playbook.
type skillSummary struct {
	Name        string
	Description string
}

// loadSkills reads every embedded playbook, keyed by its frontmatter name.
func loadSkills() (map[string]string, []skillSummary, error) {
	entries, err := skillFS.ReadDir("skills")
	if err != nil {
		return nil, nil, err
	}

	bodies := make(map[string]string, len(entries))
	summaries := make([]skillSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := skillFS.ReadFile(path.Join("skills", entry.Name()))
		if err != nil {
			return nil, nil, err
		}

		name, description, body := parseSkill(string(raw))
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}
		bodies[name] = body
		summaries = append(summaries, skillSummary{Name: name, Description: description})
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return bodies, summaries, nil
}

// parseSkill splits YAML frontmatter from the markdown body. Only name and
// description are read; everything else is content.
func parseSkill(raw string) (name, description, body string) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", normalized
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return "", "", normalized
	}

	frontmatter := normalized[4 : 4+end]
	body = strings.TrimPrefix(normalized[4+end+4:], "\n")

	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description, body
}

func registerSkillTool(add addTool) error {
	bodies, summaries, err := loadSkills()
	if err != nil {
		return err
	}

	var catalog strings.Builder
	catalog.WriteString("Get a step-by-step playbook for a multi-step BabelSuite task, including the tool call order and the errors that task tends to produce. Read the relevant playbook before starting the work rather than discovering the sequence by trial and error.\n\nAvailable skills:\n")
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		catalog.WriteString(fmt.Sprintf("- %s: %s\n", summary.Name, summary.Description))
		names = append(names, summary.Name)
	}

	add(toolRead,
		mcp.NewTool("get_skill",
			mcp.WithDescription(catalog.String()),
			mcp.WithString("skill",
				mcp.Required(),
				mcp.Description("Which playbook to fetch"),
				mcp.Enum(names...),
			),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("skill")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body, ok := bodies[strings.TrimSpace(name)]
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("unknown skill %q — available: %s", name, strings.Join(names, ", "))), nil
			}
			return mcp.NewToolResultText(body), nil
		},
	)
	return nil
}
