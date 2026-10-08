package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// toolKind says what a tool does to server state. It drives both read-only
// filtering and the hints clients use to decide what needs confirmation.
type toolKind int

const (
	// toolRead only queries; safe to call without asking.
	toolRead toolKind = iota
	// toolWrite creates or changes something, but destroys nothing.
	toolWrite
	// toolDestructive removes something or tears an environment down.
	toolDestructive
)

// addTool registers a tool, skipping state-changing ones in read-only mode so
// an agent never sees a capability the server will refuse to run.
type addTool func(kind toolKind, tool mcp.Tool, handler server.ToolHandlerFunc)

func toolRegistrar(s *server.MCPServer, readOnly bool) addTool {
	return func(kind toolKind, tool mcp.Tool, handler server.ToolHandlerFunc) {
		if kind != toolRead && readOnly {
			return
		}
		tool.Annotations = mcp.ToolAnnotation{
			ReadOnlyHint:    mcp.ToBoolPtr(kind == toolRead),
			DestructiveHint: mcp.ToBoolPtr(kind == toolDestructive),
			IdempotentHint:  mcp.ToBoolPtr(kind != toolWrite),
			OpenWorldHint:   mcp.ToBoolPtr(true),
		}
		s.AddTool(tool, handler)
	}
}

func registerExtendedTools(add addTool, c *client) {
	registerSuiteTools(add, c)
	registerProfileTools(add, c)
	registerCronJobTools(add, c)
	registerOpsTools(add, c)
}

// ── suites ───────────────────────────────────────────────────────────────────

func registerSuiteTools(add addTool, c *client) {
	add(toolRead,
		mcp.NewTool("get_suite",
			mcp.WithDescription("Get one suite in full: suite.star, resolved topology, profiles, and every source file. Use this before editing a suite so the existing files are not lost."),
			mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/suites/"+id, nil)
		},
	)
}

// sourceFilesArgument parses the shared "source_files" argument used when
// writing a suite package.
func sourceFilesArgument(req mcp.CallToolRequest) ([]map[string]string, error) {
	raw := req.GetArguments()["source_files"]
	if raw == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("source_files is not valid JSON: %w", err)
	}
	var entries []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return nil, fmt.Errorf("source_files must be a list of {path, content} objects: %w", err)
	}

	files := make([]map[string]string, 0, len(entries))
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			return nil, fmt.Errorf("every source file needs a path")
		}
		files = append(files, map[string]string{"path": path, "content": entry.Content})
	}
	return files, nil
}

// ── profiles ─────────────────────────────────────────────────────────────────

func profileBody(req mcp.CallToolRequest) map[string]any {
	body := map[string]any{
		"name":        req.GetString("name", ""),
		"fileName":    req.GetString("file_name", ""),
		"description": req.GetString("description", ""),
		"scope":       req.GetString("scope", ""),
		"yaml":        req.GetString("yaml", ""),
		"default":     req.GetBool("default", false),
	}
	if v := req.GetString("extends_id", ""); v != "" {
		body["extendsId"] = v
	}
	return body
}

func profileArguments(required bool) []mcp.ToolOption {
	nameOpts := []mcp.PropertyOption{mcp.Description("Display name for the profile, e.g. \"Local Debug\"")}
	fileOpts := []mcp.PropertyOption{mcp.Description("Profile file name, e.g. \"local.yaml\"")}
	yamlOpts := []mcp.PropertyOption{mcp.Description("Profile YAML body: env vars, service overrides, and runtime settings")}
	if required {
		nameOpts = append(nameOpts, mcp.Required())
		fileOpts = append(fileOpts, mcp.Required())
		yamlOpts = append(yamlOpts, mcp.Required())
	}

	return []mcp.ToolOption{
		mcp.WithString("name", nameOpts...),
		mcp.WithString("file_name", fileOpts...),
		mcp.WithString("yaml", yamlOpts...),
		mcp.WithString("description", mcp.Description("What this profile is for")),
		mcp.WithString("scope", mcp.Description("Scope label, e.g. Local, CI, Staging, Performance")),
		mcp.WithString("extends_id", mcp.Description("ID of a profile this one inherits from. Left empty, a launchable profile inherits the implicit \"base\" root, which is why created profiles report extendsId \"base\" even when the suite defines no such profile.")),
		mcp.WithBoolean("default", mcp.Description("Make this the profile launched when none is named")),
	}
}

func registerProfileTools(add addTool, c *client) {
	add(toolRead,
		mcp.NewTool("list_profile_suites",
			mcp.WithDescription("List the suites known to the profile service and how many profiles each one has, including suites with none. Start here when you do not know which suite owns a profile."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/profiles/suites", nil)
		},
	)

	add(toolRead,
		mcp.NewTool("get_suite_profiles",
			mcp.WithDescription("Get every profile defined for one suite, including its YAML body, inheritance chain, and which one is the default."),
			mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/profiles/suites/"+id, nil)
		},
	)

	createOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Create a launchable profile for a suite. A profile supplies the env vars and service overrides a run uses, so this is how you vary one suite across local, CI, and staging."),
		mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite the profile belongs to")),
	}, profileArguments(true)...)

	add(toolWrite,
		mcp.NewTool("create_suite_profile", createOpts...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			suiteID, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if _, err := req.RequireString("name"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPost(ctx, c, "/api/v1/profiles/suites/"+suiteID, profileBody(req))
		},
	)

	updateOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Replace an existing suite profile. Fetch it with get_suite_profiles first — omitted fields are cleared, not preserved."),
		mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite the profile belongs to")),
		mcp.WithString("profile_id", mcp.Required(), mcp.Description("Profile ID to replace")),
	}, profileArguments(false)...)

	add(toolWrite,
		mcp.NewTool("update_suite_profile", updateOpts...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			suiteID, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			profileID, err := req.RequireString("profile_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPut(ctx, c, "/api/v1/profiles/suites/"+suiteID+"/"+profileID, profileBody(req))
		},
	)

	add(toolWrite,
		mcp.NewTool("set_default_profile",
			mcp.WithDescription("Make one profile the default for its suite. Launches that name no profile use the default."),
			mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite ID")),
			mcp.WithString("profile_id", mcp.Required(), mcp.Description("Profile to mark as default")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			suiteID, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			profileID, err := req.RequireString("profile_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPost(ctx, c, "/api/v1/profiles/suites/"+suiteID+"/"+profileID+"/default", map[string]string{})
		},
	)

	add(toolDestructive,
		mcp.NewTool("delete_suite_profile",
			mcp.WithDescription("Delete a suite profile permanently. Executions already using it keep running; future launches naming it will fail."),
			mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite ID")),
			mcp.WithString("profile_id", mcp.Required(), mcp.Description("Profile to delete")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			suiteID, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			profileID, err := req.RequireString("profile_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callDelete(ctx, c, "/api/v1/profiles/suites/"+suiteID+"/"+profileID)
		},
	)
}

// ── cron jobs ────────────────────────────────────────────────────────────────

func cronJobBody(req mcp.CallToolRequest) (map[string]any, error) {
	body := map[string]any{
		"name":     req.GetString("name", ""),
		"schedule": req.GetString("schedule", ""),
		"enabled":  req.GetBool("enabled", true),
	}

	if raw := req.GetArguments()["suites"]; raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("suites is not valid JSON: %w", err)
		}
		var targets []struct {
			SuiteID   string `json:"suiteId"`
			Profile   string `json:"profile"`
			BackendID string `json:"backendId"`
		}
		if err := json.Unmarshal(encoded, &targets); err != nil {
			return nil, fmt.Errorf("suites must be a list of {suiteId, profile, backendId} objects: %w", err)
		}
		body["suites"] = targets
	}

	if recipients := req.GetStringSlice("email_recipients", nil); len(recipients) > 0 {
		body["email"] = map[string]any{
			"recipients": recipients,
			"subject":    req.GetString("email_subject", ""),
		}
	}
	if webhook := req.GetString("slack_webhook_url", ""); webhook != "" {
		body["slack"] = map[string]any{"webhookUrl": webhook}
	}
	return body, nil
}

func cronJobArguments(required bool) []mcp.ToolOption {
	nameOpts := []mcp.PropertyOption{mcp.Description("Human-readable job name")}
	scheduleOpts := []mcp.PropertyOption{mcp.Description("Cron expression, e.g. \"0 2 * * *\" for 02:00 daily")}
	if required {
		nameOpts = append(nameOpts, mcp.Required())
		scheduleOpts = append(scheduleOpts, mcp.Required())
	}

	return []mcp.ToolOption{
		mcp.WithString("name", nameOpts...),
		mcp.WithString("schedule", scheduleOpts...),
		mcp.WithBoolean("enabled", mcp.Description("Whether the schedule is active (default true)")),
		mcp.WithArray("suites",
			mcp.Description("Suites to launch on each tick, as objects with suiteId, profile, and backendId"),
			mcp.Items(map[string]any{"type": "object", "properties": map[string]any{
				"suiteId":   map[string]any{"type": "string", "description": "Suite to launch"},
				"profile":   map[string]any{"type": "string", "description": "Profile file name, e.g. local.yaml"},
				"backendId": map[string]any{"type": "string", "description": "Backend to run on, e.g. local-docker"},
			}}),
		),
		mcp.WithArray("email_recipients", mcp.Description("Email addresses notified when the job finishes"), mcp.WithStringItems()),
		mcp.WithString("email_subject", mcp.Description("Subject line for the notification email")),
		mcp.WithString("slack_webhook_url", mcp.Description("Slack incoming webhook posted to when the job finishes")),
	}
}

func registerCronJobTools(add addTool, c *client) {
	add(toolRead,
		mcp.NewTool("list_cron_jobs",
			mcp.WithDescription("List scheduled suite runs with their cron expression, target suites, last result, and next run time."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/cron-jobs", nil)
		},
	)

	add(toolRead,
		mcp.NewTool("get_cron_job",
			mcp.WithDescription("Get one scheduled job in full, including its last error and next scheduled run."),
			mcp.WithString("cron_job_id", mcp.Required(), mcp.Description("Cron job ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("cron_job_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/cron-jobs/"+id, nil)
		},
	)

	createOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Schedule one or more suites to run on a cron expression, with optional email and Slack notifications."),
	}, cronJobArguments(true)...)

	add(toolWrite,
		mcp.NewTool("create_cron_job", createOpts...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if _, err := req.RequireString("name"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if _, err := req.RequireString("schedule"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body, err := cronJobBody(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPost(ctx, c, "/api/v1/cron-jobs", body)
		},
	)

	updateOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Replace a scheduled job. Fetch it with get_cron_job first — omitted fields are cleared, not preserved."),
		mcp.WithString("cron_job_id", mcp.Required(), mcp.Description("Cron job to replace")),
	}, cronJobArguments(false)...)

	add(toolWrite,
		mcp.NewTool("update_cron_job", updateOpts...),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("cron_job_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body, err := cronJobBody(req)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPut(ctx, c, "/api/v1/cron-jobs/"+id, body)
		},
	)

	add(toolDestructive,
		mcp.NewTool("delete_cron_job",
			mcp.WithDescription("Delete a scheduled job permanently. Runs it already started are unaffected."),
			mcp.WithString("cron_job_id", mcp.Required(), mcp.Description("Cron job to delete")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("cron_job_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callDelete(ctx, c, "/api/v1/cron-jobs/"+id)
		},
	)
}

// ── operations ───────────────────────────────────────────────────────────────

func registerOpsTools(add addTool, c *client) {
	add(toolRead,
		mcp.NewTool("list_agents",
			mcp.WithDescription("List registered remote execution agents with their health and last heartbeat. Use this when a launch needs a backend other than the local one."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/agents", nil)
		},
	)

	add(toolRead,
		mcp.NewTool("get_engine_overview",
			mcp.WithDescription("Get engine-wide execution counters: queued, running, and completed steps across every active execution."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/engine/overview", nil)
		},
	)

	add(toolRead,
		mcp.NewTool("get_system_health",
			mcp.WithDescription("Check control plane readiness: datastore, cache, and runner subsystems. Run this first when calls fail for reasons that look environmental."),
			mcp.WithString("subsystem", mcp.Description("Check a single subsystem instead of all of them")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if subsystem := strings.TrimSpace(req.GetString("subsystem", "")); subsystem != "" {
				return callGet(ctx, c, "/api/v1/system/readyz/"+subsystem, nil)
			}
			return callGet(ctx, c, "/api/v1/system/readyz", nil)
		},
	)
}
