package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registeredToolNames(t *testing.T, readOnly bool) map[string]bool {
	t.Helper()

	s := server.NewMCPServer("test", "0.0.0", server.WithToolCapabilities(true))
	add := toolRegistrar(s, readOnly)
	registerExtendedTools(add, newClient("http://localhost:0", "token"))
	if err := registerSkillTool(add); err != nil {
		t.Fatalf("register skills: %v", err)
	}

	raw := s.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("decode: %v", err)
	}

	names := make(map[string]bool, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestExtendedToolsCoverPreviouslyUnreachableAreas(t *testing.T) {
	names := registeredToolNames(t, false)

	for _, name := range []string{
		"get_suite",
		"list_profile_suites", "get_suite_profiles", "create_suite_profile",
		"update_suite_profile", "set_default_profile", "delete_suite_profile",
		"list_cron_jobs", "get_cron_job", "create_cron_job", "update_cron_job", "delete_cron_job",
		"list_agents", "get_engine_overview", "get_system_health",
		"get_skill",
	} {
		if !names[name] {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

func TestReadOnlyModeWithholdsEveryMutatingTool(t *testing.T) {
	readable := registeredToolNames(t, true)

	// Writes must not merely fail when called — they must not be offered.
	for _, name := range []string{
		"create_suite_profile", "update_suite_profile", "delete_suite_profile",
		"set_default_profile", "create_cron_job", "update_cron_job", "delete_cron_job",
	} {
		if readable[name] {
			t.Errorf("mutating tool %q is exposed in read-only mode", name)
		}
	}

	for _, name := range []string{"get_suite", "list_cron_jobs", "get_system_health", "get_skill"} {
		if !readable[name] {
			t.Errorf("read-only tool %q should still be available", name)
		}
	}
}

func TestSourceFilesArgumentParsesPathContentPairs(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"source_files": []any{
			map[string]any{"path": "tests/smoke.py", "content": "print('hi')"},
			map[string]any{"path": "profiles/local.yaml", "content": "name: Local"},
		},
	}

	files, err := sourceFilesArgument(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0]["path"] != "tests/smoke.py" || files[0]["content"] != "print('hi')" {
		t.Fatalf("unexpected first file: %v", files[0])
	}
}

func TestSourceFilesArgumentRejectsAPathlessEntry(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"source_files": []any{map[string]any{"content": "orphaned"}},
	}

	if _, err := sourceFilesArgument(req); err == nil {
		t.Fatal("expected a file with no path to be rejected")
	}
}

func TestSourceFilesArgumentIsOptional(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	files, err := sourceFilesArgument(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files != nil {
		t.Fatalf("expected no files, got %v", files)
	}
}

func TestEverySkillHasFrontmatterAndBody(t *testing.T) {
	bodies, summaries, err := loadSkills()
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("no skills were embedded")
	}

	for _, summary := range summaries {
		if summary.Name == "" {
			t.Error("a skill is missing its name")
		}
		// The description is what an agent matches on when choosing a
		// playbook, so an empty one makes the skill undiscoverable.
		if !strings.HasPrefix(summary.Description, "Use when") {
			t.Errorf("skill %q description should start with \"Use when\", got %q", summary.Name, summary.Description)
		}
		if strings.TrimSpace(bodies[summary.Name]) == "" {
			t.Errorf("skill %q has an empty body", summary.Name)
		}
		if strings.Contains(bodies[summary.Name], "---\nname:") {
			t.Errorf("skill %q still contains its frontmatter", summary.Name)
		}
	}
}
