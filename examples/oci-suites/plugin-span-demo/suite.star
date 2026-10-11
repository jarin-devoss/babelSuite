load("@babelsuite/runtime", "service")
load("@plugins/llm-judge", "monitor")
load("@plugins/shadow-diff", "diff")

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

judge_model = service.mock(name = "judge-model")

# Two builds of one endpoint whose quotes agree on everything but total.
checkout_primary = service.mock(name = "checkout-primary")
checkout_shadow = service.mock(name = "checkout-shadow")

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
