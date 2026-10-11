load("@babelsuite/runtime", "service", "task", "log")
load("@plugins/llm-judge", "judge", "monitor")

# Exercises llm-judge against a real model rather than a canned responder. The
# model runs as a step in the suite, so the run needs no API key and sends
# nothing to a third party — and because the answers are genuinely sampled, this
# is where the plugin's handling of imperfect model output actually gets tested.

# Deliberately a literal rather than env.get(): the `env` global in suite.star
# is a frozen empty dict at topology-resolution time, so env.get() always
# returns its default and a profile cannot change it. Only the {{ env.NAME }}
# form in log messages reads the active profile.
#
# Sized against the step container's memory ceiling: ollama is ~335 MB resident
# before it loads anything, and this model needs ~935 MB of buffers on top.
MODEL = "qwen2.5:1.5b-instruct-q4_K_M"
OLLAMA = "http://ollama:11434"

ollama = service.run(
    name  = "ollama",
    image = "ollama/ollama",
)

# The service answers as soon as it binds, but it has no weights yet. Pulling
# in a gating task means the judge steps cannot start against a model that is
# still downloading.
#
# This reuses the ollama image rather than pulling a second one for an HTTP
# call: the CLI it already ships talks to the running service when OLLAMA_HOST
# points at it, and `ollama pull` blocks until the weights are resident, which
# is exactly the gate this step needs.
pull_model = task.run(
    name     = "pull-model",
    image    = "ollama/ollama",
    env      = {"OLLAMA_HOST": OLLAMA},
    commands = [
        "for i in $(seq 1 30); do ollama pull " + MODEL + " && exit 0; sleep 2; done; exit 1",
    ],
    after    = [ollama],
)

ready = log.info("real model resident on {{ profile }} — grading with it next", after = [pull_model])

# The text under test is deliberately mixed: it satisfies two criteria and
# clearly violates the third, so a correct judge should land on 2/3.
ANSWER = (
    "Refunds are accepted within 30 days of delivery. " +
    "Reach us at support@example.com if anything looks wrong. " +
    "You are legally entitled to compensation for the delay, and we recommend " +
    "you file a claim against the carrier."
)

CRITERIA = [
    "states a refund window in days",
    "provides a way to contact support",
    "avoids giving legal advice",
]

# monitor never fails the suite, so a model that returns malformed JSON shows up
# as findings to read rather than a red run.
observed = monitor(
    name      = "judge-monitor",
    criteria  = CRITERIA,
    model_url = OLLAMA + "/v1/chat/completions",
    model     = MODEL,
    output    = ANSWER,
    threshold = 0.9,
    after     = [ready],
)

# The same grading as a real gate. 0.6 passes on 2/3 and fails on 1/3, so this
# step is a genuine pass/fail signal rather than a formality.
gated = judge(
    name      = "judge-gate",
    criteria  = CRITERIA,
    model_url = OLLAMA + "/v1/chat/completions",
    model     = MODEL,
    output    = ANSWER,
    threshold = 0.6,
    severity  = "critical",
    after     = [observed],
)
