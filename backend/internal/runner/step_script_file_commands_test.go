package runner

import (
	"strings"
	"testing"
)

// A step with a file and commands ships the file and runs the commands, and only
// them: the file is for the commands to use.
func TestAStepWithAFileAndCommandsRunsOnlyItsCommands(t *testing.T) {
	t.Parallel()
	script := buildStepScript(StepSpec{Node: StepNode{
		File:        "checkout.spec.ts",
		FileContent: "test('x', () => {})",
		Commands:    []string{"npx playwright test checkout.spec.ts"},
	}})
	if !strings.Contains(script, "npx playwright test checkout.spec.ts") {
		t.Fatalf("the commands are missing:\n%s", script)
	}
	if strings.Contains(script, "--experimental-strip-types") || strings.Contains(script, "ts-node") {
		t.Fatalf("the file was run by its own interpreter as well:\n%s", script)
	}
}

func TestAStepWithOnlyAFileStillRunsIt(t *testing.T) {
	t.Parallel()
	script := buildStepScript(StepSpec{Node: StepNode{File: "seed.py", FileContent: "print(1)"}})
	if !strings.Contains(script, "python "+containerWorkspaceMount+"/seed.py") {
		t.Fatalf("the file is not run:\n%s", script)
	}
}
