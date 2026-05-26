package configcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/babelsuite/babelsuite/cli/ctl/internal/support"
	"github.com/babelsuite/babelsuite/pkg/apiclient"
)

func Run(_ context.Context, rt *support.Runtime, opts support.GlobalOptions, args []string) int {
	if len(args) == 0 {
		return runShow(rt, opts)
	}
	switch args[0] {
	case "set":
		return runSet(rt, opts, args[1:])
	case "get":
		return runGet(rt, opts, args[1:])
	case "show", "list":
		return runShow(rt, opts)
	case "-h", "--help", "help":
		printUsage(rt)
		return 0
	default:
		rt.Fail(fmt.Errorf("unknown config command %q", args[0]))
		return 1
	}
}

func printUsage(rt *support.Runtime) {
	_, _ = fmt.Fprintln(rt.Stdout, "Usage: babelctl config [show] | set <key> <value> | get <key>")
	_, _ = fmt.Fprintln(rt.Stdout)
	_, _ = fmt.Fprintln(rt.Stdout, "Keys:")
	_, _ = fmt.Fprintln(rt.Stdout, "  server   Control plane URL (e.g. https://babelsuite.example.com)")
	_, _ = fmt.Fprintln(rt.Stdout, "  token    Session token (use babelctl login to authenticate interactively)")
}

func runShow(rt *support.Runtime, opts support.GlobalOptions) int {
	cfg, err := rt.Store.Load()
	if err != nil {
		rt.Fail(err)
		return 1
	}

	server := support.FirstNonEmpty(opts.Server, cfg.Server)
	if opts.Output == "json" {
		_ = support.PrintJSON(rt.Stdout, map[string]any{
			"server":    server,
			"email":     cfg.Email,
			"fullName":  cfg.FullName,
			"workspace": cfg.Workspace,
			"config":    rt.Store.Path(),
		})
		return 0
	}

	support.PrintKeyValues(rt.Stdout, [][2]string{
		{"Server", support.FirstNonEmpty(server, "(not set)")},
		{"Email", support.FirstNonEmpty(cfg.Email, "(not set)")},
		{"Workspace", support.FirstNonEmpty(cfg.Workspace, "(not set)")},
		{"Config", rt.Store.Path()},
	})
	return 0
}

func runSet(rt *support.Runtime, opts support.GlobalOptions, args []string) int {
	if len(args) < 2 {
		rt.Fail(fmt.Errorf("config set requires a key and a value"))
		return 1
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	value := strings.TrimSpace(args[1])

	cfg, err := rt.Store.Load()
	if err != nil {
		rt.Fail(err)
		return 1
	}

	switch key {
	case "server":
		cfg.Server = apiclient.New(value, "").BaseURL
	case "token":
		cfg.Token = value
	default:
		rt.Fail(fmt.Errorf("unknown config key %q; valid keys: server, token", key))
		return 1
	}

	if err := rt.Store.Save(cfg); err != nil {
		rt.Fail(err)
		return 1
	}

	if opts.Output == "json" {
		_ = support.PrintJSON(rt.Stdout, map[string]any{"key": key, "set": true})
		return 0
	}

	_, _ = fmt.Fprintf(rt.Stdout, "%s updated.\n", key)
	return 0
}

func runGet(rt *support.Runtime, opts support.GlobalOptions, args []string) int {
	if len(args) == 0 {
		rt.Fail(fmt.Errorf("config get requires a key"))
		return 1
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))

	cfg, err := rt.Store.Load()
	if err != nil {
		rt.Fail(err)
		return 1
	}

	var value string
	switch key {
	case "server":
		value = support.FirstNonEmpty(opts.Server, cfg.Server)
	case "token":
		value = cfg.Token
	default:
		rt.Fail(fmt.Errorf("unknown config key %q; valid keys: server, token", key))
		return 1
	}

	if opts.Output == "json" {
		_ = support.PrintJSON(rt.Stdout, map[string]any{"key": key, "value": value})
		return 0
	}

	_, _ = fmt.Fprintln(rt.Stdout, value)
	return 0
}
