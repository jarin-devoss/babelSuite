package execution

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/profiles"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// runModuleSuite runs a suite.star that uses the standard modules on the local
// Docker backend and returns the run's final status and its log text.
func runModuleSuite(t *testing.T, id, star string, files ...suites.SourceFile) (string, string) {
	t.Helper()
	requireDocker(t)
	base, err := suites.NewWorkspaceService().Get("storefront-browser-lab")
	if err != nil {
		t.Fatalf("load a base suite: %v", err)
	}
	definition := *base
	definition.ID = id
	definition.SuiteStar = star
	definition.APISurfaces = nil
	definition.SourceFiles = append(append([]suites.SourceFile{}, definition.SourceFiles...), files...)
	definition.Profiles = []suites.ProfileOption{{FileName: "local.yaml", Default: true}}

	service := NewServiceWithPlatform(staticSuiteSource{items: map[string]suites.Definition{id: definition}},
		stubPlatformSource{settings: &platform.PlatformSettings{
			Agents: []platform.ExecutionAgent{{AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true}},
		}})
	defer service.Close()

	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: id, Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create the execution: %v", err)
	}
	snapshot := waitForExecutionTerminal(t, service, execution.ID)

	var text strings.Builder
	if lines, err := service.SnapshotLogs(execution.ID, ""); err == nil {
		for _, entry := range lines {
			text.WriteString(entry.Text + "\n")
		}
	}
	if record, err := service.GetExecution(execution.ID, ""); err == nil {
		for _, event := range record.Events {
			text.WriteString("[" + event.Level + "] " + event.Text + "\n")
		}
	}
	return snapshot.Status, text.String()
}

func TestTheRedisModuleHonoursItsOptionsInARealContainer(t *testing.T) {
	status, logs := runModuleSuite(t, "module-redis", `
load("@babelsuite/redis", "redis", "wait_ready", "set_key")
load("@babelsuite/runtime", "task")

cache = redis(name="cache", port=6380, password="pw", max_memory="1mb", max_memory_policy="allkeys-lfu")
ready = wait_ready(cache)
stored = set_key(cache, "greeting", "hello", after=[ready])
task.run(name="show", image="redis:7.2-alpine", after=[stored], commands=[
    "REDISCLI_AUTH=pw redis-cli -h cache -p 6380 CONFIG GET maxmemory",
    "REDISCLI_AUTH=pw redis-cli -h cache -p 6380 CONFIG GET maxmemory-policy",
    "REDISCLI_AUTH=pw redis-cli -h cache -p 6380 GET greeting",
    "redis-cli -h cache -p 6380 PING",
])
`)
	if status != "Healthy" {
		t.Fatalf("the run ended %s:\n%s", status, logs)
	}
	for _, want := range []string{"1048576", "allkeys-lfu", "hello", "NOAUTH"} {
		if !strings.Contains(logs, want) {
			t.Errorf("the output lacks %q:\n%s", want, logs)
		}
	}
}

func TestThePostgresHelpersReachAServiceOnAnotherPortWithItsCredentials(t *testing.T) {
	status, logs := runModuleSuite(t, "module-postgres", `
load("@babelsuite/postgres", "pg", "connect", "query", "insert", "select")

db = pg(port=5433, database="shop", username="alice", password="s3cret")
ready = connect(db)
made = query(db, "create table items (name text);", name="create", after=[ready])
put = insert(db, "items", {"name": "widget"}, after=[made])
select(db, "items", after=[put])
`)
	if status != "Healthy" {
		t.Fatalf("the run ended %s:\n%s", status, logs)
	}
	if !strings.Contains(logs, "widget") {
		t.Errorf("the select did not return the inserted row:\n%s", logs)
	}
}

func TestTheMongoModuleInitiatesAReplicaSetAndAuthenticatesItsHelpers(t *testing.T) {
	status, logs := runModuleSuite(t, "module-mongo", `
load("@babelsuite/mongodb", "mongodb", "create_collection", "insert_documents")
load("@babelsuite/runtime", "task")

cluster = mongodb(name="mongo", port=27018, username="root", password="secret", replica_set="rs0")
made = create_collection(cluster, "shop", "orders")
put = insert_documents(cluster, "shop", "orders", [{"sku": "a1"}], after=[made])
task.run(name="status", image="mongo:7.0", after=[put], commands=[
    "mongosh --host mongo --port 27018 --username root --password secret --authenticationDatabase admin --quiet --eval 'print(\"RSOK=\" + rs.status().ok)'",
    "mongosh --host mongo --port 27018 --quiet --eval 'db.getSiblingDB(\"shop\").orders.countDocuments()' || echo UNAUTHENTICATED_REFUSED",
])
`)
	if status != "Healthy" {
		t.Fatalf("the run ended %s:\n%s", status, logs)
	}
	if !strings.Contains(logs, "RSOK=1") {
		t.Errorf("rs.status() did not report ok:\n%s", logs)
	}
}

const playwrightSpec = `import { test, expect } from "@playwright/test"

test("the spec body ran", async () => {
  console.log("VERIFY playwright-test-body-ran")
  expect(1 + 1).toBe(%s)
})
`

// The Playwright module runs a spec with the Playwright runner and exports its
// JUnit report. Run as a node script, the spec failed before its body ran: there
// was no test runner in the process.
func TestThePlaywrightModuleRunsASpecWithThePlaywrightRunner(t *testing.T) {
	// The image is about 2 GB and the step installs @playwright/test from the npm
	// registry, so this runs where the image was pulled beforehand
	// (docker pull mcr.microsoft.com/playwright:v1.53.0-noble) and is skipped
	// elsewhere instead of timing out on a pull.
	if err := exec.Command("docker", "image", "inspect", "mcr.microsoft.com/playwright:v1.53.0-noble").Run(); err != nil {
		t.Skip("the Playwright image is not pulled: docker pull mcr.microsoft.com/playwright:v1.53.0-noble")
	}
	star := `
load("@babelsuite/playwright", "browser_test")
browser_test(spec="demo.spec.ts", base_url="http://unused:1")
`
	status, logs := runModuleSuite(t, "module-playwright", star,
		suites.SourceFile{Path: "tests/demo.spec.ts", Content: strings.Replace(playwrightSpec, "%s", "2", 1)})
	if status != "Healthy" {
		t.Fatalf("a passing spec ended %s:\n%s", status, logs)
	}
	if !strings.Contains(logs, "VERIFY playwright-test-body-ran") {
		t.Errorf("the spec body did not run:\n%s", logs)
	}

	status, logs = runModuleSuite(t, "module-playwright-fail", star,
		suites.SourceFile{Path: "tests/demo.spec.ts", Content: strings.Replace(playwrightSpec, "%s", "3", 1)})
	if status == "Healthy" {
		t.Fatalf("a failing spec passed:\n%s", logs)
	}
}

// A run under a profile whose env: says COUNT: "3" builds three steps. suite.star
// used to be evaluated before the profile was read, with an empty env, so every
// profile built one.
func TestARunReadsTheLaunchProfilesEnvInItsSuiteStar(t *testing.T) {
	requireDocker(t)
	source := staticSuiteSource{items: map[string]suites.Definition{
		"env-run": {
			ID:         "env-run",
			Title:      "Env Run",
			Repository: "localhost:5000/testing/env-run",
			SuiteStar: strings.Join([]string{
				`load("@babelsuite/runtime", "task")`,
				`for i in range(int(env.get("COUNT", "1"))):`,
				`    task.run(name="step-" + str(i), image="busybox", commands=["true"])`,
			}, "\n"),
			Profiles: []suites.ProfileOption{{FileName: "local.yaml", Label: "Local", Default: true}},
		},
	}}
	profileService := profiles.NewService(source, profiles.NewMemoryStore())
	if _, err := profileService.CreateProfile("env-run", profiles.UpsertRequest{
		Name: "Counted", FileName: "counted.yaml", Scope: "Test", YAML: "env:\n  COUNT: \"3\"\n", ExtendsID: "local",
	}); err != nil {
		t.Fatalf("create the profile: %v", err)
	}

	service := NewServiceWithPlatform(profileService, stubPlatformSource{settings: &platform.PlatformSettings{
		Agents: []platform.ExecutionAgent{{AgentID: "local-docker", Name: "Local Docker", Type: "local", Enabled: true, Default: true}},
	}})
	defer service.Close()

	for profile, want := range map[string]int{"counted.yaml": 3, "local.yaml": 1} {
		execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: "env-run", Profile: profile})
		if err != nil {
			t.Fatalf("%s: create the execution: %v", profile, err)
		}
		snapshot := waitForExecutionTerminal(t, service, execution.ID)
		if snapshot.TotalSteps != want || snapshot.Status != "Healthy" {
			t.Errorf("%s: %d steps, status %s; want %d steps, Healthy", profile, snapshot.TotalSteps, snapshot.Status, want)
		}
	}
}
