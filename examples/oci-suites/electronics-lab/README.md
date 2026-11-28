# Electronics Lab

Plugin steps that check an analog filter (SPICE), a digital counter (Verilog) and a mass-spring-damper (control theory). The simulators are mocks: each answers by the design it is sent, with an example per known design and an error for any other. They do not simulate; their answers are the ones written in `mock/`.

## Execution order

```
spice-service ─── rc-filter-step-response
verilog-service ─ 4bit-counter-correctness ── 4bit-counter-strict
                ├ 4bit-counter-lint
                └ 4bit-counter-coverage
control-service ─ mass-spring-damper-response
```

## What breaks it

`DESIGN` decides what is sent. Under `broken.yaml` the same steps send a 10k RC filter (rise time 25 ms against a limit of 3 ms), a counter that never assigns its reset (its output stays X) and a ringing plant (73% overshoot), and the run must end **Failed**.

An example that matched any request would pass them. Each mock example is keyed on the design it is for (`dispatch:` on the netlist, the Verilog, or the denominator), so a design nobody wrote an answer for gets the fallback error.

## Profiles

| Profile | What it does |
|---|---|
| `local.yaml` | The good designs, default thresholds. |
| `strict.yaml` | The good designs, tighter thresholds. |
| `broken.yaml` | Bad designs; the run must end Failed. |

## Structure

| Path | Description |
|---|---|
| `suite.star` | The designs and the steps. |
| `mock/` | `spice-service`, `verilog-service`, `control-service`. |
| `api/` | Each simulator's contract. |

## Running

```bash
python scripts/push-to-zot.py --suite electronics-lab --sync http://localhost:8090
```

Then launch it from the UI, or `POST /api/v1/executions` with `{"suiteId": "electronics-lab", "profile": "local.yaml"}`.

The plugins (`spice-sim`, `verilog-sim`, `control-sim`) are in `examples/oci-plugins/` and are registered in platform settings.
