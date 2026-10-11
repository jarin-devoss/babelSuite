# Gateway Check

The smallest suite that proves a test step can reach the suite's APISIX sidecar.

One mock surface and one test step, so when it fails there is nothing else to
rule out:

```
echo-mock (service.mock)  ->  gateway-reachable (test.run, curlimages/curl)
```

`tests/reach.sh` asserts two things the runner is responsible for:

1. **`BABELSUITE_GATEWAY_URL` reaches the step environment.** A task or test
   that wants to call a mock surface has to be told where the sidecar is. If
   this variable is missing, the script fails rather than falling back to a
   hardcoded host and port — which is how a suite ends up passing against
   nothing.
2. **The sidecar answers from inside the step container.** The step and the
   sidecar are separate containers, so reaching it exercises the execution
   network, the sidecar's routes, and the mock dispatch table together.

Both failures are reported with a `GATEWAY-CHECK FAIL:` prefix and a non-zero
exit, so the cause is in the step's own logs.

## Why the image matters

`curlimages/curl` is Alpine-based and ships **no bash**. That is deliberate:
most images a step would pick for a tool are, so this suite also pins down that
the runner executes a `.sh` step with `sh` rather than `bash`. Running it with
`bash` exits 127 before the script prints anything at all.

## Running it

```bash
python scripts/push-to-zot.py --suite gateway-check
# restart the backend so it re-reads the catalog, then launch gateway-check
# with the Local profile
```
