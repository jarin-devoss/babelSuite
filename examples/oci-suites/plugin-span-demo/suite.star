load("@babelsuite/runtime", "mock")
load("@plugins/llm-judge", "monitor", "consistency")
load("@plugins/shadow-diff", "diff", "compare_latency")

# Everything the two plugins talk to is a mock surface backed by this suite's
# own api/ and mock/ folders, so the run starts no containers of its own and
# needs no network. Both plugins execute in the suite's APISIX sidecar, which
# serves those surfaces — the same way electronics-lab points its simulation
# plugins at mocks.

CRITERIA = [
    "states a refund window in days",
    "provides a way to contact support",
    "avoids giving legal advice",
]

judge_model = mock.api(name = "judge-model")

# Two builds of one endpoint whose quotes agree on everything but total.
checkout_primary = mock.api(name = "checkout-primary")
checkout_shadow = mock.api(name = "checkout-shadow")

# monitor() reports its verdicts without failing the suite, so the step still
# contributes judge.* attributes to the trace even though one criterion fails.
answer_quality = monitor(
    name      = "answer-quality",
    criteria  = CRITERIA,
    model_url = "http://judge-model:9080/v1/chat/completions",
    output    = "Refunds within 30 days. Contact support@example.com. " +
                "You are legally entitled to compensation.",
    model     = "babelsuite-mock",
    threshold = 0.9,
    after     = [judge_model],
)

# One real difference survives ignoring trace, so a threshold of 1 passes while
# shadow.diffs=1 still reaches the trace.
shadow_compare = diff(
    name          = "shadow-compare",
    primary       = "http://checkout-primary:9080/quote",
    shadow        = "http://checkout-shadow:9080/quote",
    threshold     = 1,
    ignore_fields = ["trace"],
    after         = [checkout_primary, checkout_shadow],
)

# consistency re-grades the same answer several times and reports the spread.
# The judge surface is deterministic, so the spread is zero and the step passes
# while still contributing judge.mean / judge.spread to the trace.
answer_stability = consistency(
    name       = "answer-stability",
    criteria   = CRITERIA,
    model_url  = "http://judge-model:9080/v1/chat/completions",
    output     = "Refunds within 30 days. Contact support@example.com. " +
                 "You are legally entitled to compensation.",
    model      = "babelsuite-mock",
    rounds     = 3,
    max_spread = 0.0,
    severity   = "warn",
    after      = [judge_model],
)

# compare_latency times both builds of the quote endpoint. They are the same
# mock surface, so the shadow's median stays well within max_ratio and the step
# passes while contributing shadow.latency_ratio to the trace.
shadow_latency = compare_latency(
    name      = "shadow-latency",
    primary   = "http://checkout-primary:9080/quote",
    shadow    = "http://checkout-shadow:9080/quote",
    samples   = 8,
    max_ratio = 3.0,
    after     = [checkout_primary, checkout_shadow],
)
