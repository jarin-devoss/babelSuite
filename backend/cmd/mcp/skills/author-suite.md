---
name: author-suite
description: Use when asked to create, write, or scaffold a new BabelSuite test suite from scratch
---

# Author a Suite

## Overview

A suite is a `suite.star` topology plus the files its steps run. Creating one without those files produces a shell whose steps have nothing to execute, so send both in a single `create_suite` call.

## Prerequisites

- An authenticated session (`sign_in`, or `BABELSUITE_TOKEN` already set)
- A suite ID made of lowercase letters, digits, hyphens, or underscores

## Workflow

1. **Look at a working suite first.** Topology families and argument names are easier to copy than to guess:
   ```
   list_suites()
   get_suite(suite_id="payment-suite")
   ```

2. **Write the topology.** Each assignment becomes a step, and `after=[...]` builds the dependency graph:
   ```python
   load("@babelsuite/runtime", "service", "task", "test")

   db      = service.run(name="db", image="postgres:16")
   migrate = task.run(name="migrate", file="migrate.py", image="python:3.12", after=[db])
   smoke   = test.run(name="smoke", file="smoke.py", image="python:3.12", after=[migrate])
   ```

3. **Create it with every file the topology refers to.** `file="migrate.py"` on a task resolves to `tasks/migrate.py`; on a test it resolves to `tests/smoke.py`. A `profiles/*.yaml` entry becomes a launchable profile:
   ```
   create_suite(
     id="checkout-suite",
     suite_star="<the topology above>",
     source_files=[
       {"path": "tasks/migrate.py",    "content": "..."},
       {"path": "tests/smoke.py",      "content": "..."},
       {"path": "profiles/local.yaml", "content": "name: Local\ndefault: true\nenv:\n  LOG_LEVEL: debug\n"}
     ]
   )
   ```

4. **Confirm the topology resolved.** A non-empty `topologyError` means the graph was rejected and the suite will not launch:
   ```
   get_suite(suite_id="checkout-suite")
   ```

5. **Run it.** `watch_execution` blocks until the run reaches a terminal state:
   ```
   launch_execution(suite_id="checkout-suite", profile="local.yaml")
   watch_execution(execution_id="<id>")
   ```

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `suite ID must contain only lowercase letters, digits, hyphens, or underscores` | Capitals or spaces in the ID | Use `checkout-suite`, not `Checkout Suite` |
| `A suite with this ID already exists` | The ID is taken | Pick another ID — suites are not updated in place |
| `root file "X" is not allowed in a suite package` | A source file sits at the package root | Move it under `tasks/`, `tests/`, `profiles/`, `api/`, `mock/`, or `fixtures/` |
| `source path "X" uses an unsupported file type` / `uses a blocked file type` | Extension not on the allow list, or an archive/binary | Use a supported type such as `.py`, `.sh`, `.go`, `.yaml`, `.json` |
| `after elements must be node references` | `after=[...]` was given a string or a value that is not a step | Pass the assignment variable itself, e.g. `after=[db]` |
| `undefined: X` | `after=[...]` names something never assigned | Define the step first, or fix the spelling |
| A step starts and exits instantly with code 0 | A misspelled argument was ignored — `commands` is plural, and unrecognised keywords are dropped without a warning | Re-check every keyword against the table below |
| A step runs but does nothing | The file it names was not included in `source_files`, and that is not reported at creation | Add the file, then create the suite again under a new id |
| `container exited with code 127` | The file's interpreter is missing from the image — `.sh` runs under `bash`, `.py` under `python` | Match the image to the file type: `bash:5.2` for `.sh`, `python:3.12` for `.py`. `busybox` has no bash |

## Step Arguments

Every step family draws from one shared set of keywords. An unrecognised keyword is **silently ignored**, so a typo costs a whole run to notice — check names against this table rather than relying on an error.

| Argument | Applies to | Purpose |
|----------|-----------|---------|
| `name` | all | Step name; also the container's network alias |
| `image` | all runnable | Container image |
| `file` | task, test | Script to run, resolved under `tasks/` or `tests/` |
| `commands` | all runnable | Shell lines run in order — **plural**, and each entry is a shell string (`["sleep 300"]`, not `["sh","-c","sleep 300"]`) |
| `env` | all runnable | Environment variables for the container |
| `after` | all | Steps this one waits on; pass the variables, not strings |
| `definition` | mock | Folder of mock assets. Accepted by existing suites but currently ignored — mock surfaces are read from the package's whole `mock/` folder |
| `on_failure` | all | Runs only when a named step fails |
| `continue_on_failure` | all | Keep the run going when this step fails |
| `expect_exit`, `expect_logs`, `fail_on_logs` | task, test | Success and failure assertions |
| `reset_mocks` | test | Mocks to clear before the test |
| `exports` | task, test | Artifacts to collect |

Services are detached: the engine only checks the container did not crash immediately, so a step that must wait for readiness should poll for it.

## File Type and Image

The interpreter is chosen from the file extension, so the image has to provide it:

| Extension | Runs with | Workable image |
|-----------|-----------|----------------|
| `.sh`, `.bash` | `bash` | `bash:5.2` |
| `.py` | `python` | `python:3.12` |
| `.js` | `node` | `node:22` |
| `.go` | compiled | `golang:1.24` |

Steps also share a writable directory at `$BABELSUITE_WORKSPACE_DIR`, so one step can leave state behind for a later one to assert on.
