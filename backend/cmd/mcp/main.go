// BabelSuite MCP server — exposes suite orchestration capabilities to AI agents.
// Configure via env vars:
//
//	BABELSUITE_URL      control plane base URL (default: http://localhost:8090)
//	BABELSUITE_TOKEN    JWT bearer token
//	BABELSUITE_EMAIL    email for auto sign-in on startup (if TOKEN is unset)
//	BABELSUITE_PASSWORD password for auto sign-in on startup
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	baseURL := envOr("BABELSUITE_URL", "http://localhost:8090")
	token := os.Getenv("BABELSUITE_TOKEN")

	c := newClient(baseURL, token)

	// Auto sign-in if credentials provided and no token set.
	startCtx := context.Background()
	if token == "" {
		email := os.Getenv("BABELSUITE_EMAIL")
		password := os.Getenv("BABELSUITE_PASSWORD")
		if email != "" && password != "" {
			tok, err := c.signIn(startCtx, email, password)
			if err != nil {
				fmt.Fprintf(os.Stderr, "babelsuite-mcp: auto sign-in failed: %v\n", err)
			} else {
				c.token = tok
			}
		}
	}

	s := server.NewMCPServer(
		"BabelSuite",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// ── Auth ──────────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("sign_in",
			mcp.WithDescription("Sign in to BabelSuite and obtain a JWT token. Subsequent tool calls use the token automatically."),
			mcp.WithString("email", mcp.Required(), mcp.Description("User email address")),
			mcp.WithString("password", mcp.Required(), mcp.Description("User password")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			email, err := req.RequireString("email")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			password, err := req.RequireString("password")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			tok, err := c.signIn(ctx, email, password)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			c.token = tok
			return mcp.NewToolResultText(fmt.Sprintf(`{"token":%q,"note":"Token stored — subsequent calls use it automatically."}`, tok)), nil
		},
	)

	// ── Suites ────────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_suites",
			mcp.WithDescription("List all available test suites with their profiles and backend options, ready to launch."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/executions/launch-suites", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("create_suite",
			mcp.WithDescription("Create a new test suite from a Starlark suite.star definition. The suite is persisted to the workspace and available immediately."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Unique suite ID (lowercase letters, digits, hyphens, underscores only)")),
			mcp.WithString("suite_star", mcp.Required(), mcp.Description("Starlark suite.star content defining the topology (services, tasks, tests, traffic)")),
			mcp.WithString("title", mcp.Description("Human-readable suite title. Defaults to a humanized version of the ID.")),
			mcp.WithString("description", mcp.Description("Short description of what the suite tests.")),
			mcp.WithString("owner", mcp.Description("Team or person that owns the suite.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			suiteStar, err := req.RequireString("suite_star")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body := map[string]string{
				"id":       id,
				"suiteStar": suiteStar,
			}
			if v := req.GetString("title", ""); v != "" {
				body["title"] = v
			}
			if v := req.GetString("description", ""); v != "" {
				body["description"] = v
			}
			if v := req.GetString("owner", ""); v != "" {
				body["owner"] = v
			}
			return callPost(ctx, c, "/api/v1/suites", body)
		},
	)

	s.AddTool(
		mcp.NewTool("resolve_suite_ref",
			mcp.WithDescription("Resolve an OCI reference (e.g. 'payment-suite' or 'ghcr.io/org/repo:tag') to a full suite definition."),
			mcp.WithString("ref", mcp.Required(), mcp.Description("OCI ref or suite ID to resolve")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ref, err := req.RequireString("ref")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/executions/resolve-ref", map[string]string{"ref": ref})
		},
	)

	// ── Executions ────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_executions",
			mcp.WithDescription("List recent suite executions, most recent first."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/executions", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("launch_execution",
			mcp.WithDescription("Launch a new suite execution. Returns the execution record with an ID. Use watch_execution to block until it completes — it handles polling automatically. Use validate_plugin_config before launching if the suite uses plugin.run() steps."),
			mcp.WithString("suite_id", mcp.Required(), mcp.Description("Suite ID to execute")),
			mcp.WithString("profile", mcp.Description("Profile file name (e.g. staging.yaml). Omit to use the default.")),
			mcp.WithString("backend", mcp.Description("Backend agent ID. Omit to use the default backend.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			suiteID, err := req.RequireString("suite_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body := map[string]string{"suiteId": suiteID}
			if p := req.GetString("profile", ""); p != "" {
				body["profile"] = p
			}
			if b := req.GetString("backend", ""); b != "" {
				body["backend"] = b
			}
			return callPost(ctx, c, "/api/v1/executions", body)
		},
	)

	s.AddTool(
		mcp.NewTool("get_execution",
			mcp.WithDescription("Get full execution details: status, step snapshots, events, and test/coverage artifacts."),
			mcp.WithString("execution_id", mcp.Required(), mcp.Description("Execution ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("execution_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/executions/"+id, nil)
		},
	)

	s.AddTool(
		mcp.NewTool("watch_execution",
			mcp.WithDescription("Block until an execution reaches a terminal state (healthy or failed), then return its full record. Poll interval grows automatically from 2s up to 30s. Use this after launch_execution to wait for results without spinning."),
			mcp.WithString("execution_id", mcp.Required(), mcp.Description("Execution ID to watch")),
			mcp.WithNumber("timeout_minutes", mcp.Description("Maximum minutes to wait before giving up (default: 30)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("execution_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			minutes := req.GetFloat("timeout_minutes", 30)
			timeout := time.Duration(minutes * float64(time.Minute))
			data, err := c.watchExecution(ctx, id, timeout)
			if err != nil {
				if data != nil {
					// Timed out but we have the last known state — return it with an annotation.
					return mcp.NewToolResultText(prettyJSON(data)), nil
				}
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(prettyJSON(data)), nil
		},
	)

	s.AddTool(
		mcp.NewTool("get_execution_overview",
			mcp.WithDescription("Get the live execution dashboard: all active executions with step counts and progress ratios."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/executions/overview", nil)
		},
	)

	// ── Catalog ───────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_packages",
			mcp.WithDescription("List all packages in the OCI catalog (suites, tasks, mocks, etc.) across configured registries."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/catalog/packages", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("get_package",
			mcp.WithDescription("Get metadata for a specific catalog package."),
			mcp.WithString("package_id", mcp.Required(), mcp.Description("Package ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("package_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/catalog/packages/"+id, nil)
		},
	)

	s.AddTool(
		mcp.NewTool("list_favorites",
			mcp.WithDescription("List the current user's starred catalog packages."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/catalog/favorites", nil)
		},
	)

	// ── Platform ──────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("get_platform_settings",
			mcp.WithDescription("Get the platform configuration: execution agents, OCI registries, and secrets provider settings."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/platform-settings", nil)
		},
	)

	// ── Sandboxes ─────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_sandboxes",
			mcp.WithDescription("List active Docker/Kubernetes sandboxes: running containers, networks, volumes, and resource usage."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/sandboxes", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("reap_sandbox",
			mcp.WithDescription("Clean up a specific sandbox (containers, networks, volumes) by its execution ID."),
			mcp.WithString("sandbox_id", mcp.Required(), mcp.Description("Sandbox/execution ID to clean up")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("sandbox_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callPost(ctx, c, "/api/v1/sandboxes/"+id+"/reap", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("reap_all_sandboxes",
			mcp.WithDescription("Clean up ALL BabelSuite-managed sandboxes. Use with care — removes all running test environments."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callPost(ctx, c, "/api/v1/sandboxes/reap-all", nil)
		},
	)

	// ── Plugins ───────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_plugins",
			mcp.WithDescription("List all registered APISIX Lua plugins. Each plugin can be used in a suite with plugin.run()."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/platform-settings/plugins", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("create_plugin",
			mcp.WithDescription("Register a new APISIX Lua plugin. The plugin becomes available immediately as a plugin.run() step in any suite. The Lua source is embedded into the APISIX sidecar config at execution time. After creating, call check_plugin to verify the schema and trigger are valid."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Unique plugin name, e.g. babelsuite-pii-scanner")),
			mcp.WithString("trigger", mcp.Required(), mcp.Description("APISIX trigger route the runner POSTs to, e.g. /_babelsuite/plugins/pii-scanner/start")),
			mcp.WithString("lua", mcp.Required(), mcp.Description("Full Lua plugin source code. Must implement _M.access(conf, ctx) and return _M.")),
			mcp.WithString("kind", mcp.Description("Step kind the plugin handles. Defaults to 'plugin'.")),
			mcp.WithString("variants", mcp.Description("Comma-separated list of variant names the plugin handles, e.g. 'pii-scanner'.")),
			mcp.WithString("schema", mcp.Description("CUE schema string for validating the config dict passed to plugin.run(). Optional but recommended.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			trigger, err := req.RequireString("trigger")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			lua, err := req.RequireString("lua")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body := map[string]any{
				"name":    name,
				"trigger": trigger,
				"lua":     lua,
				"kind":    req.GetString("kind", "plugin"),
			}
			if v := req.GetString("variants", ""); v != "" {
				variants := []string{}
				for _, s := range strings.Split(v, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						variants = append(variants, s)
					}
				}
				body["variants"] = variants
			}
			if v := req.GetString("schema", ""); v != "" {
				body["schema"] = v
			}
			return callPost(ctx, c, "/api/v1/platform-settings/plugins", body)
		},
	)

	s.AddTool(
		mcp.NewTool("delete_plugin",
			mcp.WithDescription("Remove a registered plugin by name. Suites that reference it will fail until a replacement is registered."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Plugin name to remove")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, err := c.delete(ctx, "/api/v1/platform-settings/plugins/"+name)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if data != nil {
				return mcp.NewToolResultText(prettyJSON(data)), nil
			}
			return mcp.NewToolResultText(`{"deleted":true}`), nil
		},
	)

	// ── Modules ───────────────────────────────────────────────────────────────

	s.AddTool(
		mcp.NewTool("list_modules",
			mcp.WithDescription("List all OCI module packages available in the catalog (e.g. @babelsuite/kafka, @babelsuite/postgres). Modules are loaded in suite.star with load()."),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callGet(ctx, c, "/api/v1/catalog/packages", map[string]string{"kind": "stdlib"})
		},
	)

	s.AddTool(
		mcp.NewTool("get_module",
			mcp.WithDescription("Get metadata and exported symbols for a specific OCI module package."),
			mcp.WithString("module_id", mcp.Required(), mcp.Description("Module package ID, e.g. stdlib-kafka")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("module_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/catalog/packages/"+id, nil)
		},
	)

	s.AddTool(
		mcp.NewTool("check_plugin",
			mcp.WithDescription("Verify a registered plugin's static configuration: confirms the plugin exists, the trigger path is valid, and the CUE schema (if provided) parses without errors. Call this after create_plugin before using the plugin in a suite."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Plugin name to check")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/platform-settings/plugins/"+name+"/check", nil)
		},
	)

	s.AddTool(
		mcp.NewTool("validate_plugin_config",
			mcp.WithDescription("Validate a config dict against a plugin's CUE schema without executing anything. Returns valid=true or a detailed CUE validation error. Use before launch_execution to catch config mistakes early."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Plugin name whose schema to validate against")),
			mcp.WithString("config", mcp.Required(), mcp.Description("JSON object representing the config dict passed to plugin.run()")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			configStr, err := req.RequireString("config")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var configMap map[string]any
			if err := json.Unmarshal([]byte(configStr), &configMap); err != nil {
				return mcp.NewToolResultError("config must be a valid JSON object: " + err.Error()), nil
			}
			return callPost(ctx, c, "/api/v1/platform-settings/plugins/"+name+"/validate", map[string]any{"config": configMap})
		},
	)

	s.AddTool(
		mcp.NewTool("get_execution_logs",
			mcp.WithDescription("Return a snapshot of all log lines emitted so far for an execution. Plugin findings, step output, and error messages are all included. Useful after watch_execution to inspect what each plugin step reported."),
			mcp.WithString("execution_id", mcp.Required(), mcp.Description("Execution ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("execution_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return callGet(ctx, c, "/api/v1/executions/"+id+"/logs/snapshot", nil)
		},
	)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "babelsuite-mcp: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func callGet(ctx context.Context, c *client, path string, params map[string]string) (*mcp.CallToolResult, error) {
	data, err := c.get(ctx, path, params)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(prettyJSON(data)), nil
}

func callPost(ctx context.Context, c *client, path string, body any) (*mcp.CallToolResult, error) {
	data, err := c.post(ctx, path, body)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(prettyJSON(data)), nil
}

func prettyJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
