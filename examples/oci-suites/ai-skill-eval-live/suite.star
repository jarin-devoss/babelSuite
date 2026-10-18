load("@babelsuite/runtime", "service", "task", "log")
load("@plugins/skill-eval", "trigger", "arguments", "outcome", "uplift",
     "consistency", "sequence", "latency")

# Tests a real model's skills. The ai-skill-eval suite proves the plugin scores
# correctly against canned replies; here the model genuinely chooses between
# skills, fills their arguments and follows their instructions, so the scores
# measure the model and the skill descriptions rather than the plugin.
#
# Every step is severity="warn": a 1.5B model on a CPU is a measurement, not a
# gate, and its findings are the point of the run. Set severity="critical" on
# a step to make it a gate for a model you depend on.

# Deliberately a literal rather than env.get(): the `env` global in suite.star
# is a frozen empty dict at topology-resolution time, so a profile cannot
# change it. Same model and memory sizing as llm-judge-live; Qwen 2.5 supports
# tool calls on Ollama's OpenAI-compatible endpoint.
MODEL = "qwen2.5:1.5b-instruct-q4_K_M"
OLLAMA = "http://ollama:11434"
CHAT = OLLAMA + "/v1/chat/completions"

# A CPU model is slow, and consistency asks each case several times.
TIMEOUT_MS = 120000

ollama = service.run(
    name  = "ollama",
    image = "ollama/ollama",
)

# The service answers as soon as it binds but has no weights yet; this gates
# the eval steps on the model being resident. See llm-judge-live for why it
# reuses the ollama image.
pull_model = task.run(
    name     = "pull-model",
    image    = "ollama/ollama",
    env      = {"OLLAMA_HOST": OLLAMA},
    commands = [
        "for i in $(seq 1 30); do ollama pull " + MODEL + " && exit 0; sleep 2; done; exit 1",
    ],
    after    = [ollama],
)

ready = log.info("real model resident on {{ profile }} — evaluating skills next", after = [pull_model])

SKILLS = [
    {
        "name":         "lookup_order",
        "description":  "Look up an order's status, items and delivery date by order id.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"}},"required":["order_id"]}',
    },
    {
        "name":         "issue_refund",
        "description":  "Refund an order that arrived damaged, wrong or not at all.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"},"reason":{"type":"string","enum":["damaged","wrong_item","not_delivered"]}},"required":["order_id","reason"]}',
        # Deliberately specific: uplift asks whether these instructions change
        # the answer, so they hold facts the model cannot know on its own.
        "instructions": "You are handling a refund for Northwind Outfitters. Confirm the order id back to the customer. Refunds reach the original payment method within 5 business days. Customers keep the damaged item; never ask them to send it back. Never offer discount codes.",
    },
    {
        "name":         "track_shipment",
        "description":  "Report where a parcel is right now and when it will arrive.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"}},"required":["order_id"]}',
    },
]

# Routing, with about a quarter of the cases negative controls: prompts that
# should call no skill. Over-triggering is as much a failure as missing.
ROUTING = [
    {"prompt": "Order 1001 arrived smashed, I want my money back.",     "expect": "issue_refund"},
    {"prompt": "You sent me a blue jacket instead of the red one I ordered, order 1002. Please refund it.", "expect": "issue_refund"},
    {"prompt": "Where is my parcel for order 1001 right now?",          "expect": "track_shipment"},
    {"prompt": "When will order 1003 be delivered?",                    "expect": "track_shipment"},
    {"prompt": "What items are in order 1004?",                         "expect": "lookup_order"},
    {"prompt": "Thanks, that's all for today!",                         "expect": None},
    {"prompt": "What are your opening hours?",                          "expect": None},
]

routing = trigger(
    name       = "routing",
    skills     = SKILLS,
    cases      = ROUTING,
    model_url  = CHAT,
    model      = MODEL,
    threshold  = 0.7,
    timeout_ms = TIMEOUT_MS,
    severity   = "warn",
    after      = [ready],
)

# pass^k over 3 samples per case; pass@k is reported beside it on the span.
routing_stability = consistency(
    name        = "routing-stability",
    skills      = SKILLS,
    cases       = ROUTING[:3],
    model_url   = CHAT,
    model       = MODEL,
    samples     = 3,
    temperature = 0.7,
    threshold   = 0.7,
    timeout_ms  = TIMEOUT_MS,
    severity    = "warn",
    after       = [routing],
)

# Values and shape: order_id must be an integer and reason one of the enum.
refund_args = arguments(
    name       = "refund-arguments",
    skills     = SKILLS,
    model_url  = CHAT,
    model      = MODEL,
    timeout_ms = TIMEOUT_MS,
    severity   = "warn",
    cases      = [
        {"prompt": "Order 1001 arrived smashed, refund it.", "expect": "issue_refund",
         "args": {"order_id": 1001, "reason": "damaged"}},
        {"prompt": "Order 1005 never showed up. I want a refund.", "expect": "issue_refund",
         "args": {"order_id": 1005, "reason": "not_delivered"}},
    ],
    after      = [routing],
)

# Needs both skills; in_order accepts extra calls on the way, since an agent
# that also tracks the parcel has still done the job.
refund_plan = sequence(
    name           = "refund-plan",
    skills         = SKILLS,
    model_url      = CHAT,
    model          = MODEL,
    sequence_match = "in_order",
    timeout_ms     = TIMEOUT_MS,
    severity       = "warn",
    cases          = [
        {"prompt": "Check what's in order 1001, and if it's the mug, refund it as damaged.",
         "sequence": ["lookup_order", "issue_refund"]},
    ],
    after          = [routing],
)

# A budget a CPU model can meet; tighten it for a hosted one.
answer_latency = latency(
    name           = "answer-latency",
    skills         = SKILLS,
    model_url      = CHAT,
    model          = MODEL,
    max_latency_ms = 60000,
    timeout_ms     = TIMEOUT_MS,
    severity       = "warn",
    cases          = [ROUTING[0], ROUTING[2]],
    after          = [routing],
)

REFUND_CASES = [
    {"prompt": "Order 1001 arrived smashed. When do I get my money, and do I need to send it back?",
     "expect": "issue_refund",
     "criteria": [
         "says the refund arrives within 5 business days",
         "says the customer does not need to return the item",
         "does not offer a discount code",
     ]},
]

# The same model judges itself here, which keeps the run offline. A stronger
# judge is better: set judge_url / judge_model to point at one.
refund_answers = outcome(
    name       = "refund-answers",
    skills     = SKILLS,
    cases      = REFUND_CASES,
    model_url  = CHAT,
    model      = MODEL,
    threshold  = 1.0,
    timeout_ms = TIMEOUT_MS,
    severity   = "warn",
    after      = [refund_args],
)

# The baseline. Without the instructions the model cannot know the 5-day window
# or the keep-the-item policy, so a working skill should gain criteria here.
# No gain means the instructions are not reaching the answer.
refund_uplift = uplift(
    name       = "refund-uplift",
    skills     = SKILLS,
    cases      = REFUND_CASES,
    model_url  = CHAT,
    model      = MODEL,
    min_gain   = 1,
    timeout_ms = TIMEOUT_MS,
    severity   = "warn",
    after      = [refund_answers],
)
