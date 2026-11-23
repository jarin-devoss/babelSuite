package runner

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
)

// Two runs of one suite that boot together both ask for its sidecar. Before, each
// inspected, found none, and went on to create one: the slower got "container
// name already in use" and its run failed at boot, or removed the sidecar the
// faster had just started.
func TestConcurrentEnsureSuiteSidecarStartsOneSidecar(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil || !pingDocker(context.Background()) {
		t.Skip("docker is not available")
	}
	const suiteID, profile = "sidecar-concurrent", "local.yaml"
	name := SidecarContainerName(suiteID, profile)
	remove := func() { _ = exec.Command("docker", "rm", "-f", name).Run() }
	remove()
	t.Cleanup(remove)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = EnsureSuiteSidecar(apisix.SuiteConfig{ID: suiteID}, profile, "apache/apisix:3.9.0-debian", "")
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
	}

	out, _ := exec.Command("docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}} {{.State}}").CombinedOutput()
	lines := strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(out)), "\n", " "))
	if len(lines) != 2 || lines[1] != "running" {
		t.Fatalf("want exactly one running sidecar, docker lists: %q", strings.TrimSpace(string(out)))
	}

	url := SuiteSidecarURL(suiteID, profile)
	if url == "" {
		t.Fatal("the sidecar is not published on a host port")
	}
	if err := waitForSidecarAnswer(url, 20*time.Second); err != nil {
		t.Fatalf("the one sidecar does not serve: %v", err)
	}
}

// A sidecar another process started between our check and our create is used
// when it is the one we would have made, and not when it was made from another
// configuration.
func TestARunningSidecarIsRecognisedByItsConfiguration(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil || !pingDocker(context.Background()) {
		t.Skip("docker is not available")
	}
	const suiteID, profile, image = "sidecar-recognised", "local.yaml", "apache/apisix:3.9.0-debian"
	name := SidecarContainerName(suiteID, profile)
	remove := func() { _ = exec.Command("docker", "rm", "-f", name).Run() }
	remove()
	t.Cleanup(remove)

	config := apisix.SuiteConfig{ID: suiteID}
	if err := EnsureSuiteSidecar(config, profile, image, ""); err != nil {
		t.Fatal(err)
	}
	cli, _ := sharedDockerClient()

	same := sidecarHash(config, image)
	if url := runningSidecarWithHash(cli, name, same); url == "" || url != SuiteSidecarURL(suiteID, profile) {
		t.Fatalf("the running sidecar was not recognised by its own configuration: %q", url)
	}
	if url := runningSidecarWithHash(cli, name, "another-configuration"); url != "" {
		t.Fatalf("a sidecar from another configuration was taken for ours: %q", url)
	}
	if url := runningSidecarWithHash(cli, "babel-sidecar-does-not-exist", same); url != "" {
		t.Fatalf("a missing container was taken for a sidecar: %q", url)
	}
}

func TestNameConflictsAreRecognised(t *testing.T) {
	err := errors.New(`create sidecar container: Error response from daemon: Conflict. The container name "/x" is already in use by container "abc"`)
	if !isNameConflict(err) {
		t.Fatal("a name conflict was not recognised")
	}
	if isNameConflict(errors.New("pull apisix image: not found")) {
		t.Fatal("an unrelated error was taken for a name conflict")
	}
}
