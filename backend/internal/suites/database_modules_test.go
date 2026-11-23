package suites

import (
	"strings"
	"testing"
)

func commandsOf(t *testing.T, nodes []rawTopologyNode, name string) string {
	t.Helper()
	for _, n := range nodes {
		if n.Name == name {
			return strings.Join(n.Commands, "\n")
		}
	}
	t.Fatalf("no node named %q among %d nodes", name, len(nodes))
	return ""
}

// redis-server reads none of the module's options from the environment, so they
// reach it as arguments; the helpers learn the port and password from the node.
func TestRedisModulePassesItsOptionsToRedisServer(t *testing.T) {
	t.Parallel()
	nodes, err := evalStarlarkTopology(`
load("@babelsuite/redis", "redis", "wait_ready", "set_key")
cache = redis(name="cache", port=6380, password="p'w", max_memory="1mb", max_memory_policy="allkeys-lfu", databases=4)
ready = wait_ready(cache)
set_key(cache, "k", "v", after=[ready])
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := commandsOf(t, nodes, "cache")
	for _, want := range []string{"redis-server", "'--port' '6380'", "'--maxmemory' '1mb'", "'--maxmemory-policy' 'allkeys-lfu'", "'--databases' '4'", "'--requirepass'"} {
		if !strings.Contains(server, want) {
			t.Errorf("redis-server arguments lack %s:\n%s", want, server)
		}
	}
	helper := commandsOf(t, nodes, "cache-set-k")
	if !strings.Contains(helper, "6380") || !strings.Contains(helper, "REDISCLI_AUTH='p'\"'\"'w'") {
		t.Errorf("the helper does not use the node's port and password:\n%s", helper)
	}
}

func TestMongoModuleConfiguresItsReplicaSetAndAuthenticatesItsHelpers(t *testing.T) {
	t.Parallel()
	nodes, err := evalStarlarkTopology(`
load("@babelsuite/mongodb", "mongodb", "create_collection")
cluster = mongodb(name="mongo", port=27018, username="root", password="secret", replica_set="rs0", wired_tiger_cache_gb=1)
create_collection(cluster, "shop", "orders")
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := commandsOf(t, nodes, "mongo")
	for _, want := range []string{"mongod", "'--port' '27018'", "'--replSet' 'rs0'", "'--wiredTigerCacheSizeGB' '1'", "'--keyFile'"} {
		if !strings.Contains(server, want) {
			t.Errorf("mongod arguments lack %s:\n%s", want, server)
		}
	}
	initiate := commandsOf(t, nodes, "mongo-rs-init")
	if !strings.Contains(initiate, "rs.initiate") || !strings.Contains(initiate, "mongo:27018") {
		t.Errorf("the replica set is not initiated with the member's address:\n%s", initiate)
	}
	helper := commandsOf(t, nodes, "mongo-create-shop-orders")
	for _, want := range []string{"mongo:27018", "--username 'root'", "--password 'secret'", "--authenticationDatabase admin"} {
		if !strings.Contains(helper, want) {
			t.Errorf("the helper lacks %s:\n%s", want, helper)
		}
	}
}

func TestMongoModuleRefusesHalfCredentials(t *testing.T) {
	t.Parallel()
	_, err := evalStarlarkTopology(`
load("@babelsuite/mongodb", "mongodb")
mongodb(username="root")
`, nil)
	if err == nil || !strings.Contains(err.Error(), "username= and password= together") {
		t.Fatalf("err = %v", err)
	}
}

// pg(port=, database=, password=) is honoured by every helper: they used to dial
// 5432 with the default credentials whatever the service was started with.
func TestPostgresHelpersUseTheServicesPortAndCredentials(t *testing.T) {
	t.Parallel()
	nodes, err := evalStarlarkTopology(`
load("@babelsuite/postgres", "pg", "connect", "query")
db = pg(port=5433, database="shop", username="alice", password="s3 cret")
c = connect(db)
query(db, "select 1", name="q", after=[c])
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db-connect", "q"} {
		cmd := commandsOf(t, nodes, name)
		for _, want := range []string{"PGPORT='5433'", "PGDATABASE='shop'", "PGUSER='alice'", "PGPASSWORD='s3 cret'", "PGHOST='db'"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("%s: missing %s:\n%s", name, want, cmd)
			}
		}
	}
}

// The Playwright module runs a spec with the Playwright runner and the JUnit
// reporter, with the package at the image's version, and ships the spec as a file
// rather than running it with node.
func TestPlaywrightModuleRunsSpecsWithThePlaywrightRunner(t *testing.T) {
	t.Parallel()
	nodes, err := evalStarlarkTopology(`
load("@babelsuite/playwright", "browser_test")
browser_test(spec="checkout.spec.ts", base_url="http://shop:3000")
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var node *rawTopologyNode
	for i := range nodes {
		if nodes[i].Kind == NodeKindTest {
			node = &nodes[i]
		}
	}
	if node == nil {
		t.Fatal("no test node")
	}
	if node.File != "checkout.spec.ts" {
		t.Errorf("the spec is not shipped as the step's file: %q", node.File)
	}
	if !strings.Contains(node.Image, "playwright:v1.53.0") {
		t.Errorf("image = %q", node.Image)
	}
	cmds := strings.Join(node.Commands, "\n")
	for _, want := range []string{"@playwright/test@1.53.0", "npx playwright test 'checkout.spec.ts'", "--reporter=junit", "PLAYWRIGHT_JUNIT_OUTPUT_NAME='reports/"} {
		if !strings.Contains(cmds, want) {
			t.Errorf("commands lack %s:\n%s", want, cmds)
		}
	}
}
