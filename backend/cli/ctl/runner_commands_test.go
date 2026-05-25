package babelctl

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func newTestRunner() (*Runner, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return NewRunner(&stdout, &stderr), &stdout, &stderr
}

func TestRunnerNoArgsShowsHelp(t *testing.T) {
	t.Parallel()
	runner, stdout, stderr := newTestRunner()
	status := runner.Run(context.Background(), []string{})
	if status != 0 {
		t.Fatalf("expected exit 0 for no args, got %d (stderr: %s)", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Commands:") {
		t.Fatalf("expected help output for no args, got: %s", stdout.String())
	}
}

func TestRunnerExplicitHelpFlag(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"-h", "--help", "help"} {
		runner, stdout, _ := newTestRunner()
		status := runner.Run(context.Background(), []string{flag})
		if status != 0 {
			t.Fatalf("flag %q: expected exit 0, got %d", flag, status)
		}
		if !strings.Contains(stdout.String(), "Commands:") {
			t.Fatalf("flag %q: expected help in output, got: %s", flag, stdout.String())
		}
	}
}

func TestRunnerInvalidOutputFormat(t *testing.T) {
	t.Parallel()
	runner, _, stderr := newTestRunner()
	status := runner.Run(context.Background(), []string{"--output", "yaml", "version"})
	if status != 1 {
		t.Fatalf("expected exit 1 for invalid output format, got %d", status)
	}
	if !strings.Contains(stderr.String(), "unsupported output format") {
		t.Fatalf("expected unsupported format error, got: %s", stderr.String())
	}
}

func TestRunnerOutputTextFormat(t *testing.T) {
	t.Parallel()
	runner, stdout, _ := newTestRunner()
	status := runner.Run(context.Background(), []string{"--output", "text", "version"})
	if status != 0 {
		t.Fatalf("expected exit 0 with --output text, got %d", status)
	}
	if !strings.Contains(stdout.String(), "babelctl") {
		t.Fatalf("expected version output, got: %s", stdout.String())
	}
}

func TestRunnerOutputJSONFormatAccepted(t *testing.T) {
	t.Parallel()
	runner, _, stderr := newTestRunner()
	// JSON output is accepted at the runner level; version command itself doesn't emit JSON
	// but the runner should not reject the format.
	status := runner.Run(context.Background(), []string{"--output", "json", "version"})
	if status != 0 {
		t.Fatalf("expected exit 0 with --output json, got %d (stderr: %s)", status, stderr.String())
	}
}

func TestRunnerAllCommandNamesAreDispatchable(t *testing.T) {
	t.Parallel()
	// Every registered command name must be findable without error.
	runner, _, _ := newTestRunner()
	for _, group := range runner.commandGroups() {
		for _, cmd := range group.commands {
			if _, ok := runner.findCommand(cmd.name); !ok {
				t.Errorf("command %q is registered but findCommand returns not found", cmd.name)
			}
			for _, alias := range cmd.aliases {
				if _, ok := runner.findCommand(alias); !ok {
					t.Errorf("alias %q for command %q is not findable", alias, cmd.name)
				}
			}
		}
	}
}

func TestRunnerHelpListsAllGroups(t *testing.T) {
	t.Parallel()
	runner, stdout, _ := newTestRunner()
	runner.Run(context.Background(), []string{"help"})
	out := stdout.String()
	for _, group := range runner.commandGroups() {
		if !strings.Contains(out, group.title+":") {
			t.Errorf("help output missing group %q", group.title)
		}
	}
}

func TestRunnerHelpListsAllCommandUsages(t *testing.T) {
	t.Parallel()
	runner, stdout, _ := newTestRunner()
	runner.Run(context.Background(), []string{"help"})
	out := stdout.String()
	for _, group := range runner.commandGroups() {
		for _, cmd := range group.commands {
			if !strings.Contains(out, cmd.usage) {
				t.Errorf("help output missing usage %q", cmd.usage)
			}
		}
	}
}

func TestRunnerHelpShowsAliases(t *testing.T) {
	t.Parallel()
	runner, stdout, _ := newTestRunner()
	runner.Run(context.Background(), []string{"help"})
	// "environments" has alias "envs" — it should appear in help.
	if !strings.Contains(stdout.String(), "envs") {
		t.Fatal("expected alias 'envs' in help output")
	}
}

func TestRunnerConfigPathOption(t *testing.T) {
	t.Parallel()
	runner, stdout, _ := newTestRunner()
	// Passing a non-existent config path should be accepted at the runner level
	// (the store is lazy — it only errors on actual reads).
	status := runner.Run(context.Background(), []string{"--config", "/tmp/nonexistent-babel.yaml", "version"})
	if status != 0 {
		t.Fatalf("expected exit 0 with custom config path, got %d", status)
	}
	_ = stdout
}
