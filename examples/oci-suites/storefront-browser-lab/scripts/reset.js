// Rollback for the api-smoke step.
//
// Declared with on_failure=[api_smoke], so this runs only when the API smoke
// test failed. It puts the catalog mock back to a known state and leaves a
// clear record of what it reverted, which is the whole point of a rollback
// step: the next run should not inherit whatever the failed test left behind.
//
// Plain JavaScript and stdlib only — `node scripts/reset.js` has to work in
// node:22-alpine with no package install, because a rollback that needs the
// network is a rollback that fails exactly when you need it.

const fs = require("fs");
const path = require("path");

const CATALOG_URL = process.env.CATALOG_URL ?? "http://catalog-api:9080";
const workspace = process.cwd();

function log(message) {
  console.log(`reset-catalog: ${message}`);
}

log(`workspace=${workspace}`);
log(`catalog=${CATALOG_URL}`);

// Reports from the failed run are what a human will read next, so they are
// kept rather than deleted; anything the test wrote as scratch state is not.
const scratch = ["tmp", ".cache", "node_modules/.vite"];
let removed = 0;
for (const rel of scratch) {
  const target = path.join(workspace, rel);
  try {
    if (fs.existsSync(target)) {
      fs.rmSync(target, { recursive: true, force: true });
      removed += 1;
      log(`removed scratch path ${rel}`);
    }
  } catch (err) {
    // A rollback must not fail on top of the failure it is cleaning up after.
    log(`could not remove ${rel}: ${err instanceof Error ? err.message : err}`);
  }
}
log(`${removed} scratch path(s) removed`);

const reportsDir = path.join(workspace, "reports");
if (fs.existsSync(reportsDir)) {
  const kept = fs.readdirSync(reportsDir);
  log(`kept ${kept.length} report file(s) for inspection: ${kept.join(", ") || "none"}`);
} else {
  log("no reports directory — the smoke test failed before writing one");
}

// The catalog mock's state is owned by the sidecar, which the runner resets
// through reset_mocks on the next step that declares it. Recording the intent
// here keeps the rollback honest about what it can and cannot undo itself.
fs.mkdirSync(reportsDir, { recursive: true });
fs.writeFileSync(
  path.join(reportsDir, "rollback.json"),
  JSON.stringify(
    {
      step: "reset-catalog",
      ranAt: new Date().toISOString(),
      catalogUrl: CATALOG_URL,
      scratchPathsRemoved: removed,
      note: "catalog mock state is reset by the runner via reset_mocks, not by this script",
    },
    null,
    2,
  ),
  "utf-8",
);

log("rollback complete");
